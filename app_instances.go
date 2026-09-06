package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"multilauncherwails/launcher"
	"multilauncherwails/launcher/plugin"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) ListPlugins(instName string) []launcher.Plugin {
	dir, sub := a.contentLocation(instName, "mods")
	return launcher.ListFilesDir(dir, sub)
}

func (a *App) ListContent(instName, subfolder string) []launcher.Plugin {
	dir, sub := a.contentLocation(instName, subfolder)
	return launcher.ListFilesDir(dir, sub)
}

func (a *App) DownloadContent(instName, subfolder, url, filename string) error {
	dir, sub := a.contentLocation(instName, subfolder)
	return launcher.DownloadContentDir(dir, sub, url, filename)
}

func (a *App) DeleteContent(instName, subfolder, filename string) error {
	dir, sub := a.contentLocation(instName, subfolder)
	if err := launcher.DeleteContentDir(dir, sub, filename); err != nil {
		return err
	}
	if subfolder == "mods" {
		if client, _, _ := a.clientInstanceMeta(instName); client == "lunar" || client == "ogulniega" {
			launcher.MarkUserRemoved(filepath.Join(dir, sub), filename)
		}
	}
	return nil
}

func (a *App) ToggleContent(instName, subfolder, filename string, disable bool) error {
	dir, sub := a.contentLocation(instName, subfolder)
	return launcher.ToggleContentDir(dir, sub, filename, disable)
}

func (a *App) InspectMod(instName, subfolder, filename string) launcher.ModInfo {
	dir, sub := a.contentLocation(instName, subfolder)
	return launcher.InspectModDir(dir, sub, filename)
}

// contentLocation resolves where an instance's content subfolder lives.
// The "mods" subfolder of lunar instances maps onto
// instances/<name>/mods/<module>-<mcVersion> (where that instance's base
// modpack is installed), and Ogulniega instances map onto
// instances/<name>/mods/<entryName> from launcher.json (with OptiFine one
// level down in preinstalled/). Both are returned with an empty subfolder
// because the target already IS the mods folder. Everything else stays in
// the regular instance directory.
func (a *App) contentLocation(instName, subfolder string) (dir, sub string) {
	if subfolder == "mods" {
		if client, v, m := a.clientInstanceMeta(instName); client != "" {
			switch client {
			case "lunar":
				if v != "" {
					return launcher.LunarInstanceModsDir(instName, v, strings.ToLower(m)), ""
				}
			case "ogulniega":
				if sub, err := launcher.OgulniegaModsSubdir(v, m); err == nil && sub != "" {
					return filepath.Join(launcher.InstanceDir(instName), "mods", sub), ""
				}
			}
		}
	}
	return launcher.InstanceDir(instName), subfolder
}

func (a *App) clientInstanceMeta(instName string) (client, version, module string) {
	data, err := os.ReadFile(launcherStatePath())
	if err != nil {
		return "", "", ""
	}
	var s persistedState
	if json.Unmarshal(data, &s) != nil {
		return "", "", ""
	}
	var groups []struct {
		Instances []struct {
			Name string `json:"name"`
			Meta struct {
				Source    string `json:"source"`
				Client    string `json:"client"`
				MCVersion string `json:"mcVersion"`
				Module    string `json:"module"`
			} `json:"meta"`
		} `json:"instances"`
	}
	if json.Unmarshal(s.Groups, &groups) != nil {
		return "", "", ""
	}
	for _, g := range groups {
		for _, inst := range g.Instances {
			if inst.Name != instName {
				continue
			}
			if inst.Meta.Source == "lunar" {
				return "lunar", inst.Meta.MCVersion, inst.Meta.Module
			}
			if inst.Meta.Source == "client" {
				return inst.Meta.Client, inst.Meta.MCVersion, inst.Meta.Module
			}
			return "", "", ""
		}
	}
	return "", "", ""
}

