package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"multilauncherwails/launcher"
	"multilauncherwails/launcher/plugin"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) PickPluginFile() (string, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Select plugin archive",
		Filters: []runtime.FileFilter{{DisplayName: "Multi Launcher plugins", Pattern: "*.mlplugin"}},
	})
	if err != nil {
		return "", err
	}
	return p, nil
}

func (a *App) GetEnabledPlugins() []plugin.InstalledPlugin {
	if a.pluginManager == nil {
		return nil
	}
	return a.pluginManager.List()
}

func (a *App) GetPluginPermissions() []string {
	return plugin.AllPermissions
}

func (a *App) GetPluginCommands() map[string][]string {
	if a.pluginManager == nil {
		return nil
	}
	return a.pluginManager.Commands()
}

func (a *App) RunPluginCommand(pluginID, name string) (any, error) {
	if a.pluginManager == nil {
		return nil, fmt.Errorf("plugins not initialized")
	}
	return a.pluginManager.RunCommand(pluginID, name)
}

func (a *App) PluginSend(pluginID, name string, data any) error {
	if a.pluginManager == nil {
		return fmt.Errorf("plugins not initialized")
	}
	return a.pluginManager.DeliverMessage(pluginID, name, data)
}

func (a *App) GetPluginSettings(pluginID string) (map[string]any, error) {
	if a.pluginManager == nil {
		return nil, fmt.Errorf("plugins not initialized")
	}
	for _, p := range a.pluginManager.List() {
		if p.Manifest.ID != pluginID {
			continue
		}
		out := map[string]any{}
		for _, f := range p.Manifest.Settings {
			if f.Default != nil {
				out[f.Key] = f.Default
			}
		}
		saved, err := a.pluginManager.StorageGet(pluginID, "_settings")
		if err != nil {
			return out, nil
		}
		if m, ok := saved.(map[string]any); ok {
			for k, v := range m {
				out[k] = v
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("plugin %q not installed", pluginID)
}

func (a *App) SavePluginSettings(pluginID, settingsJSON string) error {
	if a.pluginManager == nil {
		return fmt.Errorf("plugins not initialized")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(settingsJSON), &m); err != nil {
		return fmt.Errorf("parse settings: %w", err)
	}
	return a.pluginManager.StorageSet(pluginID, "_settings", m)
}

func (a *App) PluginFetch(pluginID, method, url, body string) (map[string]any, error) {
	if a.pluginManager == nil {
		return nil, fmt.Errorf("plugins not initialized")
	}
	m := a.pluginManager.Manifest(pluginID)
	if m == nil {
		return nil, fmt.Errorf("plugin %q not installed", pluginID)
	}
	if !plugin.HasPermission(m.Permissions, plugin.PermNetworkCustom) {
		return nil, fmt.Errorf("permission %s required", plugin.PermNetworkCustom)
	}
	if err := plugin.CheckPublicURL(url); err != nil {
		return nil, err
	}
	if method == "" {
		method = "GET"
	}
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	client := plugin.SafeHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": resp.StatusCode, "body": string(data)}, nil
}

func (a *App) InspectPluginZip(zipPath string) (plugin.Manifest, error) {
	m, err := plugin.InspectZip(zipPath)
	if err != nil {
		return plugin.Manifest{}, err
	}
	return *m, nil
}

func (a *App) confirmInstall(m plugin.Manifest) bool {
	perms := "none"
	if len(m.Permissions) > 0 {
		perms = strings.Join(m.Permissions, ", ")
	}
	res, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Install plugin",
		Message:       fmt.Sprintf("Install %q v%s by %s?\n\nRequested permissions: %s", m.Name, m.Version, m.Author, perms),
		DefaultButton: "No",
	})
	return err == nil && res == "Yes"
}

func (a *App) rejectPlugin(reason error) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.ErrorDialog,
		Title:   "Plugin rejected",
		Message: fmt.Sprintf("This plugin may be dangerous (%s). It was not added.", reason),
	})
}

func (a *App) confirmPermIncrease(m plugin.Manifest, added []string) bool {
	res, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "New permissions requested",
		Message:       fmt.Sprintf("Update of %q to v%s adds NEW permissions:\n%s\n\nInstall anyway?", m.Name, m.Version, strings.Join(added, ", ")),
		DefaultButton: "No",
	})
	return err == nil && res == "Yes"
}

func (a *App) InstallPluginFromZip(zipPath string) (plugin.InstalledPlugin, error) {
	m, err := plugin.InspectZip(zipPath)
	if err != nil {
		return plugin.InstalledPlugin{}, err
	}
	if err := plugin.ScanArchive(zipPath); err != nil {
		a.rejectPlugin(err)
		return plugin.InstalledPlugin{}, fmt.Errorf("plugin rejected: %w", err)
	}
	if prev := a.pluginManager.Manifest(m.ID); prev != nil {
		added := plugin.AddedPermissions(prev.Permissions, m.Permissions)
		if len(added) > 0 && !a.confirmPermIncrease(*m, added) {
			return plugin.InstalledPlugin{}, fmt.Errorf("installation canceled")
		}
	}
	if !a.confirmInstall(*m) {
		return plugin.InstalledPlugin{}, fmt.Errorf("installation canceled")
	}
	tmp, err := os.MkdirTemp("", "mlplugin-*")
	if err != nil {
		return plugin.InstalledPlugin{}, err
	}
	defer os.RemoveAll(tmp)
	if err := plugin.InstallFromZip(zipPath, tmp); err != nil {
		return plugin.InstalledPlugin{}, err
	}
	p, err := a.pluginManager.Install(tmp)
	if err != nil {
		return plugin.InstalledPlugin{}, err
	}
	a.pluginManager.StopRuntime(p.Manifest.ID)
	if a.pluginManager.IsEnabled(p.Manifest.ID) {
		if err := a.pluginManager.StartPlugin(p.Manifest.ID); err != nil {
			return *p, err
		}
	}
	runtime.EventsEmit(a.ctx, "plugins:changed", nil)
	return *p, nil
}

