package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// WASM plugin ABI (launcher <=> guest module)
//
// Guest module must export:
//
//	memory          — linear memory
//	alloc(size i32) -> ptr i32        — allocate a buffer in guest memory
//	dealloc(ptr i32, size i32)        — optional, free a host-written buffer
//	launcher_call(cmd_ptr i32, cmd_len i32, args_ptr i32, args_len i32) -> i64
//
// launcher_call returns a packed (ptr << 32) | len pointing at a JSON result
// in guest memory, or 0 for a null result.
//
// Commands are auto-discovered: any exported function named `cmd_<name>`
// (signature: no params, returns i64 packed JSON result or nothing) becomes
// a launcher command called `name`.
//
// The host imports a module "launcher" providing:
//
//	launcher_log(ptr, len)
//	launcher_storage_get(key_ptr, key_len) -> i64 packed JSON
//	launcher_storage_set(key_ptr, key_len, val_ptr, val_len) -> i32
//	launcher_storage_delete(key_ptr, key_len) -> i32
//	launcher_http_request(json_ptr, json_len) -> i64 packed JSON
//	launcher_emit(json_ptr, json_len) -> i64 packed JSON
//	launcher_provider(cap_ptr, cap_len, args_ptr, args_len) -> i64 packed JSON
//
// Host-written values are placed in guest memory via the guest's `alloc`
// export. Guest code is never re-entered concurrently: every call into the
// module is serialized on WasmRuntime.mu because linear memory is not
// reentrant (the same discipline as the goja runtime).

const wasmMemoryPages = 512 // 512 * 64 KiB = 32 MiB linear memory cap

type WasmRuntime struct {
	id     string
	mod    api.Module
	mem    api.Memory
	rt     wazero.Runtime
	cancel context.CancelFunc
	base   context.Context

	allocFn   api.Function
	deallocFn api.Function
	callFn    api.Function
	commands  map[string]api.Function

	mu sync.Mutex
}

func (m *Manager) startWasmRuntime(p *InstalledPlugin) error {
	manifest := p.Manifest
	if !HasPermission(manifest.Permissions, PermRuntimeWasm) {
		return fmt.Errorf("plugin %s: runtime %q requires the %s permission", manifest.ID, "wasm", PermRuntimeWasm)
	}
	code, err := os.ReadFile(filepath.Join(p.Dir, manifest.Entry))
	if err != nil {
		return fmt.Errorf("read plugin entry: %w", err)
	}
	if err := ScanWasm(manifest.Entry, code); err != nil {
		return fmt.Errorf("plugin %s rejected: %w", manifest.ID, err)
	}

	base, cancel := context.WithCancel(context.Background())
	wr := &WasmRuntime{
		id:       manifest.ID,
		base:     base,
		cancel:   cancel,
		commands: map[string]api.Function{},
	}

	cfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true). // hard-abort guest calls when the ctx expires, like vm.Interrupt in goja
		WithMemoryLimitPages(wasmMemoryPages)
	wr.rt = wazero.NewRuntimeWithConfig(base, cfg)
	wr.installHostAPI(manifest, m)

	mod, err := wr.rt.Instantiate(base, code)
	if err != nil {
		wr.Stop()
		return fmt.Errorf("plugin %s: instantiate wasm: %w", manifest.ID, err)
	}
	wr.mod = mod
	wr.mem = mod.Memory()
	wr.allocFn = mod.ExportedFunction("alloc")
	wr.callFn = mod.ExportedFunction("launcher_call")
	wr.deallocFn = mod.ExportedFunction("dealloc")
	if wr.mem == nil || wr.allocFn == nil || wr.callFn == nil {
		wr.Stop()
		return fmt.Errorf("plugin %s: wasm module must export memory, alloc and launcher_call", manifest.ID)
	}

	// Auto-discover commands: every export prefixed cmd_ is a command.
	for _, name := range parseWasmExportNames(code) {
		if cmd, ok := strings.CutPrefix(name, "cmd_"); ok && cmd != "" {
			wr.commands[cmd] = mod.ExportedFunction(name)
		}
	}

	m.mu.Lock()
	m.wasms[manifest.ID] = wr
	m.mu.Unlock()

	// Optional init hook — lets the guest run one-off setup.
	if init := mod.ExportedFunction("launcher_init"); init != nil {
		if _, err := wr.callWithTimeout(func(ctx context.Context) ([]uint64, error) {
			return init.Call(ctx)
		}); err != nil {
			m.stopWasmRuntime(manifest.ID)
			return fmt.Errorf("plugin %s: launcher_init: %w", manifest.ID, err)
		}
	}
	return nil
}

