package plugin

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

	"github.com/dop251/goja"
)

const pluginTimeout = 5 * time.Second

type Runtime struct {
	id       string
	vm       *goja.Runtime
	mu       sync.Mutex
	unsubs   []func()
	commands map[string]goja.Value
	messages map[string]goja.Callable
	timers   map[int64]*time.Timer
	timerSeq int64
}

func (m *Manager) startRuntime(p *InstalledPlugin) error {
	if p.Manifest.Entry == "" {
		return nil
	}
	// Dispatch: .wasm entries go to the wazero runtime, everything else to goja.
	if p.Manifest.Runtime == "wasm" || isWasmName(p.Manifest.Entry) {
		return m.startWasmRuntime(p)
	}
	code, err := os.ReadFile(filepath.Join(p.Dir, p.Manifest.Entry))
	if err != nil {
		return fmt.Errorf("read plugin entry: %w", err)
	}
	if err := ScanScript(p.Manifest.Entry, code); err != nil {
		return fmt.Errorf("plugin %s rejected: %w", p.Manifest.ID, err)
	}
	rt := &Runtime{
		id:       p.Manifest.ID,
		commands: map[string]goja.Value{},
		messages: map[string]goja.Callable{},
		timers:   map[int64]*time.Timer{},
	}
	rt.vm = goja.New()
	rt.installAPI(p.Manifest, m)

	m.mu.Lock()
	m.runtimes[p.Manifest.ID] = rt
	m.mu.Unlock()

	if err := rt.run(code); err != nil {
		m.stopRuntime(p.Manifest.ID)
		return fmt.Errorf("plugin %s: %w", p.Manifest.ID, err)
	}
	return nil
}

func (m *Manager) stopRuntime(id string) {
	m.mu.Lock()
	rt := m.runtimes[id]
	delete(m.runtimes, id)
	wr := m.wasms[id]
	delete(m.wasms, id)
	m.mu.Unlock()
	if wr != nil {
		wr.Stop()
	}
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, unsub := range rt.unsubs {
		unsub()
	}
	rt.unsubs = nil
	for _, t := range rt.timers {
		t.Stop()
	}
	rt.timers = nil
	rt.messages = nil
	rt.vm = nil
}

func (m *Manager) Commands() map[string][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]string{}
	for id, rt := range m.runtimes {
		rt.mu.Lock()
		for name := range rt.commands {
			out[id] = append(out[id], name)
		}
		rt.mu.Unlock()
	}
	for id, wr := range m.wasms {
		out[id] = append(out[id], wr.Commands()...)
	}
	return out
}

func (rt *Runtime) run(code []byte) error {
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(pluginTimeout):
			rt.vm.Interrupt("plugin timeout")
		}
	}()
	rt.mu.Lock()
	_, err := rt.vm.RunString(string(code))
	rt.mu.Unlock()
	close(done)
	return err
}

func (m *Manager) callLocked(rt *Runtime, call goja.Callable, args ...goja.Value) (goja.Value, error) {
	vm := rt.vm
	if vm == nil {
		return nil, fmt.Errorf("plugin %s stopped", rt.id)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(pluginTimeout):
			vm.Interrupt("plugin timeout")
		}
	}()
	v, err := call(goja.Undefined(), args...)
	close(done)
	if err != nil {
		var iErr *goja.InterruptedError
		if errors.As(err, &iErr) {
			go m.stopRuntime(rt.id)
			return nil, fmt.Errorf("plugin %s timed out, runtime stopped", rt.id)
		}
	}
	return v, err
}

func (m *Manager) addTimer(rt *Runtime, fn goja.Value, delay time.Duration, repeat bool) int64 {
	call, ok := goja.AssertFunction(fn)
	if !ok {
		panic(rt.vm.ToValue(fmt.Errorf("timer callback must be a function")))
	}
	if rt.vm == nil {
		return 0
	}
	rt.timerSeq++
	id := rt.timerSeq
	var t *time.Timer
	t = time.AfterFunc(delay, func() {
		rt.mu.Lock()
		if _, ok := rt.timers[id]; !ok {
			rt.mu.Unlock()
			return
		}
		if !repeat {
			delete(rt.timers, id)
		}
		if rt.vm == nil {
			rt.mu.Unlock()
			return
		}
		_, _ = m.callLocked(rt, call)
		alive := rt.vm != nil
		rt.mu.Unlock()
		if repeat && alive {
			t.Reset(delay)
		}
	})
	rt.timers[id] = t
	return id
}

