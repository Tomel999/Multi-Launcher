package launcher

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"multilauncherwails/launcher/plugin"
)

type LaunchOptions struct {
	Memory     string   `json:"memory"`
	MinMemory  string   `json:"minMemory"`
	JvmArgs    string   `json:"jvmArgs"`
	JavaPath   string   `json:"javaPath"`
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Fullscreen bool     `json:"fullscreen"`
	Server     string   `json:"server"`
	Env        []string `json:"env"`
	PreLaunch  string   `json:"preLaunch"`
	PostExit   string   `json:"postExit"`
	// AccountID selects a specific account for this launch; empty = active.
	AccountID string `json:"accountId"`
}
type LogFn func(line string)

type StateFn func(running bool, pid int, errMsg string)

type runningProc struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	stop chan struct{}
	done chan struct{}
}

var proc = &runningProc{stop: make(chan struct{}), done: make(chan struct{})}

func IsRunning() bool {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	return proc.cmd != nil
}

func Stop() {
	proc.mu.Lock()
	cmd := proc.cmd
	proc.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}

func findJava(major int) (string, error) {
	if p := installedJava(major); p != "" {
		return p, nil
	}
	if major >= 17 {
		if p := newestInstalledJavaAtLeast(javaDir(), major); p != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("bundled java %d not found — run the launcher again to download it", major)
}

type launchContext struct {
	gameDir      string
	assetsDir    string
	assetIndexID string
	nativesDir   string
	clientJar    string
	libraryDir   string
	classpath    string
	mainClass    string
	username     string
	versionID    string
	versionType  string
	launcherName string
	launcherVer  string
	resolutionW  string
	resolutionH  string
	memory       string
}

func artifactKey(p string) string {
	p = filepath.ToSlash(p)
	i := strings.LastIndex(p, "libraries/")
	if i < 0 {
		return ""
	}
	parts := strings.Split(p[i+len("libraries/"):], "/")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:len(parts)-2], "/")
}

