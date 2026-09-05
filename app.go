package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"multilauncherwails/internal/updater"
	"multilauncherwails/internal/version"
	"multilauncherwails/launcher"
	"multilauncherwails/launcher/plugin"
	"multilauncherwails/panorama"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateOwner / updateRepo are the GitHub repository hosting releases.
//
// CHANGE THESE if the project is renamed or moved. The updater reads releases
// from https://api.github.com/repos/<owner>/<repo>/releases/latest.
const (
	updateOwner = "Tomel999"
	updateRepo  = "Multi-Launcher"
)

const (
	// updateCheckDelay keeps the startup check off the critical path: the UI
	// is interactive first, then we look for an update.
	updateCheckDelay = 3 * time.Second
	// updateCheckTimeout bounds a single check so a hanging connection can
	// never wedge the launcher.
	updateCheckTimeout = 15 * time.Second
)

// UpdateProgress is the payload of the "update:progress" Wails event.
type UpdateProgress struct {
	Percent    int    `json:"percent"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	AssetName  string `json:"assetName"`
}

// UpdateError is the payload of the "update:error" Wails event.
type UpdateError struct {
	Message string `json:"message"`
	Kind    string `json:"kind"`
	Stage   string `json:"stage"`
}

type App struct {
	ctx           context.Context
	curseforgeKey string
	msLoginCancel context.CancelFunc
	launchMu      sync.Mutex
	installMu     sync.Mutex
	pluginManager *plugin.Manager
	safeMode      bool

	mu       sync.RWMutex
	progress launcher.Progress
	state    launcher.State

	logMu   sync.Mutex
	logs    []string
	logSeq  int
	logRead int

	// Auto-update state. Held under updateMu; the download itself runs in a
	// background goroutine so the UI never blocks.
	updateMu       sync.Mutex
	updateChecker  *updater.Checker
	updateUpdater  *updater.Updater
	updateAsset    updater.Asset
	updateInfo     *updater.UpdateInfo
	updatePath     string // where the verified download was staged
	updateBusy     bool
	updateCancel   context.CancelFunc
}

var (
	logFileMu      sync.Mutex
	logFile        *os.File
	logWriteFailed bool // warn only once per process
)

const maxLogSize = 5 << 20 // rotate launch.log at 5 MiB

// openLogFile lazily opens the on-disk launch log under the launcher data
// dir. Returns nil if the file cannot be opened; callers should just skip.
// The file is rotated: when launch.log exceeds maxLogSize it is renamed to
// launch.log.1 (replacing any previous rotation) and a fresh file is opened,
// so the log cannot grow without bound.
func openLogFile() *os.File {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile != nil {
		return logFile
	}
	dir := filepath.Join(filepath.Dir(launcher.Root()), "logs")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "launch.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		_ = os.Rename(path, filepath.Join(dir, "launch.log.1"))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	logFile = f
	return logFile
}

// resolveCurseForgeKey returns the CurseForge API key. The key can be
// supplied via the CURSEFORGE_API_KEY environment variable (recommended) or
// embedded at build time. The embedded fallback is intentionally
// lightly-obfuscated only; a committed key in a public repository must be
// treated as public — rotate it on the CurseForge developer portal and set
// the env var instead of relying on this fallback.
func resolveCurseForgeKey() string {
	if k := os.Getenv("CURSEFORGE_API_KEY"); k != "" {
		return k
	}
	return "$2a$10$P4u6n0rSzrPMVhLAi5kGj.nVVMQo5oy/Jz3Y6/59Efodnhjz9gLbe"
}

var CurseForgeKey = resolveCurseForgeKey()

func NewApp() *App {
	return &App{
		curseforgeKey: CurseForgeKey,
		safeMode:      hasSafeModeFlag(),
		updateChecker: updater.NewChecker(updateOwner, updateRepo, version.Version),
	}
}

func hasSafeModeFlag() bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, "-safe") || strings.EqualFold(arg, "--safe") {
			return true
		}
	}
	return false
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	launcher.Migrate()
	launcher.LoadAccounts()
	a.applyStoredConcurrency()
	a.initPlugins()
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		for _, p := range paths {
			if !strings.EqualFold(filepath.Ext(p), ".mlplugin") {
				continue
			}
			m, err := plugin.InspectZip(p)
			if err != nil {
				a.emitLog("Plugin install failed: " + err.Error())
				continue
			}
			runtime.EventsEmit(a.ctx, "plugins:dropRequest", map[string]any{
				"path":     p,
				"manifest": m,
			})
		}
	})
	// Look for an update shortly after launch, off the startup path. Any
	// failure here is logged and otherwise ignored: the launcher must stay
	// usable when GitHub is unreachable.
	a.scheduleUpdateCheck()
}

func (a *App) initPlugins() {
	a.pluginManager = plugin.NewManager(plugin.Bus, pluginsRoot())
	a.pluginManager.SetLogger(func(pluginID, msg string) {
		a.emitLog("[" + pluginID + "] " + msg)
	})
	a.registerPluginProviders()
	if a.safeMode {
		a.pluginManager.SetSafeMode(true)
		a.emitLog("Safe mode: plugins are disabled for this session")
		return
	}
	a.pluginManager.Start()
}

func (a *App) registerPluginProviders() {
	type instanceInfo struct {
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Group string         `json:"group,omitempty"`
		Meta  map[string]any `json:"meta,omitempty"`
	}
	instances := func() []instanceInfo {
		data, err := os.ReadFile(launcherStatePath())
		if err != nil {
			return nil
		}
		var s persistedState
		if json.Unmarshal(data, &s) != nil {
			return nil
		}
		var groups []struct {
			Name      string         `json:"name"`
			Instances []instanceInfo `json:"instances"`
		}
		if json.Unmarshal(s.Groups, &groups) != nil {
			return nil
		}
		out := []instanceInfo{}
		for _, g := range groups {
			for _, inst := range g.Instances {
				inst.Group = g.Name
				out = append(out, inst)
			}
		}
		return out
	}
	str := func(args any) string {
		s, _ := args.(string)
		return s
	}

	p := a.pluginManager
	p.RegisterProvider("instances.list", func(string, any) (any, error) { return instances(), nil })
	p.RegisterProvider("instances.get", func(_ string, args any) (any, error) {
		name := str(args)
		for _, inst := range instances() {
			if inst.Name == name {
				return inst, nil
			}
		}
		return nil, nil
	})
	p.RegisterProvider("mods.list", func(_ string, args any) (any, error) {
		return launcher.ListPlugins(str(args)), nil
	})
	p.RegisterProvider("worlds.list", func(_ string, args any) (any, error) {
		return launcher.ListWorlds(str(args)), nil
	})
	p.RegisterProvider("accounts.list", func(string, any) (any, error) {
		return pubAccounts(), nil
	})
	p.RegisterProvider("logs.tail", func(_ string, args any) (any, error) {
		n := 100
		switch v := args.(type) {
		case float64:
			n = int(v)
		case int:
			n = v
		}
		return a.LogsTail(n), nil
	})
	p.RegisterProvider("ui.emit", func(pluginID string, args any) (any, error) {
		m, ok := args.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected {name, data}")
		}
		name, _ := m["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		runtime.EventsEmit(a.ctx, "plugin:"+pluginID+":"+name, m["data"])
		return nil, nil
	})
	p.RegisterProvider("launch.start", func(pluginID string, args any) (any, error) {
		m, ok := args.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected {instance}")
		}
		name, _ := m["instance"].(string)
		if name == "" {
			return nil, fmt.Errorf("instance is required")
		}
		a.emitLog("[" + pluginID + "] launch.start " + name)
		var meta map[string]any
		for _, inst := range instances() {
			if inst.Name == name {
				meta = inst.Meta
				break
			}
		}
		if meta == nil {
			return nil, fmt.Errorf("instance %q not found", name)
		}
		version, _ := meta["mcVersion"].(string)
		if version == "" {
			return nil, fmt.Errorf("instance %q has no mcVersion recorded", name)
		}
		loader, _ := meta["loader"].(string)
		loaderVersion, _ := meta["loaderVersion"].(string)
		if err := a.LaunchInstance(name, version, loader, loaderVersion, launcher.LaunchOptions{}); err != nil {
			return nil, err
		}
		return map[string]any{"started": true}, nil
	})
	p.RegisterProvider("app.state", func(string, any) (any, error) {
		s := a.GetState()
		return map[string]any{"running": s.Running, "pid": s.PID, "error": s.Error}, nil
	})
	p.RegisterProvider("open.folder", func(pluginID string, args any) (any, error) {
		m, ok := args.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected {instance}")
		}
		name, _ := m["instance"].(string)
		if name == "" {
			return nil, fmt.Errorf("instance is required")
		}
		a.emitLog("[" + pluginID + "] open.folder " + name)
		return nil, a.OpenInstanceFolder(name)
	})
	p.RegisterProvider("fs.read", func(_ string, args any) (any, error) {
		inst, rel, err := fsArgs(args)
		if err != nil {
			return nil, err
		}
		path, err := safeInstancePath(inst, rel)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(data) > 1<<20 {
			return nil, fmt.Errorf("file too large to read (%d bytes, max 1 MiB)", len(data))
		}
		return string(data), nil
	})
	p.RegisterProvider("fs.list", func(_ string, args any) (any, error) {
		inst, rel, err := fsArgs(args)
		if err != nil {
			return nil, err
		}
		path, err := safeInstancePath(inst, rel)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names, nil
	})
	p.RegisterProvider("fs.write", func(pluginID string, args any) (any, error) {
		m, ok := args.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected {instance, path, content}")
		}
		inst, _ := m["instance"].(string)
		rel, _ := m["path"].(string)
		content, _ := m["content"].(string)
		if inst == "" || rel == "" {
			return nil, fmt.Errorf("instance and path are required")
		}
		path, err := safeInstancePath(inst, rel)
		if err != nil {
			return nil, err
		}
		if inModsDir(inst, path) {
			pm := a.pluginManager.Manifest(pluginID)
			if pm == nil || !plugin.HasPermission(pm.Permissions, plugin.PermFSModsWrite) {
				return nil, fmt.Errorf("permission %s required to write into the mods folder", plugin.PermFSModsWrite)
			}
		}
		a.emitLog(fmt.Sprintf("[%s] fs.write %s/%s", pluginID, inst, rel))
		if len(content) > 4<<20 {
			return nil, fmt.Errorf("content too large to write (%d bytes, max 4 MiB)", len(content))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(path, []byte(content), 0o644)
	})
}

func inModsDir(inst, path string) bool {
	mods := strings.ToLower(filepath.Join(launcher.InstanceDir(inst), "mods"))
	p := strings.ToLower(filepath.Clean(path))
	return p == mods || strings.HasPrefix(p, mods+string(os.PathSeparator))
}

func fsArgs(args any) (inst, rel string, err error) {
	m, ok := args.(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("expected {instance, path}")
	}
	inst, _ = m["instance"].(string)
	rel, _ = m["path"].(string)
	if inst == "" || rel == "" {
		return "", "", fmt.Errorf("instance and path are required")
	}
	return inst, rel, nil
}

func safeInstancePath(inst, rel string) (string, error) {
	base := launcher.InstanceDir(inst)
	p := filepath.Clean(filepath.Join(base, filepath.FromSlash(rel)))
	if p != base && !strings.HasPrefix(p, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes instance directory")
	}
	return p, nil
}

func pluginsRoot() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, ".multilauncher", "plugins")
}

func (a *App) applyStoredConcurrency() {
	data, err := os.ReadFile(launcherStatePath())
	if err != nil {
		return
	}
	var s persistedState
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	var settings struct {
		DLMode          string `json:"dlMode"`
		ConcurrencyAuto *bool  `json:"concurrencyAuto"`
		Concurrency     int    `json:"concurrency"`
	}
	if err := json.Unmarshal(s.Settings, &settings); err != nil {
		return
	}
	mode := settings.DLMode
	if mode == "" {
		switch {
		case settings.ConcurrencyAuto != nil && *settings.ConcurrencyAuto:
			mode = "auto"
		case settings.ConcurrencyAuto != nil && !*settings.ConcurrencyAuto:
			mode = "manual"
		default:
			mode = "off"
		}
	}
	var n int
	switch mode {
	case "auto":
		n = 0
	case "manual":
		n = settings.Concurrency
		if n <= 0 {
			n = 8
		}
	default:
		n = 1
	}
	launcher.SetDownloadConcurrency(n)
}

func (a *App) emitProgress(p launcher.Progress) {
	a.mu.Lock()
	a.progress = p
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "mc:progress", p)
}

func (a *App) emitLog(line string) {
	a.logMu.Lock()
	a.logs = append(a.logs, line)
	a.logSeq++
	if len(a.logs) > 500 {
		a.logs = a.logs[len(a.logs)-500:]
	}
	a.logMu.Unlock()
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "mc:log", launcher.LogLine{Line: line})
	if f := openLogFile(); f != nil {
		if _, err := f.WriteString(line + "\n"); err != nil && !logWriteFailed {
			// Best-effort only; surface the first failure so a read-only
			// data dir or full disk is not silently ignored, then stay quiet.
			logWriteFailed = true
			println("launch.log write failed:", err.Error())
		}
	}
}

func (a *App) emitState(running bool, pid int, errMsg string) {
	s := launcher.State{Running: running, PID: pid, Error: errMsg}
	a.mu.Lock()
	a.state = s
	if running {
		a.progress = launcher.Progress{}
	}
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "mc:state", s)
}

func (a *App) resetLaunch() {
	a.mu.Lock()
	a.progress = launcher.Progress{}
	a.state = launcher.State{}
	a.mu.Unlock()
	a.logMu.Lock()
	a.logs = nil
	a.logSeq = 0
	a.logRead = 0
	a.logMu.Unlock()
}

func (a *App) GetProgress() launcher.Progress {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.progress
}

func (a *App) GetState() launcher.State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

func (a *App) LogsTail(n int) []string {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if n <= 0 || n > len(a.logs) {
		n = len(a.logs)
	}
	out := make([]string, n)
	copy(out, a.logs[len(a.logs)-n:])
	return out
}

func (a *App) GetLogs() []string {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.logRead >= len(a.logs) {
		return nil
	}
	out := make([]string, len(a.logs)-a.logRead)
	copy(out, a.logs[a.logRead:])
	a.logRead = len(a.logs)
	return out
}

func (a *App) GetPanoramaVersions() ([]panorama.VersionEntry, error) {
	m, err := panorama.GetManifest()
	if err != nil {
		return nil, err
	}
	return m.Versions, nil
}

func (a *App) GetPanorama(version string) ([6]string, error) {
	return panorama.FetchPanoramas(version)
}

func (a *App) HttpGet(rawURL string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, rawURL)
	}
	return string(body), nil
}

func launcherStatePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, ".multilauncher", "config")
	os.MkdirAll(p, 0o755)
	return filepath.Join(p, "launcher.json")
}

type persistedState struct {
	Settings json.RawMessage `json:"settings"`
	Groups   json.RawMessage `json:"groups"`
}

func (a *App) GetPersistedState() (string, error) {
	data, err := os.ReadFile(launcherStatePath())
	if err != nil {
		return "{}", nil
	}
	return string(data), nil
}

func (a *App) SaveState(settings, groups string) error {
	s := persistedState{Settings: json.RawMessage(settings), Groups: json.RawMessage(groups)}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(launcherStatePath(), data, 0o644)
}

func (a *App) DeleteLunarFolder() error {
	return os.RemoveAll(launcher.LunarDataDir())
}

func (a *App) DeleteFeatherFolder() error {
	return os.RemoveAll(launcher.FeatherDataDir())
}

func (a *App) DeleteDawnFolder() error {
	return os.RemoveAll(launcher.DawnDataDir())
}

// ---------------------------------------------------------------------------
// Auto-update
//
// By default updates are silent: a newer release is downloaded in the
// background after startup and installed on shutdown (no relaunch, no clicks).
// Set MULTILAUNCHER_NO_AUTO_UPDATE=1 for the classic flow, where the frontend
// drives three bound methods plus four events:
//
//	CheckForUpdate()  -> *updater.UpdateInfo     (also emitted at startup)
//	DownloadUpdate()  -> starts a background download
//	ApplyUpdate()     -> installs the staged download and quits
//
//	"update:available"  *updater.UpdateInfo (with .managed set for
//	                      manager-owned installs)
//	"update:progress"   UpdateProgress
//	"update:downloaded" {"path": string}
//	"update:error"      UpdateError
// ---------------------------------------------------------------------------

// GetAppVersion returns the running build's version, or "dev" when it was not
// stamped at build time.
func (a *App) GetAppVersion() string {
	return version.Version
}

// CheckForUpdate asks GitHub whether a newer release exists for this platform.
// It returns nil when the app is already up to date — that is the normal case
// and is not an error.
//
// Any failure is returned as an error and must be treated as non-fatal by the
// caller: the launcher works fine without updates.
func (a *App) CheckForUpdate() (*updater.UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	return a.checkForUpdate(ctx)
}

// checkForUpdate performs the check and, when an update exists, remembers the
// release and asset so DownloadUpdate can proceed without a second API call.
func (a *App) checkForUpdate(ctx context.Context) (*updater.UpdateInfo, error) {
	a.updateMu.Lock()
	checker := a.updateChecker
	a.updateMu.Unlock()
	if checker == nil {
		return nil, errors.New("updates are not configured for this build")
	}

	info, err := checker.CheckForUpdate(ctx)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil // up to date
	}

	rel := checker.LastRelease()
	if rel == nil {
		return nil, errors.New("update metadata is unavailable")
	}
	asset, err := updater.SelectAssetForPlatform(*rel)
	if err != nil {
		return nil, err
	}

	u := checker.NewUpdater()
	u.SetRelease(rel)

	a.updateMu.Lock()
	a.updateUpdater = u
	a.updateAsset = asset
	a.updateInfo = info
	a.updatePath = ""
	a.updateMu.Unlock()
	return info, nil
}

// scheduleUpdateCheck runs one update check a few seconds after startup.
//
// In auto mode (the default) a portable install downloads the update
// silently and installs it on shutdown — the user never clicks anything.
// Managed installs (AUR, Flatpak, ...) either update through the host manager
// or surface an "update:available" event carrying manager instructions, and
// notify mode (MULTILAUNCHER_NO_AUTO_UPDATE=1) keeps the classic modal flow.
//
// It never blocks startup and never surfaces errors to the user: unreachable
// networks, unpublished releases and missing platform builds are all logged
// and ignored.
func (a *App) scheduleUpdateCheck() {
	if !updateChecksEnabled() {
		return
	}
	go func() {
		select {
		case <-time.After(updateCheckDelay):
		case <-a.ctx.Done():
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()

		info, err := a.checkForUpdate(ctx)
		if err != nil {
			// "Nothing published for this platform" is expected, not a fault.
			if !updater.IsNotFound(err) {
				a.emitLog("Update check failed: " + err.Error())
			}
			return
		}
		if info == nil {
			return
		}
		a.emitLog(fmt.Sprintf("Update available: %s (current %s)", info.Version, info.Current))

		if m := updater.DetectManagedExe(); m != nil {
			a.handleManagedUpdate(info, m)
			return
		}

		if !autoUpdateEnabled() {
			runtime.EventsEmit(a.ctx, "update:available", info)
			return
		}

		if err := a.DownloadUpdate(); err != nil {
			a.emitLog("Silent update download failed: " + err.Error())
			return
		}
		a.emitLog(fmt.Sprintf("Downloading %s in the background; it installs on quit", info.Version))
	}()
}

// handleManagedUpdate deals with an update for a manager-owned install. When
// the host manager can be driven automatically (Flatpak via flatpak-spawn) it
// runs silently in the background; otherwise the frontend is told to show the
// manager's update command instead of an "Update now" button.
func (a *App) handleManagedUpdate(info *updater.UpdateInfo, m *updater.Managed) {
	info.Managed = m
	go func() {
		handled, err := updater.TryHostUpdate(m)
		if !handled {
			runtime.EventsEmit(a.ctx, "update:available", info)
			return
		}
		if err != nil {
			a.emitUpdateError("install", err)
			return
		}
		a.emitLog("Update applied by the host package manager (" + m.Name + ")")
	}()
}

// DownloadUpdate starts downloading the release in the background and returns
// immediately. Progress is reported via "update:progress", completion via
// "update:downloaded", and any failure via "update:error".
//
// The download is streamed to a staging file and verified against the
// release's "<asset>.sha256" sidecar before it is published, so a corrupt or
// tampered download never reaches the installation.
func (a *App) DownloadUpdate() error {
	a.updateMu.Lock()
	if a.updateBusy {
		a.updateMu.Unlock()
		return errors.New("an update is already downloading")
	}
	asset := a.updateAsset
	u := a.updateUpdater
	if u == nil || asset.Name == "" {
		a.updateMu.Unlock()
		return errors.New("no update is staged — run CheckForUpdate first")
	}
	a.updateBusy = true
	a.updateMu.Unlock()

	dir, err := updateCacheDir()
	if err != nil {
		a.setUpdateBusy(false)
		a.emitUpdateError("download", err)
		return err
	}
	dest := filepath.Join(dir, asset.Name)

	ctx, cancel := context.WithCancel(context.Background())
	a.updateMu.Lock()
	a.updateCancel = cancel
	a.updateMu.Unlock()

	go func() {
		defer cancel()
		defer a.setUpdateBusy(false)

		err := u.Download(ctx, asset, dest, func(downloaded, total int64) {
			pct := 0
			if total > 0 {
				pct = int(downloaded * 100 / total)
				if pct > 100 {
					pct = 100
				}
			}
			runtime.EventsEmit(a.ctx, "update:progress", UpdateProgress{
				Percent:    pct,
				Downloaded: downloaded,
				Total:      total,
				AssetName:  asset.Name,
			})
		})
		if err != nil {
			a.emitUpdateError("download", err)
			return
		}

		a.updateMu.Lock()
		a.updatePath = dest
		a.updateMu.Unlock()
		runtime.EventsEmit(a.ctx, "update:downloaded", map[string]any{"path": dest})
	}()
	return nil
}

// ApplyUpdate installs the verified download and quits so the updated build
// can start.
//
// On Windows the running .exe cannot overwrite itself, so the swap is handed
// to a detached batch script that completes after this process exits. On macOS
// the .app bundle is replaced and reopened; on Linux the binary is renamed
// over atomically.
func (a *App) ApplyUpdate() error {
	a.updateMu.Lock()
	path := a.updatePath
	a.updateMu.Unlock()
	if path == "" {
		return errors.New("no verified update is ready to install")
	}
	if err := updater.RejectIfManaged(); err != nil {
		a.emitUpdateError("install", err)
		return err
	}
	if err := updater.Install(path); err != nil {
		a.emitUpdateError("install", err)
		return err
	}
	// The platform installer has already spawned the replacement process
	// (or, on Windows, a script that will once we exit).
	runtime.Quit(a.ctx)
	return nil
}

// shutdown installs a silently-staged update, if any, without relaunching.
// Wails calls it synchronously on quit, so the file swap completes before the
// process exits. Failures are logged only: the user is leaving, and the next
// launch re-checks anyway.
func (a *App) shutdown(ctx context.Context) {
	a.updateMu.Lock()
	cancel := a.updateCancel
	path := a.updatePath
	a.updatePath = ""
	a.updateCancel = nil
	a.updateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if path == "" {
		return
	}
	a.emitLog("Installing staged update on shutdown")
	if err := updater.InstallWithoutRelaunch(path); err != nil {
		a.emitLog("Staged update failed: " + err.Error())
		return
	}
	a.emitLog("Update installed; it takes effect on next launch")
}
// setUpdateBusy flips the download guard.
func (a *App) setUpdateBusy(busy bool) {
	a.updateMu.Lock()
	a.updateBusy = busy
	a.updateMu.Unlock()
}

// emitUpdateError reports a failure to the frontend with a stable kind string
// so the UI can choose sensible wording, and logs it for diagnosis.
func (a *App) emitUpdateError(stage string, err error) {
	kind := "unknown"
	var ue *updater.Error
	if errors.As(err, &ue) {
		kind = string(ue.Kind)
	}
	a.emitLog(fmt.Sprintf("Update %s failed: %v", stage, err))
	runtime.EventsEmit(a.ctx, "update:error", UpdateError{
		Message: err.Error(),
		Kind:    kind,
		Stage:   stage,
	})
}

// updateCacheDir returns the directory holding staged downloads. It is a cache
// location rather than the install directory so a download never pollutes the
// .app bundle (macOS) or a possibly read-only Program Files tree (Windows).
func updateCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, ".multilauncher", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// envFlag reports whether an environment variable is set to a truthy value.
// Unset, "0", "false" and "no" are all false.
func envFlag(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// autoUpdateEnabled reports whether updates download silently and install on
// shutdown (the default). Set MULTILAUNCHER_NO_AUTO_UPDATE=1 to keep the
// classic flow: a modal prompt with "Update now" / "Later" buttons.
func autoUpdateEnabled() bool {
	return !envFlag("MULTILAUNCHER_NO_AUTO_UPDATE")
}

// updateChecksEnabled reports whether the automatic startup check should run.
//
// It is disabled by MULTILAUNCHER_NO_UPDATE_CHECK=1, and disabled by default on
// dev builds (which have no real version and would otherwise be replaced by a
// release binary mid-development). Set MULTILAUNCHER_UPDATE_CHECK_DEV=1 to opt
// a dev build in.
func updateChecksEnabled() bool {
	if envFlag("MULTILAUNCHER_NO_UPDATE_CHECK") {
		return false
	}
	if version.IsDev(version.Version) {
		return envFlag("MULTILAUNCHER_UPDATE_CHECK_DEV")
	}
	return true
}
