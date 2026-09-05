package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- minimal WASM binary builder (guest module) -----------------------------

func uleb(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func sleb(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			out = append(out, b)
			return out
		}
		out = append(out, b|0x80)
	}
}

func section(id byte, payload []byte) []byte {
	out := append([]byte{id}, uleb(uint64(len(payload)))...)
	return append(out, payload...)
}

// testGuestModule builds a valid wasm module exporting memory, alloc,
// launcher_call and cmd_ping. cmd_ping returns pack(ptr=1024, len=13) pointing
// at the data-section string {"pong":true} placed at offset 1024.
func testGuestModule(t *testing.T) []byte {
	t.Helper()

	pong := []byte(`{"pong":true}`)
	const buf uint32 = 1024

	// type section: ()->i64, (i32)->i32, (i32,i32,i32,i32)->i64
	types := []byte{0x03}
	types = append(types, 0x60, 0x00, 0x01, 0x7E)
	types = append(types, 0x60, 0x01, 0x7F, 0x01, 0x7F)
	types = append(types, 0x60, 0x04, 0x7F, 0x7F, 0x7F, 0x7F, 0x01, 0x7E)
	// func section: cmd_ping:type0, alloc:type1, launcher_call:type2
	funcs := append(uleb(3), 0x00, 0x01, 0x02)
	// memory section: 1 memory, min 1 page
	mems := append(uleb(1), 0x00, 0x01)
	// export section
	exports := uleb(4)
	exports = append(exports, uleb(6)...)
	exports = append(exports, []byte("memory")...)
	exports = append(exports, 0x02, 0x00)
	for _, e := range []struct {
		name   string
		findex byte
	}{{"alloc", 1}, {"launcher_call", 2}, {"cmd_ping", 0}} {
		exports = append(exports, uleb(uint64(len(e.name)))...)
		exports = append(exports, e.name...)
		exports = append(exports, 0x00, e.findex)
	}
	// code section
	body0 := append(uleb(0), 0x42) // cmd_ping: i64.const packed(buf,len)
	body0 = append(body0, sleb(int64(buf)<<32|int64(len(pong)))...)
	body0 = append(body0, 0x0B)
	body1 := append(uleb(0), 0x41) // alloc: i32.const buf
	body1 = append(body1, sleb(int64(buf))...)
	body1 = append(body1, 0x0B)
	body2 := append(uleb(0), 0x42, 0x00, 0x0B) // launcher_call: i64.const 0
	code := uleb(3)
	for _, b := range [][]byte{body0, body1, body2} {
		code = append(code, uleb(uint64(len(b)))...)
		code = append(code, b...)
	}
	// data section: place pong at buf
	data := uleb(1)
	data = append(data, 0x00, 0x41)
	data = append(data, sleb(int64(buf))...)
	data = append(data, 0x0B, byte(len(pong)))
	data = append(data, pong...)

	bin := append([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
		section(1, types)...)
	bin = append(bin, section(3, funcs)...)
	bin = append(bin, section(5, mems)...)
	bin = append(bin, section(7, exports)...)
	bin = append(bin, section(10, code)...)
	bin = append(bin, section(11, data)...)
	return bin
}

// --- tests ------------------------------------------------------------------

func installTestWasmPlugin(t *testing.T, m *Manager) {
	t.Helper()
	code := testGuestModule(t)
	manifest, err := json.Marshal(&Manifest{
		ID:                 "wasm-test",
		Name:               "Wasm Test",
		Version:            "1.0.0",
		Category:           "tools",
		Runtime:            "wasm",
		Entry:              "main.wasm",
		MinLauncherVersion: WasmSDKVersion,
		Permissions:        []string{PermRuntimeWasm},
	})
	if err != nil {
		t.Fatal(err)
	}
	pdir := filepath.Join(m.root, "wasm-test")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "main.wasm"), code, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanWasm(t *testing.T) {
	if err := ScanWasm("main.wasm", testGuestModule(t)); err != nil {
		t.Fatalf("valid module rejected: %v", err)
	}
	if err := ScanWasm("main.wasm", []byte("not a wasm binary")); err == nil {
		t.Fatal("bad magic accepted")
	}
	if MaxFileScanLimit("main.wasm") != maxWasmBytes {
		t.Fatal("wasm scan limit not applied")
	}
	if MaxFileScanLimit("main.js") != maxScanBytes {
		t.Fatal("js scan limit changed")
	}
}

func TestWasmRuntimeLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(NewEventBus(), filepath.Join(dir, "plugins"))
	m.SetLogger(func(pluginID, msg string) { t.Logf("[%s] %s", pluginID, msg) })

	installTestWasmPlugin(t, m)
	if err := m.SetEnabled("wasm-test", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	cmds := m.Commands()
	if len(cmds["wasm-test"]) != 1 || cmds["wasm-test"][0] != "ping" {
		t.Fatalf("auto-discovered commands = %v, want [ping]", cmds["wasm-test"])
	}

	out, err := m.RunCommand("wasm-test", "ping")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if data, err := json.Marshal(out); err != nil || string(data) != `{"pong":true}` {
		t.Fatalf("command result = %s (err %v), want {\"pong\":true}", data, err)
	}

	if err := m.SetEnabled("wasm-test", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := m.RunCommand("wasm-test", "ping"); err == nil {
		t.Fatal("command ran on a stopped runtime")
	}
}

func TestWasmRuntimeManifestValidation(t *testing.T) {
	m := &Manifest{ID: "x", Name: "X", Version: "1.0.0", Category: "tools",
		Runtime: "wasm", Entry: "main.wasm", MinLauncherVersion: "1.2.0",
		Permissions: []string{PermRuntimeWasm}}
	if err := m.Validate(); err == nil {
		t.Fatal("wasm plugin with old minLauncherVersion accepted")
	}
	m.Entry = "main.js"
	m.MinLauncherVersion = WasmSDKVersion
	if err := m.Validate(); err == nil {
		t.Fatal("wasm plugin with .js entry accepted")
	}
	m.Entry = "main.wasm"
	m.Permissions = nil
	if err := m.Validate(); err == nil {
		t.Fatal("wasm plugin without runtime:wasm permission accepted")
	}
}

func TestParseWasmExportNames(t *testing.T) {
	names := parseWasmExportNames(testGuestModule(t))
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, want := range []string{"memory", "alloc", "launcher_call", "cmd_ping"} {
		if !got[want] {
			t.Fatalf("export %q missing, got %v", want, names)
		}
	}
	if names := parseWasmExportNames([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, 0xFF}); names != nil {
		t.Fatalf("truncated binary parsed as %v", names)
	}
}
