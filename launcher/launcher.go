package launcher

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const manifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
const libraryBaseURL = "https://libraries.minecraft.net"
const resourceURL = "https://resources.download.minecraft.net"

type Progress struct {
	Phase      string         `json:"phase"`
	Current    int64          `json:"current"`
	Total      int64          `json:"total"`
	Bytes      int64          `json:"bytes"`
	TotalBytes int64          `json:"totalBytes"`
	Speed      int64          `json:"speed"`
	File       string         `json:"file"`
	Files      []FileProgress `json:"files"`
}

type FileProgress struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Size  int64  `json:"size"`
	Speed int64  `json:"speed"`
}

type LogLine struct {
	Line string `json:"line"`
}

type State struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid"`
	Error   string `json:"error"`
}

type Manifest struct {
	Versions []VersionEntry `json:"versions"`
}

type VersionEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
	// version_manifest_v2 embeds javaVersion per version, so the required
	// Java can be resolved even when the per-version JSON is unreachable.
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
}

type VersionMeta struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
	MainClass    string `json:"mainClass"`
	Arguments    *struct {
		Game []json.RawMessage `json:"game"`
		JVM  []json.RawMessage `json:"jvm"`
	} `json:"arguments"`
	MinecraftArguments string `json:"minecraftArguments"`
	JavaVersion        struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	AssetIndex struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	} `json:"assetIndex"`
	Downloads struct {
		Client struct {
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
		} `json:"client"`
	} `json:"downloads"`
	Libraries []Library `json:"libraries"`
}

type Rule struct {
	Action string `json:"action"`
	OS     *struct {
		Name string `json:"name"`
		Arch string `json:"arch"`
	} `json:"os"`
	Features map[string]bool `json:"features"`
}

type Library struct {
	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Rules     []Rule            `json:"rules"`
	Natives   map[string]string `json:"natives"`
	Downloads struct {
		Artifact    Artifact            `json:"artifact"`
		Classifiers map[string]Artifact `json:"classifiers"`
	} `json:"downloads"`
}

type Artifact struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type AssetIndex struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
}

var httpClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 32,
		MaxConnsPerHost:     32,
		IdleConnTimeout:     90 * time.Second,
	},
}

func Migrate() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	base := filepath.Join(dir, ".multilauncher")
	migrateDir(filepath.Join(base, "game"), filepath.Join(base, "minecraft"))
	migrateDir(filepath.Join(base, "launcher"), filepath.Join(base, "config"))
}

func migrateDir(old, new string) {
	if _, err := os.Stat(old); err != nil {
		return
	}
	if _, err := os.Stat(new); os.IsNotExist(err) {
		if err := os.Rename(old, new); err == nil {
			return
		}
		os.MkdirAll(new, 0o755)
	}
	entries, err := os.ReadDir(old)
	if err != nil {
		return
	}
	for _, e := range entries {
		src := filepath.Join(old, e.Name())
		dst := filepath.Join(new, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		os.Rename(src, dst)
	}
	os.RemoveAll(old)
}

func Root() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, ".multilauncher", "minecraft")
	os.MkdirAll(p, 0o755)
	return p
}

func SkinsDir() string {
	p := filepath.Join(filepath.Dir(root()), "assets", "skins")
	os.MkdirAll(p, 0o755)
	return p
}

func SetRootForTests(dir string) func() {
	orig := rootDir
	rootDir = dir
	return func() { rootDir = orig }
}

var rootDir string

func root() string {
	if rootDir != "" {
		return rootDir
	}
	return Root()
}

func InstanceDir(name string) string {
	p := filepath.Join(root(), "instances", sanitize(name))
	os.MkdirAll(p, 0o755)
	return p
}

// InstanceExists reports whether an instance with the given display name
// already has a directory on disk. Display names are sanitized the same way
// they are when the directory is created, so callers can pass the raw name
// the user typed (or that was derived from meta).
func InstanceExists(name string) bool {
	p := filepath.Join(root(), "instances", sanitize(name))
	_, err := os.Stat(p)
	return err == nil
}

