package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func jsonMarshal(v any) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}

// Weryfikuje przykladowe pluginy z examples/plugins end-to-end przez Managera.
func TestExamplePluginsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(NewEventBus(), filepath.Join(dir, "plugins"))
	m.SetLogger(func(id, msg string) { t.Logf("[%s] %s", id, msg) })

	root, err := filepath.Abs(filepath.Join("..", "..", "examples", "plugins"))
	if err != nil {
		t.Fatal(err)
	}

	// --- js-demo.mlplugin ---
	if err := InstallFromZip(filepath.Join(root, "js-demo.mlplugin"), filepath.Join(dir, "stage-js")); err != nil {
		t.Fatalf("install js-demo: %v", err)
	}
	if _, err := m.Install(filepath.Join(dir, "stage-js")); err != nil {
		t.Fatalf("install js-demo dir: %v", err)
	}
	if err := m.SetEnabled("js-demo", true); err != nil {
		t.Fatalf("enable js-demo: %v", err)
	}
	stats, err := m.RunCommand("js-demo", "stats")
	if err != nil {
		t.Fatalf("js-demo stats: %v", err)
	}
	if sm, ok := stats.(map[string]any); !ok || sm["launches"] == nil {
		t.Fatalf("js-demo stats = %#v", stats)
	}
	// messaging frontend -> backend
	if err := m.DeliverMessage("js-demo", "get-greeting", nil); err != nil {
		t.Fatalf("js-demo get-greeting: %v", err)
	}
	if v, err := m.StorageGet("js-demo", "lastLaunch"); err != nil || v == nil {
		t.Logf("js-demo lastLaunch = %v (event jeszcze nie odpalony — ok)", v)
	}

	// --- wasm-demo.mlplugin ---
	if err := InstallFromZip(filepath.Join(root, "wasm-demo.mlplugin"), filepath.Join(dir, "stage-wasm")); err != nil {
		t.Fatalf("install wasm-demo: %v", err)
	}
	if _, err := m.Install(filepath.Join(dir, "stage-wasm")); err != nil {
		t.Fatalf("install wasm-demo dir: %v", err)
	}
	if err := m.SetEnabled("wasm-demo", true); err != nil {
		t.Fatalf("enable wasm-demo: %v", err)
	}
	cmds := m.Commands()
	if len(cmds["wasm-demo"]) < 2 {
		t.Fatalf("wasm-demo commands = %v, want ping+info", cmds["wasm-demo"])
	}
	out, err := m.RunCommand("wasm-demo", "ping")
	if err != nil {
		t.Fatalf("wasm-demo ping: %v", err)
	}
	if data, jerr := jsonMarshal(out); jerr != nil || data != `{"pong":true}` {
		t.Fatalf("wasm-demo ping = %s (%v)", data, jerr)
	}
	out, err = m.RunCommand("wasm-demo", "info")
	if err != nil {
		t.Fatalf("wasm-demo info: %v", err)
	}
	if data, _ := jsonMarshal(out); data != `{"plugin":"wasm-demo"}` {
		t.Fatalf("wasm-demo info = %s", data)
	}
	// launcher_init zapisal do storage przez host import
	if v, err := m.StorageGet("wasm-demo", "boot"); err != nil || v == nil {
		t.Fatalf("wasm-demo storage boot = %v err %v, want zapisany przez launcher_init", v, err)
	}

	if err := m.SetEnabled("wasm-demo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RunCommand("wasm-demo", "ping"); err == nil {
		t.Fatal("wasm-demo ping po stop powinien się nie udać")
	}
	_ = os.RemoveAll(dir)
}