func (m *Manager) stopWasmRuntime(id string) {
	m.mu.Lock()
	wr := m.wasms[id]
	delete(m.wasms, id)
	m.mu.Unlock()
	if wr != nil {
		wr.Stop()
	}
}

// Stop tears the module down. Callers must not hold wr.mu.
func (wr *WasmRuntime) Stop() {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.commands = nil
	wr.mod = nil
	wr.mem = nil
	wr.allocFn = nil
	wr.callFn = nil
	if wr.cancel != nil {
		wr.cancel() // aborts any in-flight guest call (WithCloseOnContextDone)
		wr.cancel = nil
	}
	if wr.rt != nil {
		_ = wr.rt.Close(context.Background())
		wr.rt = nil
	}
}

func (wr *WasmRuntime) callWithTimeout(fn func(context.Context) ([]uint64, error)) (uint64, error) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	return wr.callLocked(fn)
}

// callLocked runs a guest call under wr.mu (linear memory is not reentrant)
// with the 5 s plugin timeout, mirroring callLocked for the goja runtime.
func (wr *WasmRuntime) callLocked(fn func(context.Context) ([]uint64, error)) (uint64, error) {
	if wr.mod == nil {
		return 0, fmt.Errorf("plugin %s stopped", wr.id)
	}
	ctx, cancel := context.WithTimeout(wr.base, pluginTimeout)
	defer cancel()
	res, err := fn(ctx)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			wr.Stop()
			return 0, fmt.Errorf("plugin %s timed out, runtime stopped", wr.id)
		}
		return 0, err
	}
	if len(res) == 0 {
		return 0, nil
	}
	return res[0], nil
}

func (wr *WasmRuntime) readGuest(ptr, size uint32) ([]byte, error) {
	if wr.mem == nil {
		return nil, fmt.Errorf("plugin %s stopped", wr.id)
	}
	data, ok := wr.mem.Read(ptr, size)
	if !ok {
		return nil, fmt.Errorf("plugin %s: invalid guest memory range (%d, %d)", wr.id, ptr, size)
	}
	return data, nil
}

// writeGuest allocates a buffer in guest memory and copies data into it.
// Must be called with wr.mu held.
func (wr *WasmRuntime) writeGuest(data []byte) (uint32, error) {
	if wr.allocFn == nil {
		return 0, fmt.Errorf("plugin %s: guest does not export alloc", wr.id)
	}
	res, err := wr.allocFn.Call(wr.base, uint64(len(data)))
	if err != nil {
		return 0, fmt.Errorf("guest alloc(%d): %w", len(data), err)
	}
	ptr := uint32(res[0])
	if !wr.mem.Write(ptr, data) {
		return 0, fmt.Errorf("plugin %s: failed to write %d bytes at %d", wr.id, len(data), ptr)
	}
	return ptr, nil
}

func (wr *WasmRuntime) readJSONResult(packed uint64) (any, error) {
	if packed == 0 {
		return nil, nil
	}
	ptr, size := unpackResult(packed)
	data, err := wr.readGuest(ptr, size)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return string(data), nil // non-JSON result: return as text
	}
	return out, nil
}

// hostStr reads a (ptr, len) string argument inside a host call. The guest's
// linear memory is stable while the host call runs (single-threaded ABI).
func (wr *WasmRuntime) hostStr(ptr, size uint32) string {
	data, err := wr.readGuest(ptr, size)
	if err != nil {
		return ""
	}
	return string(data)
}

