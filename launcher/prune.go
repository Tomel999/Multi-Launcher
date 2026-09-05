package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PruneReport describes what PruneOrphanedFiles removed for one Minecraft
// version. Removed holds paths relative to the shared root.
type PruneReport struct {
	Version    string   `json:"version"`
	Removed    []string `json:"removed"`
	FreedBytes int64    `json:"freedBytes"`
}

// PruneOrphanedFiles deletes the shared files of mcVersion that no remaining
// version needs: versions/<v>/, natives/<v>/, the asset index, and the
// version's libraries that no other version references.
//
// usedVersions lists the mcVersions still in use; the caller excludes the
// deleted instances. When mcVersion is still used, nothing happens.
//
// Libraries are deleted by reference counting, not by directory: libraries/
// is a shared maven layout, so only files listed in the deleted version's
// JSON and in no remaining version's JSON are removed. Fabric/Quilt loader
// jars and Forge installer products never appear in a vanilla version JSON,
// so they are never touched here.
func PruneOrphanedFiles(mcVersion string, usedVersions map[string]bool) (PruneReport, error) {
	rep := PruneReport{Version: mcVersion}
	if mcVersion == "" || usedVersions[mcVersion] {
		return rep, nil
	}

	meta, err := readVersionMeta(mcVersion)
	if err != nil {
		meta = nil
	}

	kept := map[string]bool{}
	if entries, err := os.ReadDir(versionsRoot()); err == nil {
		for _, e := range entries {
			if !e.IsDir() || e.Name() == mcVersion {
				continue
			}
			other, err := readVersionMeta(e.Name())
			if err != nil || other == nil {
				// An unreadable remaining version may reference anything;
				// skip the libraries phase rather than risk its files.
				kept = nil
				break
			}
			for _, p := range libraryPaths(other) {
				kept[p] = true
			}
		}
	}

	if meta != nil && kept != nil {
		for _, p := range libraryPaths(meta) {
			if kept[p] {
				continue
			}
			rep.remove(filepath.Join(librariesRoot(), filepath.FromSlash(p)))
		}
		rep.pruneEmptyDirs(librariesRoot())
	}

	if meta != nil && strings.TrimSpace(meta.AssetIndex.ID) != "" {
		rep.remove(filepath.Join(assetsRoot(), "indexes", meta.AssetIndex.ID+".json"))
	}
	rep.removeDir(filepath.Join(root(), "natives", mcVersion))
	rep.removeDir(filepath.Join(versionsRoot(), mcVersion))
	return rep, nil
}

// PruneLoaderJar deletes loaders/<loader>/<loaderVersion>.jar when no
// remaining instance uses that loader build. Loader names keep the frontend
// casing ("Forge", "NeoForge").
func PruneLoaderJar(loader, loaderVersion string, usedLoaders map[string]bool) (PruneReport, error) {
	rep := PruneReport{Version: loader + " " + loaderVersion}
	if loader == "" || loader == "Vanilla" || loaderVersion == "" {
		return rep, nil
	}
	if usedLoaders[loader+"\x00"+loaderVersion] {
		return rep, nil
	}
	rep.remove(filepath.Join(root(), "loaders", loader, loaderVersion+".jar"))
	rep.pruneEmptyDirs(filepath.Join(root(), "loaders"))
	return rep, nil
}

func readVersionMeta(version string) (*VersionMeta, error) {
	data, err := os.ReadFile(filepath.Join(versionsRoot(), version, version+".json"))
	if err != nil {
		return nil, err
	}
	var meta VersionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// versionsRoot, librariesRoot and assetsRoot mirror versionsDir,
// librariesDir and assetsDir, but go through the overridable root() so tests
// can redirect them. In production both spell the same directory.
func versionsRoot() string  { return filepath.Join(root(), "versions") }
func librariesRoot() string { return filepath.Join(root(), "libraries") }
func assetsRoot() string    { return filepath.Join(root(), "assets") }

// libraryPaths lists every file a version JSON references inside libraries/:
// artifacts and native classifiers alike.
func libraryPaths(meta *VersionMeta) []string {
	var out []string
	for _, lib := range meta.Libraries {
		if lib.Downloads.Artifact.Path != "" {
			out = append(out, lib.Downloads.Artifact.Path)
		}
		for _, c := range lib.Downloads.Classifiers {
			if c.Path != "" {
				out = append(out, c.Path)
			}
		}
	}
	return out
}

// remove deletes a single file, accounting its size. Missing files are not
// an error: a partial download may simply lack them.
func (r *PruneReport) remove(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return
	}
	if err := os.Remove(path); err != nil {
		return
	}
	r.FreedBytes += fi.Size()
	if rel, err := filepath.Rel(root(), path); err == nil {
		r.Removed = append(r.Removed, filepath.ToSlash(rel))
	} else {
		r.Removed = append(r.Removed, path)
	}
}

// removeDir deletes a whole directory tree, accounting its size first.
// Missing directories are ignored and never reported as removed.
func (r *PruneReport) removeDir(path string) {
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return
	}
	r.FreedBytes += dirSize(path)
	if err := os.RemoveAll(path); err != nil {
		return
	}
	if rel, err := filepath.Rel(root(), path); err == nil {
		r.Removed = append(r.Removed, filepath.ToSlash(rel))
	}
}

// pruneEmptyDirs removes empty directories under base, bottom-up. os.Remove
// refuses non-empty directories, so populated branches are left alone.
func (r *PruneReport) pruneEmptyDirs(base string) {
	var dirs []string
	filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && p != base {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
}