// instanceMu serializes instance create/duplicate so concurrent UI actions
// (double-click, plugin triggers) cannot race on name de-duplication. The
// directory-level atomicity comes from launcher.CreateInstanceDir's single
// os.Mkdir; this mutex just keeps the higher-level flow orderly.
var instanceMu sync.Mutex

// CreateInstance creates the on-disk directory for a new instance and returns
// the final, de-duplicated name. Plugins can cancel the creation or rewrite
// the name via the "instance.beforeCreate" event. If the requested name is
// already in use, a numeric suffix is appended (e.g. "Foo", "Foo (2)",
// "Foo (3)") so each instance gets its own folder, worlds, options and
// screenshots — this is what prevents "Lunar Client 1.21.11 fabric" and
// "Lunar Client 1.21.11 no-of" from collapsing into a single shared folder.
func (a *App) CreateInstance(name string) (string, error) {
	for _, r := range plugin.Bus.Call("instance.beforeCreate", plugin.InstanceEvent{Name: name}) {
		if cancel, _ := r.Value["cancel"].(bool); cancel {
			reason, _ := r.Value["reason"].(string)
			return name, fmt.Errorf("instance creation canceled by plugin %s: %s", r.PluginID, reason)
		}
		if newName, _ := r.Value["rename"].(string); newName != "" && newName != name {
			name = newName
		}
	}

	instanceMu.Lock()
	final, err := launcher.CreateInstanceDir(name)
	instanceMu.Unlock()
	if err != nil {
		return name, err
	}
	plugin.Bus.Emit("instance.afterCreate", plugin.InstanceEvent{Name: final})
	return final, nil
}

func (a *App) DeleteInstance(name string) error {
	return os.RemoveAll(launcher.InstanceDir(name))
}

// RenameInstance renames an instance's directory and display name. The game
// must not be running (Windows locks the files). Name collisions are an
// error — unlike CreateInstance there is no silent suffixing.
func (a *App) RenameInstance(oldName, newName string) error {
	if launcher.IsRunning() {
		return fmt.Errorf("stop the game before renaming")
	}
	instanceMu.Lock()
	defer instanceMu.Unlock()
	return launcher.RenameInstanceDir(oldName, newName)
}

// instanceUsage is the set of shared files still needed after the doomed
// instances are gone: mcVersions in use and loader builds in use.
type instanceUsage struct {
	versions map[string]bool
	loaders  map[string]bool
}