func (wr *WasmRuntime) installHostAPI(manifest *Manifest, mgr *Manager) {
	_, err := wr.rt.NewHostModuleBuilder("launcher").
		NewFunctionBuilder().
		WithFunc(func(ptr, size uint32) {
			mgr.audit(wr.id, wr.hostStr(ptr, size))
		}).Export("launcher_log").
		NewFunctionBuilder().
		WithFunc(func(keyPtr, keyLen uint32) uint64 {
			v, err := mgr.StorageGet(wr.id, wr.hostStr(keyPtr, keyLen))
			if err != nil {
				return 0
			}
			return wr.respond(v)
		}).Export("launcher_storage_get").
		NewFunctionBuilder().
		WithFunc(func(keyPtr, keyLen, valPtr, valLen uint32) uint32 {
			key := wr.hostStr(keyPtr, keyLen)
			var val any
			if data, err := wr.readGuest(valPtr, valLen); err == nil {
				_ = json.Unmarshal(data, &val)
			}
			return boolErr(mgr.StorageSet(wr.id, key, val))
		}).Export("launcher_storage_set").
		NewFunctionBuilder().
		WithFunc(func(keyPtr, keyLen uint32) uint32 {
			return boolErr(mgr.StorageSet(wr.id, wr.hostStr(keyPtr, keyLen), nil))
		}).Export("launcher_storage_delete").
		NewFunctionBuilder().
		WithFunc(func(argsPtr, argsLen uint32) uint64 {
			args := wr.hostJSON(argsPtr, argsLen)
			if !HasPermission(manifest.Permissions, PermNetworkCustom) {
				return wr.respond(map[string]any{"error": fmt.Sprintf("permission %s required", PermNetworkCustom)})
			}
			url, _ := args["url"].(string)
			if err := CheckPublicURL(url); err != nil {
				return wr.respond(map[string]any{"error": err.Error()})
			}
			method, _ := args["method"].(string)
			if method == "" {
				method = "GET"
			}
			body, _ := args["body"].(string)
			req, err := http.NewRequest(method, url, strings.NewReader(body))
			if err != nil {
				return wr.respond(map[string]any{"error": err.Error()})
			}
			if headers, ok := args["headers"].(map[string]any); ok {
				for k, v := range headers {
					if s, ok := v.(string); ok {
						req.Header.Set(k, s)
					}
				}
			}
			client := SafeHTTPClient(30 * time.Second)
			resp, err := client.Do(req)
			if err != nil {
				return wr.respond(map[string]any{"error": err.Error()})
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			if err != nil {
				return wr.respond(map[string]any{"error": err.Error()})
			}
			return wr.respond(map[string]any{"status": resp.StatusCode, "body": string(data)})
		}).Export("launcher_http_request").
		NewFunctionBuilder().
		WithFunc(func(argsPtr, argsLen uint32) uint64 {
			mgr.mu.Lock()
			provider := mgr.providers["ui.emit"]
			mgr.mu.Unlock()
			if provider == nil {
				return wr.respond(map[string]any{"error": "capability ui.emit unavailable"})
			}
			out, err := provider(wr.id, wr.hostJSON(argsPtr, argsLen))
			if err != nil {
				return wr.respond(map[string]any{"error": err.Error()})
			}
			return wr.respond(out)
		}).Export("launcher_emit").
		NewFunctionBuilder().
		WithFunc(func(capPtr, capLen, argsPtr, argsLen uint32) uint64 {
			capName := wr.hostStr(capPtr, capLen)
			perm := capabilityPermission(capName)
			if perm != "" && !HasPermission(manifest.Permissions, perm) {
				return wr.respond(map[string]any{"error": fmt.Sprintf("permission %s required", perm)})
			}
			mgr.audit(wr.id, "call "+capName)
			mgr.mu.Lock()
			provider := mgr.providers[capName]
			mgr.mu.Unlock()
			if provider == nil {
				return wr.respond(map[string]any{"error": fmt.Sprintf("capability %s unavailable", capName)})
			}
			out, err := provider(wr.id, wr.hostJSON(argsPtr, argsLen))
			if err != nil {
				return wr.respond(map[string]any{"error": err.Error()})
			}
			// Round-trip through JSON so guest-visible payloads match the
			// goja runtime's toJSPayload normalisation.
			data, err := json.Marshal(out)
			if err != nil {
				return wr.respond(map[string]any{"error": err.Error()})
			}
			var norm any
			_ = json.Unmarshal(data, &norm)
			return wr.respond(norm)
		}).Export("launcher_provider").
		Instantiate(wr.base)
	if err != nil {
		// Host module instantiation precedes the guest module; a failure here
		// means the guest will fail to instantiate too, which startWasmRuntime
		// reports. Record the cause for the log.
		mgr.audit(wr.id, "host module error: "+err.Error())
	}
}

func boolErr(err error) uint32 {
	if err != nil {
		return 1
	}
	return 0
}

// parseWasmExportNames walks the binary's export section and returns every
// export name. wazero's api.Module doesn't enumerate exports, so we read the
// section directly (the binary was already validated by ScanWasm and the
// compiler will reject anything malformed when we Instantiate).
func parseWasmExportNames(code []byte) (names []string) {
	defer func() { _ = recover() }() // defensive: never crash on an odd binary
	if len(code) < 8 || !bytes.Equal(code[:4], wasmMagic) {
		return nil
	}
	pos := 8 // skip magic + version
	for pos < len(code) {
		sectionID := int(code[pos])
		pos++
		// section size (ULEB128)
		var sectionLen uint64
		var shift uint
		for {
			if pos >= len(code) {
				return names
			}
			b := code[pos]
			pos++
			sectionLen |= uint64(b&0x7F) << shift
			if b&0x80 == 0 {
				break
			}
			shift += 7
		}
		sectionEnd := pos + int(sectionLen)
		if sectionEnd > len(code) {
			return names
		}
		if sectionID != 7 { // export section
			pos = sectionEnd
			continue
		}
		// export count
		var count uint64
		shift = 0
		for {
			if pos >= sectionEnd {
				return names
			}
			b := code[pos]
			pos++
			count |= uint64(b&0x7F) << shift
			if b&0x80 == 0 {
				break
			}
			shift += 7
		}
		for i := uint64(0); i < count; i++ {
			// name (ULEB128 length + bytes)
			var nameLen uint64
			shift = 0
			for {
				if pos >= sectionEnd {
					return names
				}
				b := code[pos]
				pos++
				nameLen |= uint64(b&0x7F) << shift
				if b&0x80 == 0 {
					break
				}
				shift += 7
			}
			if pos+int(nameLen) > sectionEnd {
				return names
			}
			names = append(names, string(code[pos:pos+int(nameLen)]))
			pos += int(nameLen)
			pos += 1 // export kind byte
			pos += 1 // index (single-byte ULEB is enough for real modules;
			// oversized indexes simply abort the walk at the bounds check)
			if pos > sectionEnd {
				return names
			}
		}
		return names
	}
	return names
}

// capabilityPermission maps a provider capability name to the permission it
// requires, mirroring the hardcoded bindings in Runtime.installAPI (goja).
func capabilityPermission(capName string) string {
	switch {
	case strings.HasPrefix(capName, "instances."):
		if capName == "instances.open" {
			return PermInstancesOpen
		}
		return PermInstancesRead
	case strings.HasPrefix(capName, "mods."):
		return PermModsRead
	case strings.HasPrefix(capName, "worlds."):
		return PermWorldsRead
	case strings.HasPrefix(capName, "accounts."):
		return PermAccountsRead
	case capName == "logs.tail":
		return PermLogsRead
	case capName == "launch.start":
		return PermLaunchStart
	case capName == "fs.read" || capName == "fs.list":
		return PermFSInstanceRead
	case capName == "fs.write":
		return PermFSInstanceWrite
	default:
		return ""
	}
}

// Commands returns the auto-discovered command names (exports prefixed cmd_).
func (wr *WasmRuntime) Commands() []string {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	out := make([]string, 0, len(wr.commands))
	for name := range wr.commands {
		out = append(out, name)
	}
	return out
}

// RunCommand invokes a cmd_ export with no arguments and unpacks its JSON
// result (packed ptr/len in guest memory).
func (wr *WasmRuntime) RunCommand(name string) (any, error) {
	wr.mu.Lock()
	fn, ok := wr.commands[name]
	wr.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("command %q not found in plugin %q", name, wr.id)
	}
	if wr.mod == nil {
		return nil, fmt.Errorf("plugin %q is not running", wr.id)
	}
	packed, err := wr.callWithTimeout(func(ctx context.Context) ([]uint64, error) {
		return fn.Call(ctx)
	})
	if err != nil {
		return nil, err
	}
	return wr.readJSONResult(packed)
}

func (wr *WasmRuntime) hostJSON(ptr, size uint32) map[string]any {
	out := map[string]any{}
	data, err := wr.readGuest(ptr, size)
	if err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

// respond packs a JSON value into guest memory, returning the packed (ptr,len).
// Must be called with wr.mu held (host calls always are — the guest is blocked).
func (wr *WasmRuntime) respond(v any) uint64 {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	ptr, err := wr.writeGuest(data)
	if err != nil {
		return 0
	}
	return packResult(ptr, uint32(len(data)))
}

func packResult(ptr, size uint32) uint64 {
	return uint64(ptr)<<32 | uint64(size)
}

func unpackResult(v uint64) (uint32, uint32) {
	return uint32(v >> 32), uint32(v & 0xFFFFFFFF)
}
