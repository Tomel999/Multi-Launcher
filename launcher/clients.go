package launcher

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ClientInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	HasModules bool   `json:"hasModules"`
}

type ClientVersion struct {
	ID        string   `json:"id"`
	Modules   []string `json:"modules"`
	LunarOnly bool     `json:"lunarOnly"`
}

var registeredClients = []ClientInfo{
	{
		ID: "lunar", Name: "Lunar Client",
		HasModules: true,
	},
	{
		ID: "feather", Name: "Feather",
	},
	{
		ID: "dawn", Name: "Dawn",
	},
}

func Clients() []ClientInfo { return registeredClients }

func ClientVersions(id string) ([]ClientVersion, error) {
	switch id {
	case "lunar":
		return lunarVersions()
	case "feather":
		return featherVersions(id)
	case "dawn":
		return dawnVersions()
	}
	return nil, fmt.Errorf("unknown client %q", id)
}

func LaunchClient(instName, clientID, version, module string, acc Account, opts LaunchOptions, log LogFn, state StateFn, onProgress ProgressFn) error {
	switch clientID {
	case "lunar":
		return LaunchLunar(instName, version, module, acc, LunarLaunchOptions{LaunchOptions: opts, Module: module}, log, state, onProgress)
	case "feather", "dawn":
		return LaunchFeather(clientID, instName, version, module, acc, opts, log, state, onProgress)
	}
	return fmt.Errorf("unknown client %q", clientID)
}

const lunarMetadataVer = "3.7.12"

type lunarMetadataSubversion struct {
	ID      string `json:"id"`
	Modules []struct {
		ID string `json:"id"`
	} `json:"modules"`
}

type lunarMetadataFamily struct {
	Subversions []lunarMetadataSubversion `json:"subversions"`
}

type lunarMetadataResponse struct {
	Versions []lunarMetadataFamily `json:"versions"`
}

func lunarFetchMetadata(list string) (map[string]bool, map[string][]string, error) {
	url := lunarAPIBase + "/launcher/metadata/versions/" + list +
		"?installation_id=" + uuid.NewString() +
		"&os=" + lunarPlatform() + "&arch=" + lunarArch() +
		"&launcher_version=" + lunarMetadataVer
	if list == "lunar" {
		url += "&branch=" + lunarBranch
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Lunar Client Launcher v"+lunarMetadataVer)
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("lunar metadata %s: HTTP %d", list, resp.StatusCode)
	}
	var out lunarMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, nil, err
	}
	ids := map[string]bool{}
	modules := map[string][]string{}
	for _, f := range out.Versions {
		for _, s := range f.Subversions {
			ids[s.ID] = true
			var mods []string
			seen := map[string]bool{}
			for _, m := range s.Modules {
				if !seen[m.ID] {
					seen[m.ID] = true
					mods = append(mods, m.ID)
				}
			}
			modules[s.ID] = mods
		}
	}
	return ids, modules, nil
}

func lunarVersions() ([]ClientVersion, error) {
	cache := filepath.Join(lunarBaseDir(), "versions.json")
	if vs, ok := readLunarVersionCache(cache); ok {
		return vs, nil
	}
	var lunarIDs, vanillaIDs map[string]bool
	var lunarModules map[string][]string
	var errL, errV error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		lunarIDs, lunarModules, errL = lunarFetchMetadata("lunar")
	}()
	go func() {
		defer wg.Done()
		vanillaIDs, _, errV = lunarFetchMetadata("vanilla")
	}()
	wg.Wait()
	if errL != nil {
		return nil, errL
	}
	if errV != nil {
		return nil, errV
	}
	versions := make([]ClientVersion, 0, len(lunarIDs))
	for id, mods := range lunarModules {
		versions = append(versions, ClientVersion{
			ID:        id,
			Modules:   mods,
			LunarOnly: !vanillaIDs[id],
		})
	}
	sort.Slice(versions, func(i, j int) bool { return lunarVersionLess(versions[i].ID, versions[j].ID) })
	writeLunarVersionCache(cache, versions)
	return versions, nil
}

func lunarVersionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			return a < b
		}
		if an != bn {
			return an < bn
		}
	}
	return len(as) < len(bs)
}

func readLunarVersionCache(path string) ([]ClientVersion, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cached struct {
		Fetched  time.Time       `json:"fetched"`
		Format   int             `json:"format"`
		Versions []ClientVersion `json:"versions"`
	}
	if json.Unmarshal(data, &cached) != nil || cached.Format != 2 || time.Since(cached.Fetched) > 6*time.Hour || len(cached.Versions) == 0 {
		return nil, false
	}
	return cached.Versions, true
}

func writeLunarVersionCache(path string, versions []ClientVersion) {
	data, _ := json.Marshal(struct {
		Fetched  time.Time       `json:"fetched"`
		Format   int             `json:"format"`
		Versions []ClientVersion `json:"versions"`
	}{time.Now().UTC(), 2, versions})
	os.WriteFile(path, data, 0o644)
}
