package launcher

import (
	"encoding/json"

	"multilauncherwails/launcher/plugin"
)

var patchableFields = map[string]bool{
	"memory":    true,
	"minMemory": true,
	"width":     true,
	"height":    true,
	"server":    true,
}

func ApplyLaunchHooks(log func(string), state func(bool, int, string), instName, version string, opts LaunchOptions) *LaunchOptions {
	for _, r := range plugin.Bus.Call("launch.beforePrepare", plugin.LaunchEvent{Instance: instName, Version: version}) {
		if cancel, _ := r.Value["cancel"].(bool); cancel {
			reason, _ := r.Value["reason"].(string)
			log("Launch canceled by plugin " + r.PluginID + ": " + reason)
			state(false, 0, "canceled by plugin "+r.PluginID+": "+reason)
			return nil
		}
		patchRaw, ok := r.Value["patch"]
		if !ok {
			continue
		}
		patch, ok := patchRaw.(map[string]any)
		if !ok || len(patch) == 0 {
			continue
		}
		data, err := json.Marshal(opts)
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		for k, v := range patch {
			if !patchableFields[k] {
				log("Launch patch from plugin " + r.PluginID + " ignored: field " + k + " is not patchable")
				continue
			}
			m[k] = v
		}
		merged, err := json.Marshal(m)
		if err != nil {
			continue
		}
		var next LaunchOptions
		if json.Unmarshal(merged, &next) != nil {
			continue
		}
		opts = next
		log("Launch options patched by plugin " + r.PluginID)
	}
	return &opts
}
