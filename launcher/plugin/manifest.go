package plugin

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const PluginSDKVersion = "1.3.0"

// WasmSDKVersion is the launcher version that introduced WASM plugin support.
// Plugins with runtime "wasm" must require at least this version so that
// older launchers reject them cleanly via the minLauncherVersion check
// instead of trying to load a .wasm binary as JavaScript.
const WasmSDKVersion = "1.3.0"

var validID = regexp.MustCompile(`^[a-z0-9-]+$`)

func ValidID(id string) bool {
	return validID.MatchString(id)
}

func safeRelPath(p string) bool {
	if strings.Contains(p, "..") {
		return false
	}
	clean := filepath.Clean(p)
	if filepath.IsAbs(clean) {
		return false
	}
	if strings.HasPrefix(filepath.ToSlash(clean), "/") {
		return false
	}
	return true
}

type SettingField struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Label   string   `json:"label"`
	Default any      `json:"default,omitempty"`
	Options []string `json:"options,omitempty"`
}

var validSettingTypes = map[string]bool{"bool": true, "string": true, "number": true, "select": true}

type Manifest struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Version            string         `json:"version"`
	Category           string         `json:"category"`
	Runtime            string         `json:"runtime,omitempty"`
	Entry              string         `json:"entry,omitempty"`
	Frontend           string         `json:"frontend,omitempty"`
	Description        string         `json:"description,omitempty"`
	Author             string         `json:"author,omitempty"`
	Homepage           string         `json:"homepage,omitempty"`
	MinLauncherVersion string         `json:"minLauncherVersion,omitempty"`
	Permissions        []string       `json:"permissions,omitempty"`
	Settings           []SettingField `json:"settings,omitempty"`
}

func (m *Manifest) Validate() error {
	if !validID.MatchString(m.ID) {
		return fmt.Errorf("plugin id %q must match ^[a-z0-9-]+$", m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("plugin version is required")
	}
	if !validID.MatchString(m.Category) {
		return fmt.Errorf("plugin category %q must match ^[a-z0-9-]+$", m.Category)
	}
	if m.Entry == "" && m.Frontend == "" {
		return fmt.Errorf("plugin needs an entry or a frontend entry")
	}
	switch m.Runtime {
	case "", "js":
		// default: JavaScript via goja
	case "wasm":
		if m.Entry == "" {
			return fmt.Errorf(`plugin with runtime "wasm" needs an entry`)
		}
		if !strings.HasSuffix(strings.ToLower(m.Entry), ".wasm") {
			return fmt.Errorf(`plugin with runtime "wasm" must have a .wasm entry, got %q`, m.Entry)
		}
		if compareVersions(m.MinLauncherVersion, WasmSDKVersion) < 0 {
			return fmt.Errorf(`plugin with runtime "wasm" requires minLauncherVersion >= %s (old launchers would try to load it as JavaScript)`, WasmSDKVersion)
		}
		if !HasPermission(m.Permissions, PermRuntimeWasm) {
			return fmt.Errorf(`plugin with runtime "wasm" requires the %s permission`, PermRuntimeWasm)
		}
	default:
		return fmt.Errorf("unknown runtime %q (want \"js\" or \"wasm\")", m.Runtime)
	}
	if m.Entry != "" {
		if !safeRelPath(m.Entry) {
			return fmt.Errorf("plugin entry must be a relative path without '..'")
		}
	}
	if m.Frontend != "" {
		if !safeRelPath(m.Frontend) {
			return fmt.Errorf("plugin frontend must be a relative path without '..'")
		}
	}
	if m.MinLauncherVersion != "" && compareVersions(m.MinLauncherVersion, PluginSDKVersion) > 0 {
		return fmt.Errorf("plugin requires launcher %s, this launcher is %s", m.MinLauncherVersion, PluginSDKVersion)
	}
	seen := map[string]bool{}
	for _, s := range m.Settings {
		if s.Key == "" {
			return fmt.Errorf("settings entry with empty key")
		}
		if seen[s.Key] {
			return fmt.Errorf("duplicate settings key %q", s.Key)
		}
		seen[s.Key] = true
		if !validSettingTypes[s.Type] {
			return fmt.Errorf("setting %q has unknown type %q (want bool, string, number or select)", s.Key, s.Type)
		}
		if s.Type == "select" && len(s.Options) == 0 {
			return fmt.Errorf("select setting %q needs options", s.Key)
		}
		if s.Label == "" {
			return fmt.Errorf("setting %q needs a label", s.Key)
		}
	}
	return nil
}

func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse plugin.json: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func compareVersions(a, b string) int {
	as, bs := versionParts(a), versionParts(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
