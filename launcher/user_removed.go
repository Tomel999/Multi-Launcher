package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const userRemovedFile = ".user-removed.json"
const userModsFile = ".user-mods.json"

func readStringSet(path string) map[string]bool {
	set := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return set
	}
	var list []string
	if json.Unmarshal(data, &list) != nil {
		return set
	}
	for _, p := range list {
		p = filepath.ToSlash(p)
		if p != "" && !filepath.IsAbs(p) && !strings.Contains(p, "..") {
			set[p] = true
		}
	}
	return set
}

func writeStringSet(path string, set map[string]bool) {
	list := make([]string, 0, len(set))
	for p := range set {
		list = append(list, p)
	}
	sort.Strings(list)
	if data, err := json.Marshal(list); err == nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, data, 0o644)
	}
}

func userRemovedSet(dir string) map[string]bool {
	return readStringSet(filepath.Join(dir, userRemovedFile))
}

func writeUserRemoved(dir string, set map[string]bool) {
	writeStringSet(filepath.Join(dir, userRemovedFile), set)
}

func userModsSet(dir string) map[string]bool {
	return readStringSet(filepath.Join(dir, userModsFile))
}

func writeUserMods(dir string, set map[string]bool) {
	writeStringSet(filepath.Join(dir, userModsFile), set)
}

func normalizeModRel(rel string) (string, bool) {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".disabled")
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", false
	}
	return rel, true
}

// MarkUserRemoved records an explicit user delete from a managed mod folder
// (Lunar base modpack, Ogulniega) so the next sync doesn't resurrect the
// file. It also drops the file from the user-managed set: deleting wins.
func MarkUserRemoved(dir, rel string) {
	rel, ok := normalizeModRel(rel)
	if !ok {
		return
	}
	if um := userModsSet(dir); um[rel] {
		delete(um, rel)
		writeUserMods(dir, um)
	}
	set := userRemovedSet(dir)
	if set[rel] {
		return
	}
	set[rel] = true
	writeUserRemoved(dir, set)
}

func unmarkUserRemoved(dir, rel string) {
	set := userRemovedSet(dir)
	if !set[rel] {
		return
	}
	delete(set, rel)
	writeUserRemoved(dir, set)
}

// markUserMod takes over a file into user management: the sync verifies,
// updates and deletes everything except these. Used when a previously
// removed manifest mod reappears (manual re-add) — from then on the user
// owns it and the launcher leaves it alone.
func markUserMod(dir, rel string) {
	rel, ok := normalizeModRel(rel)
	if !ok {
		return
	}
	unmarkUserRemoved(dir, rel)
	set := userModsSet(dir)
	if set[rel] {
		return
	}
	set[rel] = true
	writeUserMods(dir, set)
}
