package launcher

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const (
	lunarAPIBase         = "https://api.lunarclientprod.com"
	lunarLaunchURL       = lunarAPIBase + "/launcher/launch"
	lunarLauncherVer     = "3.7.12-ow"
	lunarBranch          = "master"
	lunarUserAgent       = "LunarClient/CustomLauncher"
	lunarResourceURL     = "https://resources.download.minecraft.net"
	lunarVersionManifest = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
)

func lunarBaseDir() string {
	p := LunarDataDir()
	os.MkdirAll(p, 0o755)
	return p
}

// LunarDataDir is the shared Lunar Client folder. No side effects, so callers
// can inspect or delete it without recreating it.
func LunarDataDir() string {
	p := filepath.Join(ClientsDataDir(), "lunar")
	return p
}

func lunarOfflineDir() string {
	p := filepath.Join(lunarBaseDir(), "offline", "multiver")
	os.MkdirAll(p, 0o755)
	return p
}

// LunarInstanceModsDir is the folder holding one lunar instance's mods:
// instances/<name>/mods/<module>-<ver>. Both the official base modpack and
// the user's own mods live here, so every instance has an independent mod
// set. With an empty module it falls back to the instance mods directory.
func LunarInstanceModsDir(instName, version, module string) string {
	if module == "" {
		return lunarInstanceModsRoot(instName)
	}
	return filepath.Join(lunarInstanceModsRoot(instName), module+"-"+version)
}

func lunarInstanceModsRoot(instName string) string {
	p := filepath.Join(InstanceDir(instName), "mods")
	os.MkdirAll(p, 0o755)
	return p
}

func lunarTexturesDir() string {
	p := filepath.Join(lunarBaseDir(), "textures")
	os.MkdirAll(p, 0o755)
	return p
}

func lunarUIDir() string {
	p := filepath.Join(lunarBaseDir(), "ui")
	os.MkdirAll(p, 0o755)
	return p
}

func lunarProfilesDir() string {
	p := filepath.Join(lunarBaseDir(), "profiles")
	os.MkdirAll(p, 0o755)
	return p
}

func randomHex(n int) string {
	const chars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func lunarPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	}
	return runtime.GOOS
}

func lunarArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	}
	return runtime.GOARCH
}

func lunarOSRelease() string {
	// os.release() approximation — the API only logs it, any string works.
	return "0.0.0"
}

func lunarClasspathSep() string {
	if runtime.GOOS == "windows" {
		return ";"
	}
	return ":"
}

// lunarLaunchPayload mirrors lunar.py _build_launch_payload.
type lunarLaunchPayload struct {
	Hwid             string   `json:"hwid"`
	InstallationID   string   `json:"installation_id"`
	HwidPrivate      string   `json:"hwid_private"`
	OS               string   `json:"os"`
	OSRelease        string   `json:"os_release"`
	Arch             string   `json:"arch"`
	LauncherVersion  string   `json:"launcher_version"`
	Version          string   `json:"version"`
	Branch           string   `json:"branch"`
	LaunchType       string   `json:"launch_type"`
	Args             []string `json:"args"`
	Module           string   `json:"module"`
	CanaryPreference string   `json:"canary_preference"`
}

type lunarArtifact struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type lunarJRE struct {
	Download struct {
		URL         string `json:"url"`
		FallbackURL string `json:"fallbackUrl"`
		Extension   string `json:"extension"`
	} `json:"download"`
	ExecutablePathInArchive []string `json:"executablePathInArchive"`
	FolderChecksum          string   `json:"folderChecksum"`
	ExtraArguments          []string `json:"extraArguments"`
}