// CreateInstanceDir atomically creates the on-disk directory for a new
// instance and returns the final display name. Unlike the
// UniqueInstanceName-then-InstanceDir dance, the existence check and the
// directory creation are a single os.Mkdir call, so two concurrent creators
// can never both "win" the same name (check-then-create TOCTOU). If the
// requested name is taken, "Foo (2)", "Foo (3)"… are tried, matching
// UniqueInstanceName's convention. Returns the final display name.
func CreateInstanceDir(name string) (string, error) {
	instances := filepath.Join(root(), "instances")
	if err := os.MkdirAll(instances, 0o755); err != nil {
		return "", err
	}
	try := func(display string) (bool, error) {
		p := filepath.Join(instances, sanitize(display))
		err := os.Mkdir(p, 0o755)
		if err == nil {
			return true, nil
		}
		if os.IsExist(err) {
			return false, nil // taken, try the next suffix
		}
		return false, err
	}
	ok, err := try(name)
	if err != nil {
		return "", err
	}
	if ok {
		return name, nil
	}
	for i := 2; ; i++ {
		display := fmt.Sprintf("%s (%d)", name, i)
		ok, err := try(display)
		if err != nil {
			return "", err
		}
		if ok {
			return display, nil
		}
	}
}

// UniqueInstanceName returns a name that does not collide with any existing
// instance directory. If "Foo" is taken, it tries "Foo (2)", "Foo (3)", and
// so on. Check-then-act is not atomic — new code that CREATES an instance
// directory should use CreateInstanceDir instead; this remains for
// display-only/derive-a-name callers such as DuplicateInstance.
func UniqueInstanceName(name string) string {
	if !InstanceExists(name) {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)", name, i)
		if !InstanceExists(candidate) {
			return candidate
		}
	}
}

type Plugin struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	Disabled bool   `json:"disabled"`
	Size     int64  `json:"size"`
}

func ListPlugins(instName string) []Plugin {
	return ListFiles(instName, "mods")
}

func ListFiles(instName, subfolder string) []Plugin {
	return ListFilesDir(InstanceDir(instName), subfolder)
}

func ListFilesDir(baseDir, subfolder string) []Plugin {
	modsDir := filepath.Join(baseDir, subfolder)
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return []Plugin{}
	}
	out := []Plugin{}
	addEntries := func(dir, prefix string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".jar") && !strings.HasSuffix(lower, ".zip") && !strings.HasSuffix(lower, ".disabled") {
				continue
			}
			p := Plugin{
				Name:     name,
				Filename: prefix + name,
				Disabled: strings.HasSuffix(lower, ".disabled"),
			}
			if info, err := e.Info(); err == nil {
				p.Size = info.Size()
			}
			out = append(out, p)
		}
	}
	addEntries(modsDir, "")
	// Client mod folders (e.g. Ogulniega) keep some jars one level down
	// (OptiFine lives in preinstalled/ for OptiFabric). Surface them with
	// a prefixed Filename so delete/toggle/inspect hit the right path.
	addEntries(filepath.Join(modsDir, "preinstalled"), "preinstalled"+string(os.PathSeparator))
	return out
}

func DownloadContent(instName, subfolder, url, filename string) error {
	return DownloadContentDir(InstanceDir(instName), subfolder, url, filename)
}

func DownloadContentDir(baseDir, subfolder, url, filename string) error {
	dest := filepath.Join(baseDir, subfolder, filename)
	os.Remove(dest)
	return downloadTo(url, dest, 0, nil)
}

func DeleteContent(instName, subfolder, filename string) error {
	return DeleteContentDir(InstanceDir(instName), subfolder, filename)
}

func DeleteContentDir(baseDir, subfolder, filename string) error {
	dest := filepath.Join(baseDir, subfolder, filename)
	return os.Remove(dest)
}

