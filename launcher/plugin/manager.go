package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type providerFunc func(pluginID string, args any) (any, error)

type InstalledPlugin struct {
	Manifest *Manifest `json:"manifest"`
	Dir      string    `json:"dir"`
	Enabled  bool      `json:"enabled"`
}

type Manager struct {
	bus      *EventBus
	root     string
	log      func(pluginID, msg string)
	safeMode bool

	mu        sync.Mutex
	logMu     sync.RWMutex
	enabled   map[string]bool
	runtimes  map[string]*Runtime
	wasms     map[string]*WasmRuntime
	providers map[string]providerFunc
}

func NewManager(bus *EventBus, root string) *Manager {
	m := &Manager{
		bus:       bus,
		root:      root,
		enabled:   map[string]bool{},
		runtimes:  map[string]*Runtime{},
		wasms:     map[string]*WasmRuntime{},
		providers: map[string]providerFunc{},
	}
	m.loadState()
	return m
}

func (m *Manager) RegisterProvider(name string, fn func(pluginID string, args any) (any, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[name] = fn
}

func (m *Manager) SetLogger(log func(pluginID, msg string)) {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	m.log = log
}

func (m *Manager) statePath() string {
	return filepath.Join(filepath.Dir(m.root), "config", "plugins.json")
}

func (m *Manager) loadState() {
	data, err := os.ReadFile(m.statePath())
	if err != nil {
		return
	}
	var st struct {
		Enabled []string `json:"enabled"`
	}
	if json.Unmarshal(data, &st) != nil {
		return
	}
	for _, id := range st.Enabled {
		m.enabled[id] = true
	}
}

func (m *Manager) saveState() error {
	ids := make([]string, 0, len(m.enabled))
	for id := range m.enabled {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	data, err := json.Marshal(struct {
		Enabled []string `json:"enabled"`
	}{Enabled: ids})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.statePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.statePath(), data, 0o644)
}

func (m *Manager) storagePath(id string) string {
	return filepath.Join(m.root, id, "storage.json")
}

func (m *Manager) storageRead(id string) map[string]any {
	data, err := os.ReadFile(m.storagePath(id))
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil {
		return map[string]any{}
	}
	return out
}

func (m *Manager) StorageGet(id, key string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.storageRead(id)[key], nil
}

const maxStorageBytes = 4 << 20

func (m *Manager) StorageSet(id, key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	store := m.storageRead(id)
	if value == nil {
		delete(store, key)
	} else {
		store[key] = value
	}
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	if len(data) > maxStorageBytes {
		return fmt.Errorf("storage full (%d bytes, max %d)", len(data), maxStorageBytes)
	}
	return os.WriteFile(m.storagePath(id), data, 0o644)
}

func (m *Manager) Manifest(id string) *Manifest {
	for _, p := range m.List() {
		if p.Manifest.ID == id {
			return p.Manifest
		}
	}
	return nil
}

func (m *Manager) SetSafeMode(v bool) {
	m.mu.Lock()
	m.safeMode = v
	m.mu.Unlock()
}

func (m *Manager) IsEnabled(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled[id] && !m.safeMode
}

func (m *Manager) StopRuntime(id string) { m.stopRuntime(id) }

func (m *Manager) StartPlugin(id string) error {
	p := m.find(id)
	if p == nil {
		return fmt.Errorf("plugin %q not installed", id)
	}
	return m.startRuntime(p)
}

func (m *Manager) Install(srcDir string) (*InstalledPlugin, error) {
	data, err := os.ReadFile(filepath.Join(srcDir, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("read plugin.json: %w", err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	if err := ValidatePermissions(manifest.Permissions); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dest := filepath.Join(m.root, manifest.ID)
	if _, err := os.Stat(dest); err == nil {
		oldData, err := os.ReadFile(filepath.Join(dest, "plugin.json"))
		var old Manifest
		if err == nil && json.Unmarshal(oldData, &old) == nil && old.Version == manifest.Version {
			return nil, fmt.Errorf("plugin %q %s is already installed", manifest.ID, manifest.Version)
		}
		if err := os.RemoveAll(dest); err != nil {
			return nil, fmt.Errorf("replace old plugin files: %w", err)
		}
	}
	if err := copyDir(srcDir, dest); err != nil {
		return nil, err
	}
	return &InstalledPlugin{Manifest: manifest, Dir: dest, Enabled: m.enabled[manifest.ID]}, nil
}

func (m *Manager) List() []InstalledPlugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil
	}
	var out []InstalledPlugin = []InstalledPlugin{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.root, e.Name(), "plugin.json"))
		if err != nil {
			continue
		}
		manifest, err := ParseManifest(data)
		if err != nil {
			continue
		}
		out = append(out, InstalledPlugin{
			Manifest: manifest,
			Dir:      filepath.Join(m.root, e.Name()),
			Enabled:  m.enabled[manifest.ID],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

func (m *Manager) SetEnabled(id string, enabled bool) error {
	m.mu.Lock()
	if enabled {
		m.enabled[id] = true
	} else {
		delete(m.enabled, id)
	}
	safe := m.safeMode
	if err := m.saveState(); err != nil {
		return err
	}
	m.mu.Unlock()
	if enabled {
		if safe {
			return nil
		}
		p := m.find(id)
		if p == nil {
			return fmt.Errorf("plugin %q not installed", id)
		}
		return m.startRuntime(p)
	}
	m.stopRuntime(id)
	return nil
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	delete(m.enabled, id)
	if err := m.saveState(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	m.stopRuntime(id)
	return os.RemoveAll(filepath.Join(m.root, id))
}

func (m *Manager) Start() error {
	m.mu.Lock()
	safe := m.safeMode
	ids := make([]string, 0, len(m.enabled))
	for id := range m.enabled {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	if safe {
		return nil
	}
	for _, id := range ids {
		if p := m.find(id); p != nil {
			if err := m.startRuntime(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) find(id string) *InstalledPlugin {
	for _, p := range m.List() {
		if p.Manifest.ID == id {
			return &p
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		w, err := os.Create(out)
		if err != nil {
			return err
		}
		defer w.Close()
		_, err = io.Copy(w, in)
		return err
	})
}