func (a *App) InstallPluginFromURL(rawURL string) (plugin.InstalledPlugin, error) {
	baseURL, wantHash, err := plugin.ParsePinnedURL(rawURL)
	if err != nil {
		return plugin.InstalledPlugin{}, err
	}
	tmp, err := os.CreateTemp("", "mlplugin-*.mlplugin")
	if err != nil {
		return plugin.InstalledPlugin{}, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := launcher.DownloadFile(baseURL, tmp.Name(), nil); err != nil {
		return plugin.InstalledPlugin{}, err
	}
	if wantHash != "" {
		got, err := launcher.SHA256File(tmp.Name())
		if err != nil {
			return plugin.InstalledPlugin{}, err
		}
		if got != wantHash {
			a.rejectPlugin(fmt.Errorf("downloaded archive hash does not match the pinned sha256"))
			return plugin.InstalledPlugin{}, fmt.Errorf("sha256 mismatch: got %s, want %s", got, wantHash)
		}
	}
	return a.InstallPluginFromZip(tmp.Name())
}

func (a *App) SetPluginEnabled(id string, enabled bool) error {
	if err := a.pluginManager.SetEnabled(id, enabled); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "plugins:changed", nil)
	return nil
}

func (a *App) RemovePlugin(id string) error {
	if err := a.pluginManager.Remove(id); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "plugins:changed", nil)
	return nil
}

type ghEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
}

type VerifiedPlugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Category    string `json:"category"`
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
	DownloadURL string `json:"downloadUrl"`
}

type VerifiedCatalog struct {
	Repo       string                      `json:"repo"`
	Skipped    []string                    `json:"skipped,omitempty"`
	Categories map[string][]VerifiedPlugin `json:"categories"`
}

var ghRepoRe = regexp.MustCompile(`^(?:https://github\.com/)?([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

func ghOwnerRepo(input string) (string, error) {
	m := ghRepoRe.FindStringSubmatch(strings.TrimSpace(input))
	if m == nil {
		return "", fmt.Errorf("use owner/repo or a https://github.com/owner/repo URL")
	}
	return m[1] + "/" + m[2], nil
}

func ghGet(client *http.Client, url string, out any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "multilauncher-plugin-directory")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.Unmarshal(data, out)
}

func verifiedFromDir(client *http.Client, dirURL string) (VerifiedPlugin, error) {
	var entries []ghEntry
	if err := ghGet(client, dirURL, &entries); err != nil {
		return VerifiedPlugin{}, err
	}
	var archive, manifestURL string
	for _, e := range entries {
		switch {
		case e.Type == "file" && strings.HasSuffix(strings.ToLower(e.Name), ".mlplugin"):
			archive = e.DownloadURL
		case e.Type == "file" && e.Name == "plugin.json":
			manifestURL = e.DownloadURL
		}
	}
	if archive == "" || manifestURL == "" {
		return VerifiedPlugin{}, fmt.Errorf("directory needs a plugin.json and a .mlplugin archive")
	}
	var m plugin.Manifest
	if err := ghGet(client, manifestURL, &m); err != nil {
		return VerifiedPlugin{}, err
	}
	if err := m.Validate(); err != nil {
		return VerifiedPlugin{}, err
	}
	return VerifiedPlugin{
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Category:    m.Category,
		Author:      m.Author,
		Description: m.Description,
		DownloadURL: archive,
	}, nil
}

// verifiedPluginsRepo is the hardcoded, launcher-blessed plugin directory.
// FetchVerifiedPlugins falls back to it when the caller passes no repo.
const verifiedPluginsRepo = "Tomel999/ML-plugins"

func (a *App) FetchVerifiedPlugins() (VerifiedCatalog, error) {
	repoInput := verifiedPluginsRepo
	repo, err := ghOwnerRepo(repoInput)
	if err != nil {
		return VerifiedCatalog{}, err
	}
	client := plugin.SafeHTTPClient(30 * time.Second)
	base := "https://api.github.com/repos/" + repo + "/contents/plugins"
	var cats []ghEntry
	if err := ghGet(client, base, &cats); err != nil {
		return VerifiedCatalog{}, err
	}
	out := VerifiedCatalog{Repo: repo, Categories: map[string][]VerifiedPlugin{}}
	type job struct {
		cat string
		dir ghEntry
	}
	var jobs []job
	for _, c := range cats {
		if c.Type != "dir" || !plugin.ValidID(c.Name) {
			continue
		}
		var entries []ghEntry
		if err := ghGet(client, base+"/"+c.Name, &entries); err != nil {
			return out, err
		}
		for _, e := range entries {
			if e.Type == "dir" && plugin.ValidID(e.Name) {
				jobs = append(jobs, job{c.Name, e})
			}
		}
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, j := range jobs {
		wg.Add(1)
		go func(cat string, dir ghEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p, err := verifiedFromDir(client, base+"/"+cat+"/"+dir.Name)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Skipped = append(out.Skipped, cat+"/"+dir.Name)
				return
			}
			out.Categories[cat] = append(out.Categories[cat], p)
		}(j.cat, j.dir)
	}
	wg.Wait()
	for _, list := range out.Categories {
		sort.Slice(list, func(i, k int) bool { return list[i].ID < list[k].ID })
	}
	return out, nil
}