func ToggleContent(instName, subfolder, filename string, disable bool) error {
	return ToggleContentDir(InstanceDir(instName), subfolder, filename, disable)
}

func ToggleContentDir(baseDir, subfolder, filename string, disable bool) error {
	dest := filepath.Join(baseDir, subfolder, filename)
	var target string
	if disable && !strings.HasSuffix(filename, ".disabled") {
		target = dest + ".disabled"
	} else if !disable && strings.HasSuffix(filename, ".disabled") {
		target = dest[:len(dest)-len(".disabled")]
	} else {
		return nil
	}
	return os.Rename(dest, target)
}

type ModInfo struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

func InspectMod(instName, subfolder, filename string) ModInfo {
	return InspectModDir(InstanceDir(instName), subfolder, filename)
}

func InspectModDir(baseDir, subfolder, filename string) ModInfo {
	path := filepath.Join(baseDir, subfolder, filename)
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ModInfo{Name: filename}
	}
	defer zr.Close()
	return inspectJar(&zr.Reader)
}

// inspectJar extracts a display name/icon from a mod jar. It takes a
// *zip.Reader (not a zip.ReadCloser by value) — copying a ReadCloser would
// copy zip.Reader's internal sync.Once, which `go vet` correctly rejects.
func inspectJar(zr *zip.Reader) ModInfo {
	var name, iconPath string
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[strings.ToLower(f.Name)] = f
	}
	if f := byName["fabric.mod.json"]; f != nil {
		if data, err := readZip(f); err == nil {
			var m struct {
				Name string `json:"name"`
				Icon string `json:"icon"`
			}
			if json.Unmarshal(data, &m) == nil {
				name, iconPath = m.Name, m.Icon
			}
		}
	} else if f := byName["meta-inf/mods.toml"]; f != nil {
		if data, err := readZip(f); err == nil {
			block := string(data)
			if i := strings.Index(block, "[[mods]]"); i >= 0 {
				if j := strings.Index(block[i:], "\n\n"); j >= 0 {
					block = block[i : i+j]
				}
				for _, line := range strings.Split(block, "\n") {
					if name == "" && strings.HasPrefix(strings.TrimSpace(line), "name=") {
						name = strings.Trim(strings.TrimSpace(line)[5:], `"'`)
					}
					if strings.HasPrefix(strings.TrimSpace(line), "logoFile=") {
						iconPath = strings.Trim(strings.TrimSpace(line)[9:], `"'`)
					}
				}
			}
		}
	}
	if name == "" {
		if f := byName["mcmod.info"]; f != nil {
			if data, err := readZip(f); err == nil {
				var list []struct {
					Name string `json:"name"`
					Icon string `json:"icon"`
				}
				if json.Unmarshal(data, &list) == nil && len(list) > 0 {
					name, iconPath = list[0].Name, list[0].Icon
				}
			}
		}
	}
	if iconPath == "" {
		if f := byName["pack.png"]; f != nil {
			iconPath = "pack.png"
		} else {
			for _, f := range zr.File {
				if strings.HasSuffix(strings.ToLower(f.Name), ".png") && !strings.Contains(f.Name, "/") {
					iconPath = f.Name
					break
				}
			}
		}
	}
	icon := ""
	if iconPath != "" {
		if f := byName[strings.ToLower(iconPath)]; f != nil {
			if data, err := readZip(f); err == nil && len(data) > 0 {
				icon = base64.StdEncoding.EncodeToString(data)
			}
		}
	}
	return ModInfo{Name: name, Icon: icon}
}