func (rt *Runtime) stopTimer(id int64) {
	if t, ok := rt.timers[id]; ok {
		t.Stop()
		delete(rt.timers, id)
	}
}

func (m *Manager) bridgeEmit(rt *Runtime) func(args any) any {
	return func(args any) any {
		m.mu.Lock()
		fn := m.providers["ui.emit"]
		m.mu.Unlock()
		if fn == nil {
			panic(rt.vm.ToValue(fmt.Errorf("capability ui.emit unavailable")))
		}
		out, err := fn(rt.id, args)
		if err != nil {
			panic(rt.vm.ToValue(err))
		}
		return out
	}
}

func (m *Manager) DeliverMessage(id, name string, data any) error {
	m.mu.Lock()
	rt := m.runtimes[id]
	m.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("plugin %q is not running", id)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.vm == nil {
		return fmt.Errorf("plugin %q is not running", id)
	}
	call, ok := rt.messages[name]
	if !ok {
		return fmt.Errorf("plugin %q has no ui.on handler for %q", id, name)
	}
	_, err := m.callLocked(rt, call, rt.vm.ToValue(data))
	return err
}

func (m *Manager) RunCommand(id, name string) (any, error) {
	m.mu.Lock()
	rt := m.runtimes[id]
	wr := m.wasms[id]
	m.mu.Unlock()
	if rt == nil && wr == nil {
		return nil, fmt.Errorf("plugin %q is not running", id)
	}
	if rt == nil {
		return wr.RunCommand(name)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	fn, ok := rt.commands[name]
	if !ok {
		return nil, fmt.Errorf("command %q not found in plugin %q", name, id)
	}
	call, ok := goja.AssertFunction(fn)
	if !ok {
		return nil, fmt.Errorf("command %q in plugin %q is not callable", name, id)
	}
	v, err := m.callLocked(rt, call)
	if err != nil {
		return nil, err
	}
	return v.Export(), nil
}

func (m *Manager) audit(pluginID, msg string) {
	m.logMu.RLock()
	log := m.log
	m.logMu.RUnlock()
	if log != nil {
		log(pluginID, msg)
	}
}

func (m *Manager) providerCall(rt *Runtime, manifest *Manifest, perm, name string) func(args any) any {
	return func(args any) any {
		if perm != "" && !HasPermission(manifest.Permissions, perm) {
			panic(rt.vm.ToValue(fmt.Errorf("permission %s required", perm)))
		}
		m.audit(rt.id, "call "+name)
		m.mu.Lock()
		fn := m.providers[name]
		m.mu.Unlock()
		if fn == nil {
			panic(rt.vm.ToValue(fmt.Errorf("capability %s unavailable", name)))
		}
		out, err := fn(rt.id, args)
		if err != nil {
			panic(rt.vm.ToValue(err))
		}
		return toJSPayload(out)
	}
}

func toJSPayload(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(data, &out) != nil {
		return v
	}
	return out
}

func (rt *Runtime) installAPI(manifest *Manifest, mgr *Manager) {
	launcher := map[string]any{
		"on": func(event string, fn goja.Value) {
			if !HasPermission(manifest.Permissions, PermEvents) {
				panic(rt.vm.ToValue(fmt.Errorf("permission %s required", PermEvents)))
			}
			call, ok := goja.AssertFunction(fn)
			if !ok {
				panic(rt.vm.ToValue(fmt.Errorf("launcher.on: second argument must be a function")))
			}
			unsub := Bus.On(event, func(_ context.Context, payload any) any {
				rt.mu.Lock()
				defer rt.mu.Unlock()
				if rt.vm == nil {
					return nil
				}
				v, err := mgr.callLocked(rt, call, rt.vm.ToValue(toJSPayload(payload)))
				if err != nil || v == nil || rt.vm == nil {
					return nil
				}
				reply, ok := v.Export().(map[string]any)
				if !ok {
					return nil
				}
				return hookReply{pluginID: rt.id, value: reply}
			})
			rt.unsubs = append(rt.unsubs, unsub)
		},
		"instances": map[string]any{
			"list":       mgr.providerCall(rt, manifest, PermInstancesRead, "instances.list"),
			"get":        mgr.providerCall(rt, manifest, PermInstancesRead, "instances.get"),
			"openFolder": mgr.providerCall(rt, manifest, PermInstancesOpen, "open.folder"),
		},
		"launch": map[string]any{
			"start": mgr.providerCall(rt, manifest, PermLaunchStart, "launch.start"),
		},
		"app": map[string]any{
			"state": mgr.providerCall(rt, manifest, PermInstancesRead, "app.state"),
		},
		"mods": map[string]any{
			"list": mgr.providerCall(rt, manifest, PermModsRead, "mods.list"),
		},
		"worlds": map[string]any{
			"list": mgr.providerCall(rt, manifest, PermWorldsRead, "worlds.list"),
		},
		"accounts": map[string]any{
			"list": mgr.providerCall(rt, manifest, PermAccountsRead, "accounts.list"),
		},
		"logs": map[string]any{
			"tail": mgr.providerCall(rt, manifest, PermLogsRead, "logs.tail"),
		},
		"fs": map[string]any{
			"read":  mgr.providerCall(rt, manifest, PermFSInstanceRead, "fs.read"),
			"list":  mgr.providerCall(rt, manifest, PermFSInstanceRead, "fs.list"),
			"write": mgr.providerCall(rt, manifest, PermFSInstanceWrite, "fs.write"),
		},
		"setTimeout": func(fn goja.Value, ms int64) int64 {
			return mgr.addTimer(rt, fn, time.Duration(ms)*time.Millisecond, false)
		},
		"setInterval": func(fn goja.Value, ms int64) int64 {
			return mgr.addTimer(rt, fn, time.Duration(ms)*time.Millisecond, true)
		},
		"clearTimeout":  rt.stopTimer,
		"clearInterval": rt.stopTimer,
		"ui": map[string]any{
			"on": func(name string, fn goja.Value) {
				call, ok := goja.AssertFunction(fn)
				if !ok || name == "" {
					panic(rt.vm.ToValue(fmt.Errorf("ui.on: a message name and a function are required")))
				}
				rt.messages[name] = call
			},
			"emit": mgr.bridgeEmit(rt),
		},
		"storage": map[string]any{
			"get": func(key string) (any, error) { return mgr.StorageGet(rt.id, key) },
			"set": func(args map[string]any) (any, error) {
				key, _ := args["key"].(string)
				if key == "" {
					return nil, fmt.Errorf("key is required")
				}
				return nil, mgr.StorageSet(rt.id, key, args["value"])
			},
			"delete": func(key string) (any, error) { return nil, mgr.StorageSet(rt.id, key, nil) },
		},
		"http": map[string]any{
			"request": func(args map[string]any) (any, error) {
				if !HasPermission(manifest.Permissions, PermNetworkCustom) {
					return nil, fmt.Errorf("permission %s required", PermNetworkCustom)
				}
				method, _ := args["method"].(string)
				if method == "" {
					method = "GET"
				}
				url, _ := args["url"].(string)
				if url == "" {
					return nil, fmt.Errorf("url is required")
				}
				body, _ := args["body"].(string)
				if err := CheckPublicURL(url); err != nil {
					return nil, err
				}
				req, err := http.NewRequest(method, url, strings.NewReader(body))
				if err != nil {
					return nil, err
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
					return nil, err
				}
				defer resp.Body.Close()
				data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
				if err != nil {
					return nil, err
				}
				return map[string]any{"status": resp.StatusCode, "body": string(data)}, nil
			},
			"post": func(url, body string) (string, error) {
				if !HasPermission(manifest.Permissions, PermNetworkCustom) {
					return "", fmt.Errorf("permission %s required", PermNetworkCustom)
				}
				if err := CheckPublicURL(url); err != nil {
					return "", err
				}
				client := SafeHTTPClient(10 * time.Second)
				resp, err := client.Post(url, "application/json", strings.NewReader(body))
				if err != nil {
					return "", err
				}
				defer resp.Body.Close()
				data, err := io.ReadAll(resp.Body)
				return string(data), err
			},
		},
		"registerCommand": func(name string, fn goja.Value) {
			rt.commands[name] = fn
		},
		"log": func(msg string) {
			mgr.audit(rt.id, msg)
		},
	}
	rt.vm.Set("launcher", launcher)
}
