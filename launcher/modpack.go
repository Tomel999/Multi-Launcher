package launcher

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"multilauncherwails/launcher/plugin"
)

type ModpackFile struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

type ModpackInfo struct {
	MCVersion     string `json:"mcVersion"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
}

func DownloadFile(url, dest string, onProgress ProgressFn) error {
	tr := newTracker("modpack", onProgress)
	tr.setTotal(1, 0)
	defer tr.emit()
	return downloadTo(url, dest, 0, tr)
}

func safeRelPath(name string, mode os.FileMode) (string, error) {
	if mode&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink in archive: %s", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path in archive: %s", name)
	}
	return clean, nil
}

func stripOverrides(name string) string {
	for _, p := range []string{"overrides/", "client-overrides/"} {
		if strings.HasPrefix(name, p) {
			return name[len(p):]
		}
	}
	return name
}

func extractOne(f *zip.File, name, destAbs string) error {
	rel, err := safeRelPath(name, f.FileInfo().Mode())
	if err != nil {
		return err
	}
	out := filepath.Join(destAbs, rel)
	if !strings.HasPrefix(out, destAbs+string(os.PathSeparator)) && out != destAbs {
		return fmt.Errorf("unsafe path in archive: %s", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(out, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, rc)
	return err
}

func unzipAll(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	var clientOverrides []*zip.File
	for _, f := range zr.File {
		name := stripOverrides(f.Name)
		if name == "" {
			continue
		}
		if strings.HasPrefix(f.Name, "client-overrides/") {
			clientOverrides = append(clientOverrides, f)
			continue
		}
		if err := extractOne(f, name, destAbs); err != nil {
			return err
		}
	}
	for _, f := range clientOverrides {
		name := stripOverrides(f.Name)
		if name == "" {
			continue
		}
		if err := extractOne(f, name, destAbs); err != nil {
			return err
		}
	}
	return nil
}

func MigrateLegacyOverrides(instDir string) error {
	for _, dir := range []string{"overrides", "client-overrides"} {
		src := filepath.Join(instDir, dir)
		info, err := os.Lstat(src)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := copyTree(src, instDir); err != nil {
			return err
		}
		if err := os.RemoveAll(src); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		out := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		w, err := os.Create(out)
		if err != nil {
			return err
		}
		defer w.Close()
		_, err = io.Copy(w, in)
		return err
	})
}

func InstallModpack(instName, packZip string, files []ModpackFile, onProgress ProgressFn) error {
	inst := InstanceDir(instName)
	for _, r := range plugin.Bus.Call("modpack.beforeInstall", plugin.ModpackEvent{Instance: instName}) {
		if cancel, _ := r.Value["cancel"].(bool); cancel {
			reason, _ := r.Value["reason"].(string)
			return fmt.Errorf("modpack install canceled by plugin %s: %s", r.PluginID, reason)
		}
	}
	if err := unzipAll(packZip, inst); err != nil {
		return fmt.Errorf("extract modpack: %w", err)
	}
	jobs := make([]job, 0, len(files))
	var totalBytes int64
	for _, f := range files {
		rel, err := safeRelPath(f.Path, 0)
		if err != nil {
			return fmt.Errorf("unsafe modpack file path: %w", err)
		}
		jobs = append(jobs, job{url: f.URL, dest: filepath.Join(inst, rel), size: f.Size})
		totalBytes += f.Size
	}
	tr := newTracker("modpack", onProgress)
	tr.setTotal(int64(len(jobs)), totalBytes)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	if firstErr != nil {
		return firstErr
	}
	tr.emit()
	plugin.Bus.Emit("modpack.afterInstall", plugin.ModpackEvent{Instance: instName})
	return nil
}

type modrinthIndex struct {
	VersionID    string            `json:"versionId"`
	Dependencies map[string]string `json:"dependencies"`
	Files        []struct {
		Path      string   `json:"path"`
		Downloads []string `json:"downloads"`
		FileSize  int64    `json:"fileSize"`
	} `json:"files"`
}

func readModrinthIndex(zipPath string) ([]ModpackFile, ModpackInfo, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, ModpackInfo{}, err
	}
	defer zr.Close()
	var idx modrinthIndex
	found := false
	for _, f := range zr.File {
		if f.Name != "modrinth.index.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, ModpackInfo{}, err
		}
		err = json.NewDecoder(rc).Decode(&idx)
		rc.Close()
		if err != nil {
			return nil, ModpackInfo{}, err
		}
		found = true
		break
	}
	if !found {
		return nil, ModpackInfo{}, fmt.Errorf("no modrinth.index.json in pack")
	}
	info := ModpackInfo{MCVersion: idx.Dependencies["minecraft"]}
	for key, ver := range idx.Dependencies {
		switch key {
		case "fabric-loader":
			info.Loader, info.LoaderVersion = "Fabric", ver
		case "quilt-loader":
			info.Loader, info.LoaderVersion = "Quilt", ver
		case "forge":
			info.Loader, info.LoaderVersion = "Forge", ver
		case "neoforge":
			info.Loader, info.LoaderVersion = "NeoForge", ver
		}
	}
	if info.Loader == "" {
		info.Loader = "Vanilla"
	}
	if info.MCVersion == "" {
		info.MCVersion = idx.VersionID
	}
	var files []ModpackFile
	for _, f := range idx.Files {
		if len(f.Downloads) == 0 {
			continue
		}
		files = append(files, ModpackFile{Path: f.Path, URL: f.Downloads[0], Size: f.FileSize})
	}
	return files, info, nil
}

type curseForgeManifest struct {
	Minecraft struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID      string `json:"id"`
			Primary bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	Files []struct {
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
	} `json:"files"`
}

func readCurseForgeManifest(zipPath, apiKey string) ([]ModpackFile, ModpackInfo, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, ModpackInfo{}, err
	}
	defer zr.Close()
	var m curseForgeManifest
	found := false
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, ModpackInfo{}, err
		}
		err = json.NewDecoder(rc).Decode(&m)
		rc.Close()
		if err != nil {
			return nil, ModpackInfo{}, err
		}
		found = true
		break
	}
	if !found {
		return nil, ModpackInfo{}, fmt.Errorf("no manifest.json in pack")
	}
	info := ModpackInfo{MCVersion: m.Minecraft.Version}
	for _, ml := range m.Minecraft.ModLoaders {
		if ml.Primary {
			if i := strings.IndexByte(ml.ID, '-'); i > 0 {
				info.Loader, info.LoaderVersion = canonicalLoader(ml.ID[:i]), ml.ID[i+1:]
			}
			break
		}
	}
	if info.Loader == "" {
		info.Loader = "Vanilla"
	}
	files, err := resolveCurseForgeFiles(apiKey, &m)
	return files, info, err
}

func canonicalLoader(name string) string {
	switch name {
	case "fabric":
		return "Fabric"
	case "quilt":
		return "Quilt"
	case "forge":
		return "Forge"
	case "neoforge":
		return "NeoForge"
	}
	return name
}

func FetchAndInstallModpack(instName, source, packURL, apiKey string, onProgress ProgressFn) (ModpackInfo, error) {
	if err := MigrateLegacyOverrides(InstanceDir(instName)); err != nil {
		return ModpackInfo{}, fmt.Errorf("migrate overrides: %w", err)
	}

	marker := filepath.Join(InstanceDir(instName), "modpack-installed.json")
	if data, err := os.ReadFile(marker); err == nil {
		var cached ModpackInfo
		if json.Unmarshal(data, &cached) == nil && cached.MCVersion != "" {
			return cached, nil
		}
	}

	tmp, err := os.CreateTemp("", "modpack-*.zip")
	if err != nil {
		return ModpackInfo{}, err
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)

	if err := DownloadFile(packURL, tmpName, onProgress); err != nil {
		return ModpackInfo{}, err
	}

	var files []ModpackFile
	var info ModpackInfo
	switch source {
	case "modrinth":
		files, info, err = readModrinthIndex(tmpName)
	case "curseforge":
		files, info, err = readCurseForgeManifest(tmpName, apiKey)
	default:
		err = fmt.Errorf("unknown modpack source %q", source)
	}
	if err != nil {
		return ModpackInfo{}, err
	}

	if err := InstallModpack(instName, tmpName, files, onProgress); err != nil {
		return ModpackInfo{}, err
	}
	if data, err := json.Marshal(info); err == nil {
		os.MkdirAll(filepath.Dir(marker), 0o755)
		os.WriteFile(marker, data, 0o644)
	}
	return info, nil
}
