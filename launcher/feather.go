package launcher

import (
	"archive/zip"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	featherVersionIndexURL = "https://game-launcher.feathermc.com/release/version_index.json"
	dawnManifestBaseURL    = "https://api.dawn.gg/v2/deployments/game/stable"
	featherManifestBaseURL = "https://game-launcher.feathermc.com/release/versions"

	featherAssetBaseURL = "https://assets.feathercdn.net/"
	dawnAssetBaseURL    = "https://assets.dawn.gg/assets/"
)

type featherLib struct {
	Name      string `json:"name"`
	SHA1      string `json:"sha1"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
	Feather   bool   `json:"feather"`
	ClassPath *bool  `json:"classpath"`
}

type featherNative struct {
	Name           string `json:"name"`
	SHA1           string `json:"sha1"`
	URL            string `json:"url"`
	ExtractedFiles []struct {
		Name string `json:"name"`
	} `json:"extracted_files"`
}

type featherNativeLib struct {
	Natives map[string]featherNative `json:"natives"`
}

type featherManifest struct {
	Name               string             `json:"name"`
	MainClass          string             `json:"main_class"`
	Runtime            string             `json:"runtime"`
	MinecraftArguments string             `json:"minecraft_arguments"`
	Libraries          []featherLib       `json:"libraries"`
	Mods               []featherLib       `json:"mods"`
	NativeLibraries    []featherNativeLib `json:"native_libraries"`
	AssetsIndex        struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"assets_index"`
	GameAssetsIndex struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"game_assets_index"`
}

type featherIndexEntry struct {
	Name string `json:"name"`
	Info struct {
		URL  string `json:"url"`
		SHA1 string `json:"sha1"`
	} `json:"info"`
}

// ClientsDataDir is the parent folder holding per-client data:
// clients/lunar, clients/feather and clients/dawn. No side effects.
func ClientsDataDir() string {
	return filepath.Join(root(), "clients")
}

// FeatherDataDir is the shared Feather client folder. No side effects.
func FeatherDataDir() string {
	p := filepath.Join(ClientsDataDir(), "feather")
	return p
}

// DawnDataDir is the shared Dawn client folder. No side effects.
func DawnDataDir() string {
	p := filepath.Join(ClientsDataDir(), "dawn")
	return p
}

// featherBaseDir returns the per-client data dir (client is "feather" or
// "dawn"), so each client keeps its own versions, jars and assets.
func featherBaseDir(client string) string {
	if client != "feather" && client != "dawn" {
		client = "feather"
	}
	p := filepath.Join(ClientsDataDir(), client)
	os.MkdirAll(p, 0o755)
	return p
}

func featherJarDir(client, sha1 string) string {
	return filepath.Join(featherBaseDir(client), "jars", sha1)
}

func featherManifestPath(client, version string) string {
	return filepath.Join(featherBaseDir(client), "versions", client, version+".json")
}

func featherVersions(client string) ([]ClientVersion, error) {
	cache := filepath.Join(featherBaseDir(client), "versions.json")
	if vs, ok := readFeatherVersionCache(cache); ok {
		return vs, nil
	}
	var idx struct {
		Metadata []featherIndexEntry `json:"metadata"`
	}
	if err := fetchJSON(featherVersionIndexURL, &idx); err != nil {
		return nil, err
	}
	versions := make([]ClientVersion, 0, len(idx.Metadata))
	for _, m := range idx.Metadata {
		id := m.Name
		var modules []string
		if i := strings.LastIndex(id, "-"); i > 0 {
			modules = []string{id[i+1:]}
			id = id[:i]
		} else if mod := featherSniffModule(client, id); mod != "" {
			modules = []string{mod}
		}
		versions = append(versions, ClientVersion{ID: id, Modules: modules})
	}
	sort.Slice(versions, func(i, j int) bool { return lunarVersionLess(versions[i].ID, versions[j].ID) })
	writeFeatherVersionCache(cache, versions)
	return versions, nil
}

// featherSniffModule detects the loader bundled in a suffix-less Feather
// build (e.g. 1.8.9 and 1.12.2 ship Forge inside a plain-named manifest) by
// reading its manifest once. Returns "" when unknown or unreachable — the
// version then simply lists no modules, as before.
func featherSniffModule(client, id string) string {
	m, err := featherResolveManifest(client, id)
	if err != nil {
		return ""
	}
	if strings.Contains(m.MainClass, "Knot") {
		return "fabric"
	}
	if strings.Contains(m.MinecraftArguments, "FMLTweaker") {
		return "forge"
	}
	for _, l := range append(append([]featherLib{}, m.Libraries...), m.Mods...) {
		if strings.Contains(l.Name, "minecraftforge") || strings.Contains(l.Name, "FMLTweaker") {
			return "forge"
		}
		if strings.Contains(l.Name, "fabricmc") {
			return "fabric"
		}
	}
	return ""
}

// dawnVersionIndexURL is Dawn's own version index, recovered from the
// official launcher's DawnClientVersionIndexEndpoint class:
// https://api.dawn.gg/v2/deployments/game/<channel>/version_index.json
// Unlike the bare .../game/stable path it needs no session token.
const dawnVersionIndexURL = "https://api.dawn.gg/v2/deployments/game/stable/version_index.json"

// dawnVersions returns the MC versions Dawn actually ships, read from Dawn's
// own version index. Results cache for 6h like the rest.
func dawnVersions() ([]ClientVersion, error) {
	cache := filepath.Join(featherBaseDir("dawn"), "versions.json")
	if vs, ok := readDawnVersionCache(cache); ok {
		return vs, nil
	}
	var idx struct {
		Metadata []featherIndexEntry `json:"metadata"`
	}
	if err := fetchJSON(dawnVersionIndexURL, &idx); err != nil {
		return nil, err
	}
	byID := map[string]map[string]bool{}
	for _, m := range idx.Metadata {
		id, module := m.Name, ""
		if i := strings.LastIndex(id, "-"); i > 0 {
			module = id[i+1:]
			id = id[:i]
		}
		if byID[id] == nil {
			byID[id] = map[string]bool{}
		}
		if module != "" {
			byID[id][module] = true
		}
	}
	versions := make([]ClientVersion, 0, len(byID))
	for id, mods := range byID {
		var list []string
		for _, m := range []string{"fabric", "forge"} {
			if mods[m] {
				list = append(list, m)
			}
		}
		versions = append(versions, ClientVersion{ID: id, Modules: list})
	}
	sort.Slice(versions, func(i, j int) bool { return lunarVersionLess(versions[i].ID, versions[j].ID) })
	writeDawnVersionCache(cache, versions)
	return versions, nil
}

func readDawnVersionCache(path string) ([]ClientVersion, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cached struct {
		Fetched  time.Time       `json:"fetched"`
		Format   int             `json:"format"`
		Versions []ClientVersion `json:"versions"`
	}
	if json.Unmarshal(data, &cached) != nil || cached.Format != 5 || time.Since(cached.Fetched) > 6*time.Hour || len(cached.Versions) == 0 {
		return nil, false
	}
	return cached.Versions, true
}

func writeDawnVersionCache(path string, versions []ClientVersion) {
	data, _ := json.Marshal(struct {
		Fetched  time.Time       `json:"fetched"`
		Format   int             `json:"format"`
		Versions []ClientVersion `json:"versions"`
	}{time.Now().UTC(), 5, versions})
	os.WriteFile(path, data, 0o644)
}

func readFeatherVersionCache(path string) ([]ClientVersion, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cached struct {
		Fetched  time.Time       `json:"fetched"`
		Format   int             `json:"format"`
		Versions []ClientVersion `json:"versions"`
	}
	if json.Unmarshal(data, &cached) != nil || cached.Format != 3 || time.Since(cached.Fetched) > 6*time.Hour || len(cached.Versions) == 0 {
		return nil, false
	}
	return cached.Versions, true
}

func writeFeatherVersionCache(path string, versions []ClientVersion) {
	data, _ := json.Marshal(struct {
		Fetched  time.Time       `json:"fetched"`
		Format   int             `json:"format"`
		Versions []ClientVersion `json:"versions"`
	}{time.Now().UTC(), 3, versions})
	os.WriteFile(path, data, 0o644)
}

func featherFullVersion(version, module string) string {
	if module != "" && !strings.HasSuffix(version, "-"+module) {
		return version + "-" + module
	}
	return version
}

func featherOfflineUUID(username string) string {
	digest := md5.Sum([]byte("OfflinePlayer:" + username))
	digest[6] = (digest[6] & 0x0F) | 0x30
	digest[8] = (digest[8] & 0x3F) | 0x80
	return strings.ReplaceAll(fmt.Sprintf("%x", digest), "-", "")
}

func featherHasFabricMod(jar string) bool {
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "fabric.mod.json" {
			return true
		}
	}
	return false
}

func featherResolveManifest(client, version string) (*featherManifest, error) {
	cache := featherManifestPath(client, version)
	if data, err := os.ReadFile(cache); err == nil {
		var m featherManifest
		if json.Unmarshal(data, &m) == nil && m.MainClass != "" {
			return &m, nil
		}
	}
	url := ""
	switch client {
	case "dawn":
		url = dawnManifestBaseURL + "/" + version + "/manifest.json"
		if dawnManifestNegative(version) {
			url = featherManifestBaseURL + "/" + version + ".json"
		}
	case "feather":
		url = featherManifestBaseURL + "/" + version + ".json"
	}
	var m featherManifest
	if client == "dawn" && !dawnManifestNegative(version) {
		if err := fetchJSON(url, &m); err != nil {
			writeDawnManifestNegative(version)
			url = featherManifestBaseURL + "/" + version + ".json"
			if err2 := fetchJSON(url, &m); err2 != nil {
				return nil, fmt.Errorf("manifest %s/%s: %v", client, version, err2)
			}
		}
	} else if err := fetchJSON(url, &m); err != nil {
		plain := version
		if i := strings.LastIndex(version, "-"); i > 0 {
			plain = version[:i]
		}
		if plain == version {
			return nil, fmt.Errorf("manifest %s/%s: %w", client, version, err)
		}
		if err2 := fetchJSON(featherManifestBaseURL+"/"+plain+".json", &m); err2 != nil {
			return nil, fmt.Errorf("manifest %s/%s: %w", client, version, err)
		}
	}
	os.MkdirAll(filepath.Dir(cache), 0o755)
	if data, err := json.Marshal(&m); err == nil {
		os.WriteFile(cache, data, 0o644)
	}
	return &m, nil
}

func featherBuildCommand(java string, cp []string, nativesDir string, m *featherManifest, instDir, username, uuid, token, userType, ram, extraJVM, proxyURL string, dawn bool, javaMajor int, agentJar string) []string {
	args := []string{
		"-Xmx" + ram,
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+UseG1GC",
		"-XX:G1NewSizePercent=20",
		"-XX:G1ReservePercent=20",
		"-XX:MaxGCPauseMillis=50",
		"-XX:G1HeapRegionSize=32M",
		"-Dlog4j2.formatMsgNoLookups=true",
		"-Djava.library.path=" + nativesDir,
	}
	if runtime.GOOS == "windows" {
		args = append(args, "-Djavax.net.ssl.trustStoreType=WINDOWS-ROOT")
	}
	if dawn {
		args = append(args,
			"-Dminecraft.launcher.brand=dawn-cli",
			"-Dminecraft.launcher.version=1.0",
			"-Dminecraft.api.secure_profiles=false",
			"-Dminecraft.api.env=none",
			"-Dminecraft.chat.profile=false",
			"-Dfabric.log.level=debug",
		)
		if javaMajor >= 16 {
			args = append(args, "--add-modules=jdk.incubator.vector")
		}
	}
	if proxyURL != "" {
		args = append(args,
			"-Dminecraft.api.session.host="+proxyURL,
			"-Dminecraft.api.services.host="+proxyURL,
			"-Dminecraft.api.auth.host="+proxyURL,
			"-Dminecraft.api.profiles.host="+proxyURL,
			"-Djava.net.preferIPv4Stack=true",
		)
	}
	if agentJar != "" {
		args = append(args, "-javaagent:"+agentJar)
	}
	if extraJVM != "" {
		args = append(args, strings.Fields(extraJVM)...)
	}
	u := uuid
	subs := map[string]string{
		"${auth_player_name}":  username,
		"${game_directory}":    instDir,
		"${auth_uuid}":         u,
		"${auth_access_token}": token,
		"${user_properties}":   "{}",
		"${user_type}":         userType,
	}
	if name := m.GameAssetsIndex.Name; name != "" {
		subs["${assets_index_name}"] = strings.TrimSuffix(name, ".json")
	}
	mcArgs := strings.Fields(m.MinecraftArguments)
	for i, f := range mcArgs {
		for k, v := range subs {
			f = strings.ReplaceAll(f, k, v)
		}
		mcArgs[i] = f
	}
	cmd := append([]string{java}, args...)
	cmd = append(cmd, "-cp", strings.Join(cp, string(os.PathListSeparator)), m.MainClass)
	cmd = append(cmd, mcArgs...)
	return cmd
}

func LaunchFeather(clientID, instName, version, module string, acc Account, opts LaunchOptions, log LogFn, state StateFn, onProgress ProgressFn) error {
	username := acc.Name
	if username == "" {
		username = "Player"
	}
	if module == "lunar" {
		module = ""
	}
	version = featherFullVersion(version, module)
	m, err := featherResolveManifest(clientID, version)
	if err != nil {
		return err
	}
	instDir := InstanceDir(instName)

	log("  Bundled Java...")
	javaMajor := parseJavaRuntime(m.Runtime)
	if javaMajor == 0 {
		javaMajor = 21
	}
	var javaPath string
	if opts.JavaPath != "" {
		if _, err := os.Stat(opts.JavaPath); err != nil {
			return fmt.Errorf("custom java path not found: %s", opts.JavaPath)
		}
		javaPath = opts.JavaPath
	} else {
		javaPath, err = ensureJavaForRuntime(m.Runtime, onProgress)
		if err != nil {
			return err
		}
	}

	cp, err := featherDownloadLibs(clientID, m, instDir, log, onProgress)
	if err != nil {
		return err
	}

	nativeCP, nativesDir, err := featherDownloadNatives(clientID, version, m, onProgress)
	if err != nil {
		return err
	}
	cp = append(cp, nativeCP...)

	log("  Mojang assets...")
	if err := featherEnsureMojangAssets(m, instDir, onProgress); err != nil {
		return err
	}
	log("  Client assets...")
	if err := featherEnsureClientAssets(m, clientID, instDir, onProgress); err != nil {
		return err
	}

	ram := opts.Memory
	if ram == "" {
		ram = "2G"
	}
	online := acc.Type == "microsoft" && acc.AccessToken != ""
	uuidStr := acc.ID
	if uuidStr == "" || uuidStr == "00000000-0000-0000-0000-000000000000" {
		uuidStr = featherOfflineUUID(username)
	}
	token := acc.AccessToken
	userType := acc.UserType
	if userType == "" {
		userType = "mojang"
	}
	proxyURL := ""
	stopProxy := func() {}
	if !online {
		token = "0"
		userType = "legacy"
		if clientID == "dawn" {
			proxyURL, stopProxy = dawnAuthProxy(username, uuidStr)
			token = fakeAccessToken(username, uuidStr)
			userType = "mojang"
		}
	}

	agentJar := ""
	if strings.Contains(m.MainClass, "launchwrapper") {
		var err error
		agentJar, err = ensureFmlSideAgent(onProgress)
		if err != nil {
			return err
		}
	}
	cmd := featherBuildCommand(javaPath, cp, nativesDir, m, instDir, username, uuidStr, token, userType, ram, opts.JvmArgs, proxyURL, clientID == "dawn", javaMajor, agentJar)
	full := exec.Command(windowlessJava(javaPath), cmd[1:]...)
	full.Dir = instDir
	hideWindow(full)
	if len(opts.Env) > 0 {
		full.Env = append(os.Environ(), opts.Env...)
	}
	log("Java: " + windowlessJava(javaPath))

	if opts.PreLaunch != "" {
		log("Pre-launch: " + opts.PreLaunch)
		if err := runShell(opts.PreLaunch, log); err != nil {
			return fmt.Errorf("pre-launch command failed: %w", err)
		}
	}

	stdout, err := full.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := full.StderrPipe()
	if err != nil {
		return err
	}
	return runGame(full, opts, log, state, stdout, stderr, stopProxy, clientID+" "+version)
}