type lunarLaunchData struct {
	Success bool `json:"success"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
	LaunchTypeData struct {
		MainClass string          `json:"mainClass"`
		Artifacts []lunarArtifact `json:"artifacts"`
	} `json:"launchTypeData"`
	JRE      lunarJRE `json:"jre"`
	Textures *struct {
		IndexURL  string `json:"indexUrl"`
		BaseURL   string `json:"baseUrl"`
		IndexSHA1 string `json:"indexSha1"`
	} `json:"textures"`
	UI *struct {
		SourceURL  string `json:"sourceUrl"`
		SourceSHA1 string `json:"sourceSha1"`
		Assets     struct {
			IndexURL  string `json:"indexUrl"`
			BaseURL   string `json:"baseUrl"`
			IndexSHA1 string `json:"indexSha1"`
		} `json:"assets"`
	} `json:"ui"`
	BaseModpack *struct {
		MrpackURL string `json:"mrpackUrl"`
		Hash      string `json:"hash"`
	} `json:"baseModpack"`
}

// fetchLunarLaunchData POSTs to /launcher/launch and returns the parsed body.
// canary_preference is always "OPT_OUT": Lunar's server treats "NEUTRAL" as "no
// preference" and then serves canary (preview) builds even for branch=master.
// OPT_OUT explicitly pins the stable release channel.
func fetchLunarLaunchData(version, module, server string, online bool) (*lunarLaunchData, error) {
	payload := lunarLaunchPayload{
		Hwid:             randomHex(34),
		InstallationID:   uuid.NewString(),
		HwidPrivate:      randomHex(512),
		OS:               lunarPlatform(),
		OSRelease:        lunarOSRelease(),
		Arch:             lunarArch(),
		LauncherVersion:  lunarLauncherVer,
		Version:          version,
		Branch:           lunarBranch,
		LaunchType:       "ONLINE",
		Args:             []string{},
		Module:           module,
		CanaryPreference: "OPT_OUT",
	}
	if !online {
		payload.LaunchType = "OFFLINE"
	}
	if server != "" {
		payload.Args = []string{"--server=" + server}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, lunarLaunchURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Lunar Client Launcher v"+lunarLauncherVer)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("x-hwid", payload.Hwid)
	req.Header.Set("x-installation_id", payload.InstallationID)
	req.Header.Set("x-hwid_private", payload.HwidPrivate)
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lunar API error %d", resp.StatusCode)
	}
	var data lunarLaunchData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if !data.Success {
		return nil, fmt.Errorf("lunar launch failed: %s", data.Error.Message)
	}
	return &data, nil
}

// lunarBuildJavaCommand mirrors lunar.py _build_java_command, WITHOUT the
// offline javaagent. genesis is the main class.
func lunarBuildJavaCommand(javaPath string, classpath, classpathNames, externalsNames []string, nativesDirs []string, data *lunarLaunchData, version, module, server, accessToken, username, uuidStr string, opts LaunchOptions, instName string) ([]string, error) {
	mainClass := data.LaunchTypeData.MainClass
	if mainClass == "" {
		mainClass = "com.moonsworth.lunar.genesis.Genesis"
	}
	nativePath := filepath.Join(lunarOfflineDir(), "natives")
	if len(nativesDirs) > 0 {
		nativePath = nativesDirs[0]
	}
	webosrPath := filepath.Join(lunarOfflineDir(), "natives", "web")

	memory := opts.Memory
	if memory == "" {
		memory = "2G"
	}
	jvmArgs := []string{
		"-Xms" + memory,
		"-Xmx" + memory,
		"-Djava.library.path=" + nativePath,
		"-Dlog4j2.formatMsgNoLookups=true",
		"-Dcom.moonsworth.lunar.debug=false",
		"-Dlunar.dataDir=" + lunarBaseDir(),
		// Genesis resolves the vanilla dir (libraries/versions) from
		// ichor.gameDirectory; without it, Windows defaults to %APPDATA%\.minecraft.
		"-Dichor.gameDirectory=" + InstanceDir(instName),
		"-javaagent:" + lunarAgentJar(),
		"-noverify",
	}
	jvmArgs = append(jvmArgs, data.JRE.ExtraArguments...)
	if module == "fabric" || module == "forge" {
		jvmArgs = append(jvmArgs, "-Dichor.fabric.localModPath="+lunarInstanceModsRoot(instName))
	}

	cp := strings.Join(classpath, lunarClasspathSep())
	cpArg := []string{"-cp", cp}
	if runtime.GOOS == "windows" && len(cp) > 7000 {
		cpFile := filepath.Join(lunarOfflineDir(), "classpath.txt")
		if err := os.WriteFile(cpFile, []byte(cp), 0o644); err != nil {
			return nil, err
		}
		cpArg = []string{"@" + cpFile}
	}

	ichorCP := strings.Join(classpathNames, ",")
	ichorExt := strings.Join(externalsNames, ",")

	genesisArgs := []string{
		"--version", version,
		"--accessToken", accessToken,
		"--assetIndex", lunarAssetIndexID(version),
		"--userProperties", "{}",
		"--gameDir", InstanceDir(instName),
		"--texturesDir", lunarTexturesDir(),
		"--launcherVersion", lunarLauncherVer,
		"--hwid", "none",
		"--installationId", "none",
		"--width", fmt.Sprint(opts.Width),
		"--height", fmt.Sprint(opts.Height),
		"--workingDirectory", ".",
		"--classpathDir", lunarOfflineDir(),
		"--ichorClassPath", ichorCP,
		"--ichorExternalFiles", ichorExt,
		"--webosrPath", webosrPath,
		"--intentionalGson",
	}
	if data.UI != nil && data.UI.SourceSHA1 != "" {
		genesisArgs = append(genesisArgs, "--uiDir", filepath.Join(lunarUIDir(), data.UI.SourceSHA1))
	}
	if server != "" {
		genesisArgs = append(genesisArgs, "--server", server)
	}
	genesisArgs = append(genesisArgs, "--uuid", uuidStr, "--username", username)

	cmd := append([]string{javaPath}, jvmArgs...)
	cmd = append(cmd, cpArg...)
	cmd = append(cmd, mainClass)
	cmd = append(cmd, genesisArgs...)
	return cmd, nil
}

// LunarLaunchOptions carries the Lunar-specific launch settings.
type LunarLaunchOptions struct {
	LaunchOptions
	Module string `json:"module"` // lunar | forge | fabric
}

// runParallel runs all steps concurrently and returns the first error.
func runParallel(steps ...func() error) error {
	errs := make([]error, len(steps))
	var wg sync.WaitGroup
	for i, s := range steps {
		wg.Add(1)
		go func(i int, s func() error) {
			defer wg.Done()
			errs[i] = s()
		}(i, s)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// LaunchLunar runs the full Lunar Client flow: fetch launch data, download
// artifacts/textures/UI/modpack/assets/JRE, write the fake accounts.json and
// build the javaagent, then start Genesis (mirror of lunar.py launch()).
func LaunchLunar(instName, version, module string, acc Account, opts LunarLaunchOptions, log LogFn, state StateFn, onProgress ProgressFn) error {
	if IsRunning() {
		return fmt.Errorf("game already running")
	}
	if acc.Name == "" {
		acc.Name = "Player"
	}
	online := acc.Type == "microsoft" && acc.AccessToken != ""
	accessToken := acc.AccessToken
	if accessToken == "" {
		accessToken = "0"
	}
	uuidStr := acc.ID
	if uuidStr == "" || uuidStr == "00000000-0000-0000-0000-000000000000" {
		uuidStr = uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+acc.Name)).String()
	}

	// ponytail: startup info (version/module/player) already visible in UI; no need to emit to log

	if _, err := lunarEnsureAgent(version, onProgress); err != nil {
		return err
	}

	data, err := fetchLunarLaunchData(version, module, opts.Server, online)
	if err != nil {
		return err
	}

	classpath, classpathNames, externalsNames, nativesDirs, err := lunarDownloadArtifacts(data.LaunchTypeData.Artifacts, log, onProgress)
	if err != nil {
		return err
	}
	// ponytail: classpath/external counts not shown during download

	lunarExtractWebosr(log)
	prog, flush := aggregateProgress(onProgress)
	var assetIndexID string
	steps := []func() error{
		func() error { return lunarDownloadTextures(data.Textures, log, prog) },
		func() error { return lunarDownloadUI(data.UI, log, prog) },
		func() error { return lunarInstallBaseModpack(instName, version, module, data.BaseModpack, log, prog) },
		func() error {
			var e error
			assetIndexID, e = lunarEnsureAssets(version, instName, log, prog)
			return e
		},
		func() error { return lunarEnsureVanilla(version, instName, log, prog) },
	}
	if err := runParallel(steps...); err != nil {
		return err
	}
	flush()
	log("  Asset index: " + assetIndexID)

	major, err := requiredJavaMajor(version)
	if err != nil {
		return err
	}
	javaPath, err := ensureClientJava(major, onProgress)
	if err != nil {
		return err
	}
	// ponytail: java path shown in progress, not duplicated to log

	if err := lunarEnsureFakeAccounts(acc.Name, uuidStr); err != nil {
		return err
	}

	cmdArgs, err := lunarBuildJavaCommand(javaPath, classpath, classpathNames, externalsNames, nativesDirs, data, version, module, opts.Server, accessToken, acc.Name, uuidStr, opts.LaunchOptions, instName)
	if err != nil {
		return err
	}

	cmd := exec.Command(windowlessJava(javaPath), cmdArgs[1:]...)
	cmd.Dir = lunarOfflineDir()
	hideWindow(cmd)
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	log("Java: " + windowlessJava(javaPath))

	if opts.PreLaunch != "" {
		log("Pre-launch: " + opts.PreLaunch)
		if err := runShell(opts.PreLaunch, log); err != nil {
			return fmt.Errorf("pre-launch command failed: %w", err)
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	return runGame(cmd, opts.LaunchOptions, log, state, stdout, stderr, nil, "Lunar "+version)
}
