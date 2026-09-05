package main

import "testing"

func TestParseInstanceUsage(t *testing.T) {
	data := []byte(`{"settings":{},"groups":[
		{"name":"g","instances":[
			{"name":"A","meta":{"mcVersion":"26.2","loader":"Fabric","loaderVersion":"0.16.0"}},
			{"name":"B","meta":{"mcVersion":"26.2","loader":"Vanilla"}},
			{"name":"C","meta":{"mcVersion":"26.1","loader":"Forge","loaderVersion":"47.2.0"}},
			{"name":"D","meta":{}}
		]}
	]}`)
	u, err := parseInstanceUsage(data, map[string]bool{"A": true, "B": true})
	if err != nil {
		t.Fatalf("parseInstanceUsage: %v", err)
	}
	if u.versions["26.2"] {
		t.Error("26.2 is only used by doomed instances and must not be guarded")
	}
	if !u.versions["26.1"] {
		t.Error("26.1 is used by a remaining instance and must be guarded")
	}
	if u.loaders["Fabric\x000.16.0"] {
		t.Error("doomed loader build must not be guarded")
	}
	if !u.loaders["Forge\x0047.2.0"] {
		t.Error("remaining loader build must be guarded")
	}
}