func dedupeClasspath(cp []string) []string {
	seen := make(map[string]bool, len(cp))
	out := make([]string, 0, len(cp))
	for i := len(cp) - 1; i >= 0; i-- {
		key := artifactKey(cp[i])
		if key == "" || !seen[key] {
			seen[key] = true
			out = append(out, cp[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func buildArgs(v *VersionMeta, classpath []string, instName string, acc Account, opts LaunchOptions) ([]string, error) {
	memory := opts.Memory
	if memory == "" {
		memory = "2G"
	}
	if acc.Name == "" {
		acc.Name = "Player"
	}
	if acc.UserType == "" {
		acc.UserType = "legacy"
	}
	if acc.AccessToken == "" {
		acc.AccessToken = "0"
	}
	resW, resH := opts.Width, opts.Height
	if resW <= 0 {
		resW = 1280
	}
	if resH <= 0 {
		resH = 720
	}
	jar := clientJarPath(v.ID)
	all := append([]string{jar}, dedupeClasspath(classpath)...)
	ndir := nativesDir(v.ID)
	if v.InheritsFrom != "" {
		ndir = nativesDir(v.InheritsFrom)
	}
	ctx := launchContext{
		gameDir:      InstanceDir(instName),
		assetsDir:    assetsDir(),
		assetIndexID: v.AssetIndex.ID,
		nativesDir:   ndir,
		clientJar:    jar,
		libraryDir:   librariesDir(),
		classpath:    strings.Join(all, string(os.PathListSeparator)),
		mainClass:    v.MainClass,
		username:     acc.Name,
		versionID:    v.ID,
		versionType:  "release",
		launcherName: "multilauncher",
		launcherVer:  "1.0",
		resolutionW:  fmt.Sprint(resW),
		resolutionH:  fmt.Sprint(resH),
		memory:       memory,
	}
	if ctx.mainClass == "" {
		ctx.mainClass = "net.minecraft.client.main.Main"
	}

	sub := func(s string) string {
		r := strings.NewReplacer(
			"${auth_player_name}", ctx.username,
			"${version_name}", ctx.versionID,
			"${game_directory}", ctx.gameDir,
			"${assets_root}", ctx.assetsDir,
			"${assets_index_name}", ctx.assetIndexID,
			"${auth_uuid}", acc.ID,
			"${auth_access_token}", acc.AccessToken,
			"${user_type}", acc.UserType,
			"${version_type}", ctx.versionType,
			"${natives_directory}", ctx.nativesDir,
			"${launcher_name}", ctx.launcherName,
			"${launcher_version}", ctx.launcherVer,
			"${classpath}", ctx.classpath,
			"${classpath_separator}", string(os.PathListSeparator),
			"${library_directory}", ctx.libraryDir,
			"${resolution_width}", ctx.resolutionW,
			"${resolution_height}", ctx.resolutionH,
			"${client_jar}", ctx.clientJar,
		)
		return r.Replace(s)
	}

	var jvmArgs, gameArgs []string
	if v.Arguments != nil {
		for _, raw := range v.Arguments.JVM {
			vals, ok := parseArg(raw)
			if !ok {
				continue
			}
			if vals.rules != nil && !rulesAllow(vals.rules) {
				continue
			}
			jvmArgs = append(jvmArgs, subArgs(vals.values, sub)...)
		}
		for _, raw := range v.Arguments.Game {
			vals, ok := parseArg(raw)
			if !ok {
				continue
			}
			if vals.rules != nil && !rulesAllow(vals.rules) {
				continue
			}
			gameArgs = append(gameArgs, subArgs(vals.values, sub)...)
		}
	} else if v.MinecraftArguments != "" {
		gameArgs = strings.Fields(sub(v.MinecraftArguments))
	}
	if !hasFlag(jvmArgs, "-Djava.library.path") {
		jvmArgs = append(jvmArgs, "-Djava.library.path="+ctx.nativesDir)
	}
	if !hasFlag(jvmArgs, "-cp") {
		jvmArgs = append(jvmArgs, "-cp", ctx.classpath)
	}
	heapArgs := []string{"-Xmx" + ctx.memory}
	if opts.MinMemory != "" {
		heapArgs = append(heapArgs, "-Xms"+opts.MinMemory)
	}
	if !hasPrefixFlag(jvmArgs, "-Xmx") {
		jvmArgs = append(heapArgs, jvmArgs...)
	}
	jvmArgs = append(jvmArgs, strings.Fields(opts.JvmArgs)...)

	if host, port := splitServer(opts.Server); host != "" {
		gameArgs = append(gameArgs, "--server", host, "--port", port)
	}
	if opts.Fullscreen {
		gameArgs = append(gameArgs, "--fullscreen")
	}

	return append(append(jvmArgs, ctx.mainClass), gameArgs...), nil
}

func splitServer(addr string) (string, string) {
	if i := strings.LastIndexByte(addr, ':'); i > 0 && !strings.Contains(addr[i+1:], ":") {
		return strings.Trim(addr[:i], "[]"), addr[i+1:]
	}
	return strings.Trim(addr, "[]"), "25565"
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func hasPrefixFlag(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

type parsedArg struct {
	rules  []Rule
	values []string
}

func parseArg(raw json.RawMessage) (parsedArg, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return parsedArg{values: []string{s}}, true
	}
	var obj struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return parsedArg{}, false
	}
	var one string
	if err := json.Unmarshal(obj.Value, &one); err == nil {
		return parsedArg{rules: obj.Rules, values: []string{one}}, true
	}
	var many []string
	if err := json.Unmarshal(obj.Value, &many); err == nil {
		return parsedArg{rules: obj.Rules, values: many}, true
	}
	return parsedArg{}, false
}

func subArgs(values []string, sub func(string) string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, sub(v))
	}
	return out
}

func Launch(version, instName string, acc Account, classpath []string, mainClass, javaPath string, opts LaunchOptions, log LogFn, state StateFn) error {
	v, err := GetVersionMeta(version)
	if err != nil {
		return err
	}
	if mainClass != "" {
		v.MainClass = mainClass
	}
	if javaPath == "" {
		major, err := RequiredJavaMajor(v)
		if err != nil {
			return err
		}
		javaPath, err = findJava(major)
		if err != nil {
			return err
		}
	}
	args, err := buildArgs(v, classpath, instName, acc, opts)
	if err != nil {
		return err
	}
	cmd := exec.Command(windowlessJava(javaPath), args...)
	cmd.Dir = InstanceDir(instName)
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
	plugin.Bus.Emit("launch.beforeStart", plugin.LaunchEvent{Instance: instName, Version: version})
	return runGame(cmd, opts, log, state, stdout, stderr, nil, version)
}

func runGame(cmd *exec.Cmd, opts LaunchOptions, log LogFn, state StateFn, stdout, stderr io.Reader, onExit func(), label string) error {
	if err := cmd.Start(); err != nil {
		if onExit != nil {
			onExit()
		}
		return err
	}
	proc.mu.Lock()
	proc.cmd = cmd
	proc.done = make(chan struct{})
	proc.mu.Unlock()
	state(true, cmd.Process.Pid, "")
	log("Launching " + label + " (pid " + fmt.Sprint(cmd.Process.Pid) + ")")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			log(sc.Text())
		}
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			log(sc.Text())
		}
	}()
	go func() {
		cmd.Wait()
		wg.Wait()
		if onExit != nil {
			onExit()
		}
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		proc.mu.Lock()
		proc.cmd = nil
		close(proc.done)
		proc.mu.Unlock()
		state(false, 0, "")
		plugin.Bus.Emit("launch.afterExit", plugin.LaunchEvent{Version: label, ExitCode: exitCode})
		if opts.PostExit != "" {
			log("Post-exit: " + opts.PostExit)
			runShell(opts.PostExit, log)
		}
	}()
	return nil
}