// parseInstanceUsage reads the persisted groups and collects the mcVersions
// and loader builds used by every instance except the doomed ones. Every
// source (vanilla, modrinth, curseforge, lunar/feather/dawn clients) carries
// meta.mcVersion, so all of them guard the shared vanilla files.
func parseInstanceUsage(data []byte, doomed map[string]bool) (instanceUsage, error) {
	u := instanceUsage{versions: map[string]bool{}, loaders: map[string]bool{}}
	var s persistedState
	if err := json.Unmarshal(data, &s); err != nil {
		return u, err
	}
	var groups []struct {
		Instances []struct {
			Name string `json:"name"`
			Meta struct {
				MCVersion     string `json:"mcVersion"`
				Loader        string `json:"loader"`
				LoaderVersion string `json:"loaderVersion"`
			} `json:"meta"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(s.Groups, &groups); err != nil {
		return u, err
	}
	for _, g := range groups {
		for _, inst := range g.Instances {
			if doomed[inst.Name] {
				continue
			}
			if inst.Meta.MCVersion != "" {
				u.versions[inst.Meta.MCVersion] = true
			}
			if inst.Meta.Loader != "" && inst.Meta.Loader != "Vanilla" && inst.Meta.LoaderVersion != "" {
				u.loaders[inst.Meta.Loader+"\x00"+inst.Meta.LoaderVersion] = true
			}
		}
	}
	return u, nil
}

// PruneOrphanedVersions deletes the shared Minecraft files (versions,
// libraries, assets, loader jars) of the given deleted instances, but only
// when no remaining instance uses them. It is safe to call unconditionally:
// used versions are detected from the persisted state and skipped.
//
// The doomed names are excluded explicitly, so this works regardless of
// whether the frontend has already saved the updated groups to disk.
func (a *App) PruneOrphanedVersions(names []string) ([]launcher.PruneReport, error) {
	var out []launcher.PruneReport
	if len(names) == 0 {
		return out, nil
	}
	doomed := map[string]bool{}
	for _, n := range names {
		doomed[n] = true
	}
	data, err := os.ReadFile(launcherStatePath())
	if err != nil {
		return out, nil // no persisted state, nothing to guard or prune
	}
	u, err := parseInstanceUsage(data, doomed)
	if err != nil {
		return out, nil
	}

	// Collect each doomed instance's version exactly once.
	versions := map[string]bool{}
	loaders := map[string]bool{}
	var s persistedState
	if err := json.Unmarshal(data, &s); err == nil {
		var groups []struct {
			Instances []struct {
				Name string `json:"name"`
				Meta struct {
					MCVersion     string `json:"mcVersion"`
					Loader        string `json:"loader"`
					LoaderVersion string `json:"loaderVersion"`
				} `json:"meta"`
			} `json:"instances"`
		}
		if json.Unmarshal(s.Groups, &groups) == nil {
			for _, g := range groups {
				for _, inst := range g.Instances {
					if !doomed[inst.Name] || inst.Meta.MCVersion == "" || versions[inst.Meta.MCVersion] {
						continue
					}
					versions[inst.Meta.MCVersion] = true
					if inst.Meta.Loader != "" && inst.Meta.Loader != "Vanilla" && inst.Meta.LoaderVersion != "" {
						loaders[inst.Meta.Loader+"\x00"+inst.Meta.LoaderVersion] = true
					}
				}
			}
		}
	}

	var freed int64
	for v := range versions {
		rep, err := launcher.PruneOrphanedFiles(v, u.versions)
		if err != nil {
			continue
		}
		freed += rep.FreedBytes
		if len(rep.Removed) > 0 {
			out = append(out, rep)
		}
	}
	for key := range loaders {
		parts := strings.SplitN(key, "\x00", 2)
		rep, err := launcher.PruneLoaderJar(parts[0], parts[1], u.loaders)
		if err != nil {
			continue
		}
		freed += rep.FreedBytes
		if len(rep.Removed) > 0 {
			out = append(out, rep)
		}
	}
	if freed > 0 {
		a.emitLog(fmt.Sprintf("Pruned unused Minecraft files: freed %s", formatBytes(freed)))
	}
	return out, nil
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func (a *App) GetOptions(instName string) []launcher.Option {
	return launcher.GetOptions(instName)
}

func (a *App) SaveOptions(instName string, opts []launcher.Option) error {
	return launcher.SaveOptions(instName, opts)
}

func (a *App) OpenInstanceFolder(name string) error {
	return openFolder(launcher.InstanceDir(name))
}

func openFolder(dir string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		// Use shell association (cmd start) instead of explorer.exe directly,
		// so the user's default file manager (e.g. Files) is respected.
		cmd = exec.Command("cmd", "/c", "start", "", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}

func (a *App) DuplicateInstance(name string) (string, error) {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	src := launcher.InstanceDir(name)
	instancesDir := filepath.Join(launcher.Root(), "instances")
	newName := name + " (copy)"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(instancesDir, newName)); os.IsNotExist(err) {
			break
		}
		newName = fmt.Sprintf("%s (copy %d)", name, i)
	}
	if err := copyDir(src, filepath.Join(instancesDir, newName)); err != nil {
		return "", err
	}
	return newName, nil
}

func (a *App) ExportInstance(name string) (string, error) {
	src := launcher.InstanceDir(name)
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultDirectory: launcher.Root(),
		DefaultFilename:  name + ".zip",
		Title:            "Export instance as pack",
		Filters:          []runtime.FileFilter{{DisplayName: "ZIP archive", Pattern: "*.zip"}},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("export cancelled")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		path += ".zip"
	}
	return path, launcher.ZipDir(src, path)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		out.Close()
		return err
	})
}