func readZip(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func versionsDir() string {
	p := filepath.Join(Root(), "versions")
	os.MkdirAll(p, 0o755)
	return p
}

func librariesDir() string {
	p := filepath.Join(Root(), "libraries")
	os.MkdirAll(p, 0o755)
	return p
}

func assetsDir() string {
	p := filepath.Join(Root(), "assets")
	os.MkdirAll(p, 0o755)
	return p
}

func nativesDir(version string) string {
	p := filepath.Join(Root(), "natives", version)
	os.MkdirAll(p, 0o755)
	return p
}

func versionJSONPath(version string) string {
	return filepath.Join(versionsDir(), version, version+".json")
}

func clientJarPath(version string) string {
	return filepath.Join(versionsDir(), version, version+".jar")
}

func sanitize(name string) string {
	replacer := strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", `"`, "_",
		"<", "_", ">", "_", "|", "_",
	)
	name = replacer.Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "instance"
	}
	return name
}

func fetchJSON(url string, target any) error {
	raw, err := fetchRaw(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func fetchRaw(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func GetManifest() (*Manifest, error) {
	var m Manifest
	if err := fetchJSON(manifestURL, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func GetVersionMeta(version string) (*VersionMeta, error) {
	return getVersionMeta(version, map[string]bool{version: true})
}

func getVersionMeta(version string, seen map[string]bool) (*VersionMeta, error) {
	seen[version] = true
	jsonPath := versionJSONPath(version)
	if data, err := os.ReadFile(jsonPath); err == nil {
		var v VersionMeta
		if json.Unmarshal(data, &v) == nil && v.MainClass != "" {
			if err := mergeInherits(&v, seen); err != nil {
				return nil, err
			}
			return &v, nil
		}
	}
	m, err := GetManifest()
	if err != nil {
		return nil, err
	}
	var versionURL string
	for _, e := range m.Versions {
		if e.ID == version {
			versionURL = e.URL
			break
		}
	}
	if versionURL == "" {
		return nil, fmt.Errorf("version %q not found in manifest", version)
	}
	raw, err := fetchRaw(versionURL)
	if err != nil {
		return nil, err
	}
	os.MkdirAll(filepath.Dir(jsonPath), 0o755)
	os.WriteFile(jsonPath, raw, 0o644)
	var v VersionMeta
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if err := mergeInherits(&v, seen); err != nil {
		return nil, err
	}
	return &v, nil
}

func mergeInherits(v *VersionMeta, seen map[string]bool) error {
	if v.InheritsFrom == "" {
		return nil
	}
	if seen[v.InheritsFrom] {
		return fmt.Errorf("inherit cycle detected at %q (from %q)", v.InheritsFrom, v.ID)
	}
	parent, err := getVersionMeta(v.InheritsFrom, seen)
	if err != nil {
		return fmt.Errorf("inherit %s for %s: %w", v.InheritsFrom, v.ID, err)
	}
	if parent.AssetIndex.ID != "" {
		v.AssetIndex = parent.AssetIndex
	}
	if parent.JavaVersion.MajorVersion > 0 && v.JavaVersion.MajorVersion == 0 {
		v.JavaVersion = parent.JavaVersion
	}
	if parent.Downloads.Client.Size > 0 {
		v.Downloads = parent.Downloads
	}
	if v.Arguments == nil {
		v.Arguments = parent.Arguments
	} else if parent.Arguments != nil {
		v.Arguments.Game = append(parent.Arguments.Game, v.Arguments.Game...)
		v.Arguments.JVM = append(parent.Arguments.JVM, v.Arguments.JVM...)
	}
	if v.MinecraftArguments == "" {
		v.MinecraftArguments = parent.MinecraftArguments
	}
	return nil
}

func osName() string {
	if runtime.GOOS == "darwin" {
		return "osx"
	}
	return runtime.GOOS
}

func osArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

func rulesAllow(rules []Rule) bool {
	if len(rules) == 0 {
		return true
	}
	allow := false
	for _, r := range rules {
		ok := true
		if r.OS != nil {
			if r.OS.Name != "" && r.OS.Name != osName() {
				ok = false
			}
			if r.OS.Arch != "" && r.OS.Arch != osArch() {
				ok = false
			}
		}
		if len(r.Features) > 0 {
			ok = false
		}
		if !ok {
			continue
		}
		switch r.Action {
		case "disallow":
			return false
		case "allow":
			allow = true
		}
	}
	return allow
}