func runShell(cmdline string, log LogFn) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdline)
	} else {
		cmd = exec.Command("/bin/sh", "-c", cmdline)
	}
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			log(line)
		}
	}
	return err
}

func LaunchVanillaFlow(instName, version, loader, loaderVersion string, opts LaunchOptions, log LogFn, state StateFn, onProgress ProgressFn) {
	patched := ApplyLaunchHooks(log, state, instName, version, opts)
	if patched == nil {
		return
	}
	opts = *patched
	v, err := GetVersionMeta(version)
	if err != nil {
		log("Version error: " + err.Error())
		state(false, 0, err.Error())
		return
	}
	major, err := RequiredJavaMajor(v)
	if err != nil {
		log("Java error: " + err.Error())
		state(false, 0, err.Error())
		return
	}
	javaPath := opts.JavaPath
	if javaPath == "" {
		javaPath, err = EnsureJava(major, onProgress)
		if err != nil {
			log("Java error: " + err.Error())
			state(false, 0, err.Error())
			return
		}
	} else if _, statErr := os.Stat(javaPath); statErr != nil {
		log("Java error: custom path not found: " + javaPath)
		state(false, 0, "custom java path not found: "+javaPath)
		return
	}
	_, classpath, err := EnsureInstalled(version, onProgress)
	if err != nil {
		log("Install error: " + err.Error())
		state(false, 0, err.Error())
		return
	}
	mainClass := ""
	launchVersion := version
	if loader != "" && loader != "Vanilla" {
		lv, lc, mc, err := EnsureLoader(loader, version, loaderVersion, javaPath, onProgress)
		if err != nil {
			log("Loader error: " + err.Error())
			state(false, 0, err.Error())
			return
		}
		launchVersion = lv
		classpath = append(classpath, lc...)
		mainClass = mc
	}
	if err := Launch(launchVersion, instName, ResolveAccount(opts.AccountID), classpath, mainClass, javaPath, opts, log, state); err != nil {
		log("Launch error: " + err.Error())
		state(false, 0, err.Error())
	}
}
