package plugin

import (
	"context"
	"sync"
)

type handler struct {
	fn func(ctx context.Context, payload any) any
}

type EventBus struct {
	mu       sync.RWMutex
	handlers map[string][]*handler
}

func NewEventBus() *EventBus {
	return &EventBus{handlers: make(map[string][]*handler)}
}

func (b *EventBus) On(event string, fn func(ctx context.Context, payload any) any) func() {
	h := &handler{fn: fn}
	b.mu.Lock()
	b.handlers[event] = append(b.handlers[event], h)
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		hs := b.handlers[event]
		for i, x := range hs {
			if x == h {
				b.handlers[event] = append(hs[:i], hs[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
	}
}

func (b *EventBus) Emit(event string, payload any) {
	for _, h := range b.snapshot(event) {
		h.fn(context.Background(), payload)
	}
}

func (b *EventBus) snapshot(event string) []*handler {
	b.mu.RLock()
	hs := b.handlers[event]
	hs = append([]*handler(nil), hs...)
	b.mu.RUnlock()
	return hs
}

type HookResult struct {
	PluginID string
	Value    map[string]any
}

type hookReply struct {
	pluginID string
	value    map[string]any
}

func (b *EventBus) Call(event string, payload any) []HookResult {
	var out []HookResult
	for _, h := range b.snapshot(event) {
		r, ok := h.fn(context.Background(), payload).(hookReply)
		if !ok || r.value == nil {
			continue
		}
		out = append(out, HookResult{PluginID: r.pluginID, Value: r.value})
	}
	return out
}

var Bus = NewEventBus()

type InstanceEvent struct {
	Name string `json:"name"`
}

type LaunchEvent struct {
	Instance string `json:"instance"`
	Version  string `json:"version"`
	ExitCode int    `json:"exitCode,omitempty"`
}

type DownloadEvent struct {
	URL  string `json:"url"`
	Dest string `json:"dest"`
}

type ModpackEvent struct {
	Instance string `json:"instance"`
}

type AccountEvent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type WorldEvent struct {
	Instance string `json:"instance"`
	World    string `json:"world"`
}
