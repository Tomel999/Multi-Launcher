package panorama

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const manifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
const resourceURL = "https://resources.download.minecraft.net"

type VersionManifest struct {
	Versions []VersionEntry `json:"versions"`
}

type VersionEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type VersionMeta struct {
	AssetIndex struct {
		URL string `json:"url"`
	} `json:"assetIndex"`
	Downloads struct {
		Client struct {
			URL string `json:"url"`
		} `json:"client"`
	} `json:"downloads"`
}

type AssetIndex struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
	} `json:"objects"`
}

var (
	client     = &http.Client{Timeout: 60 * time.Second}
	manifestMu sync.Mutex
	manifest   *VersionManifest
)

func CacheRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, "multilauncherwails", "panoramas")
	os.MkdirAll(p, 0o755)
	return p
}

func versionDir(version string) string {
	p := filepath.Join(CacheRoot(), version)
	os.MkdirAll(p, 0o755)
	return p
}

func facePath(version string, i int) string {
	return filepath.Join(versionDir(version), fmt.Sprintf("panorama_%d.png", i))
}

func faceURL(version string, i int) string {
	return "/panoramas/" + version + fmt.Sprintf("/panorama_%d.png", i)
}

func doneMarker(version string) string {
	return filepath.Join(versionDir(version), "done.json")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func GetManifest() (*VersionManifest, error) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if manifest != nil {
		return manifest, nil
	}
	var m VersionManifest
	if err := fetchJSON(manifestURL, &m); err != nil {
		return nil, err
	}
	manifest = &m
	return manifest, nil
}

func FetchPanoramas(version string) ([6]string, error) {
	if fileExists(doneMarker(version)) {
		return cachedURLs(version), nil
	}

	m, err := GetManifest()
	if err != nil {
		return cachedURLs(version), err
	}
	var versionURL string
	for _, v := range m.Versions {
		if v.ID == version {
			versionURL = v.URL
			break
		}
	}
	if versionURL == "" {
		return cachedURLs(version), fmt.Errorf("version %q not in manifest", version)
	}

	var meta VersionMeta
	if err := fetchJSON(versionURL, &meta); err != nil {
		return cachedURLs(version), err
	}

	var urls [6]string
	if meta.AssetIndex.URL == "" {
		err = fetchFromJar(version, meta.Downloads.Client.URL, &urls)
	} else {
		var found bool
		found, err = fetchFromIndex(version, meta.AssetIndex.URL, &urls)
		if err == nil && !found {
			err = fetchFromJar(version, meta.Downloads.Client.URL, &urls)
		}
	}
	if err != nil {
		return urls, err
	}

	os.WriteFile(doneMarker(version), []byte("{}"), 0o644)
	return urls, nil
}

func fetchFromIndex(version, indexURL string, urls *[6]string) (bool, error) {
	var index AssetIndex
	if err := fetchJSON(indexURL, &index); err != nil {
		return false, err
	}
	found := false
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("minecraft/textures/gui/title/background/panorama_%d.png", i)
		obj, ok := index.Objects[key]
		if !ok || len(obj.Hash) < 2 {
			continue
		}
		found = true
		dest := facePath(version, i)
		if !fileExists(dest) {
			if err := downloadFile(fmt.Sprintf("%s/%s/%s", resourceURL, obj.Hash[:2], obj.Hash), dest); err != nil {
				return found, err
			}
		}
		urls[i] = faceURL(version, i)
	}
	return found, nil
}

func cachedURLs(version string) [6]string {
	var urls [6]string
	for i := 0; i < 6; i++ {
		if fileExists(facePath(version, i)) {
			urls[i] = faceURL(version, i)
		}
	}
	return urls
}

func fetchJSON(url string, target any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func downloadFile(url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}
