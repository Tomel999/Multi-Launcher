package plugin

import (
	"fmt"
	"strings"
)

const (
	PermEvents          = "events:*"
	PermFSInstanceRead  = "fs:instance:read"
	PermFSInstanceWrite = "fs:instance:write"
	PermFSModsWrite     = "fs:mods:write"
	PermNetworkCustom   = "network:custom"
	PermUITheme         = "ui:theme"
	PermUISlots         = "ui:slots"
	PermUINotifications = "ui:notifications"
	PermAccountsRead    = "accounts:read"
	PermInstancesRead   = "instances:read"
	PermModsRead        = "mods:read"
	PermWorldsRead      = "worlds:read"
	PermLogsRead        = "logs:read"
	PermUIViews         = "ui:views"
	PermUIOverride      = "ui:override"
	PermLaunchStart     = "launch:start"
	PermInstancesOpen   = "instances:open"
	PermRuntimeWasm     = "runtime:wasm"
)

var AllPermissions = []string{
	PermEvents,
	PermFSInstanceRead,
	PermFSInstanceWrite,
	PermFSModsWrite,
	PermNetworkCustom,
	PermUITheme,
	PermUISlots,
	PermUINotifications,
	PermAccountsRead,
	PermInstancesRead,
	PermModsRead,
	PermWorldsRead,
	PermLogsRead,
	PermUIViews,
	PermUIOverride,
	PermLaunchStart,
	PermInstancesOpen,
	PermRuntimeWasm,
}

func HasPermission(granted []string, perm string) bool {
	for _, g := range granted {
		if g == perm {
			return true
		}
		if strings.HasSuffix(g, ":*") && strings.HasPrefix(perm, strings.TrimSuffix(g, "*")) {
			return true
		}
	}
	return false
}

func AddedPermissions(prev, next []string) []string {
	var out []string
	for _, p := range next {
		if !HasPermission(prev, p) {
			out = append(out, p)
		}
	}
	return out
}

func ValidatePermissions(perms []string) error {
	for _, p := range perms {
		known := false
		for _, a := range AllPermissions {
			if a == p {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown permission %q", p)
		}
	}
	return nil
}
