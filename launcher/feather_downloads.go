package launcher

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func featherEnsureFile(client string, lib featherLib, tr *tracker) (string, bool, error) {
	if lib.URL == "" || lib.Name == "" {
		return "", false, nil
	}
	dir := featherJarDir(client, lib.SHA1)
	dest := filepath.Join(dir, filepath.Base(lib.Name))
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		if lib.SHA1 != "" && lib.Size == 0 {
			if sum, err := sha1File(dest); err == nil && sum == lib.SHA1 {
				return dest, featherHasFabricMod(dest), nil
			}
		} else if lib.Size == 0 {
			return dest, featherHasFabricMod(dest), nil
		} else if fi.Size() == lib.Size {
			return dest, featherHasFabricMod(dest), nil
		}
	}
	if err := downloadTo(lib.URL, dest, lib.Size, tr); err != nil {
		return "", false, err
	}
	if lib.SHA1 != "" {
		sum, err := sha1File(dest)
		if err == nil && sum != lib.SHA1 {
			os.Remove(dest)
			return "", false, fmt.Errorf("feather %s: sha1 mismatch", filepath.Base(dest))
		}
	}
	return dest, featherHasFabricMod(dest), nil
}

func featherDownloadLibs(client string, m *featherManifest, instDir string, log LogFn, onProgress ProgressFn) ([]string, error) {
	modsDir := filepath.Join(instDir, "mods")
	os.MkdirAll(modsDir, 0o755)
	libs := append(append([]featherLib{}, m.Libraries...), m.Mods...)
	jobs := make([]job, 0, len(libs))
	for _, lib := range libs {
		if lib.URL == "" || lib.Name == "" {
			continue
		}
		dest := filepath.Join(featherJarDir(client, lib.SHA1), filepath.Base(lib.Name))
		jobs = append(jobs, job{url: lib.URL, dest: dest, size: lib.Size, sha1: lib.SHA1})
	}
	var total int64
	for _, j := range jobs {
		total += j.size
	}
	tr := newTracker("feather-libs", onProgress)
	tr.setTotal(int64(len(jobs)), total)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	tr.emit()
	if firstErr != nil {
		return nil, firstErr
	}
	cp := make([]string, 0, len(libs))
	seen := map[string]bool{}
	for _, lib := range m.Libraries {
		if lib.URL == "" || lib.Name == "" {
			continue
		}
		dest := filepath.Join(featherJarDir(client, lib.SHA1), filepath.Base(lib.Name))
		if _, err := os.Stat(dest); err != nil {
			return nil, fmt.Errorf("feather lib missing after download: %s", filepath.Base(dest))
		}
		isMod := featherHasFabricMod(dest)
		isMod = isMod && !strings.Contains(filepath.Base(dest), "fabric-loader")
		if lib.ClassPath != nil && !*lib.ClassPath {
			isMod = true
		}
		if isMod {
			if err := syncClientMod(dest, modsDir, log); err != nil {
				return nil, err
			}
			continue
		}
		if !seen[dest] {
			seen[dest] = true
			cp = append(cp, dest)
		}
	}
	for _, mod := range m.Mods {
		if mod.URL == "" || mod.Name == "" {
			continue
		}
		dest := filepath.Join(featherJarDir(client, mod.SHA1), filepath.Base(mod.Name))
		if _, err := os.Stat(dest); err != nil {
			return nil, fmt.Errorf("feather mod missing after download: %s", filepath.Base(dest))
		}
		if err := syncClientMod(dest, modsDir, log); err != nil {
			return nil, err
		}
	}
	if len(cp) == 0 {
		return nil, fmt.Errorf("feather: no classpath jars")
	}
	return cp, nil
}

// syncClientMod copies a client-bundled mod into the instance mods dir,
// respecting a previous user delete or disable. Anything the client declares
// in its manifest mods list belongs in mods/, not on the classpath where the
// loader would ignore it.
func syncClientMod(dest, modsDir string, log LogFn) error {
	target := filepath.Join(modsDir, filepath.Base(dest))
	_, targetErr := os.Stat(target)
	_, disabledErr := os.Stat(target + ".disabled")
	switch {
	case targetErr == nil && disabledErr == nil:
		os.Remove(target)
		log("    [mod] " + filepath.Base(dest) + " stays disabled")
	case targetErr == nil || disabledErr == nil:
	default:
		if err := copyFile(dest, target); err != nil {
			return fmt.Errorf("copy mod %s: %w", filepath.Base(dest), err)
		}
		log("    [mod] " + filepath.Base(dest) + " -> mods/")
	}
	return nil
}

func featherDownloadNatives(client, version string, m *featherManifest, onProgress ProgressFn) ([]string, string, error) {
	nativesDir := filepath.Join(featherBaseDir(client), "natives", version)
	os.MkdirAll(nativesDir, 0o755)
	type nativeJob struct {
		dest string
		info featherNative
	}
	var njs []nativeJob
	jobs := make([]job, 0)
	for _, nl := range m.NativeLibraries {
		info, ok := nl.Natives[featherPlatformKey()]
		if !ok || info.URL == "" {
			continue
		}
		dest := filepath.Join(featherJarDir(client, info.SHA1), filepath.Base(info.Name))
		jobs = append(jobs, job{url: info.URL, dest: dest, size: 0})
		njs = append(njs, nativeJob{dest: dest, info: info})
	}
	tr := newTracker("feather-natives", onProgress)
	tr.setTotal(int64(len(jobs)), 0)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	tr.emit()
	if firstErr != nil {
		return nil, "", firstErr
	}
	cp := make([]string, 0, len(njs))
	for _, nj := range njs {
		cp = append(cp, nj.dest)
		var allowed map[string]bool
		if len(nj.info.ExtractedFiles) > 0 {
			allowed = map[string]bool{}
			for _, ef := range nj.info.ExtractedFiles {
				allowed[filepath.ToSlash(ef.Name)] = true
			}
		}
		extractNativesFromJar(nj.dest, nativesDir, allowed)
	}
	return cp, nativesDir, nil
}

func featherPlatformKey() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	if runtime.GOOS == "darwin" {
		if runtime.GOARCH == "arm64" {
			return "osx-arm"
		}
		return "osx"
	}
	return "linux"
}

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func mirrorFile(src, dst string) {
	if _, err := os.Stat(dst); err != nil {
		os.MkdirAll(filepath.Dir(dst), 0o755)
		linkOrCopy(src, dst)
	}
}

func featherEnsureMojangAssets(m *featherManifest, instDir string, onProgress ProgressFn) error {
	idx := m.GameAssetsIndex
	if idx.URL == "" || idx.Name == "" {
		return nil
	}
	indexID := strings.TrimSuffix(idx.Name, ".json")
	sharedIndex := filepath.Join(assetsDir(), "indexes", indexID+".json")
	indexesDir := filepath.Join(instDir, "assets", "indexes")
	os.MkdirAll(indexesDir, 0o755)
	indexFile := filepath.Join(indexesDir, indexID+".json")
	if _, err := os.Stat(indexFile); err == nil {
		mirrorFile(indexFile, sharedIndex)
	} else {
		mirrorFile(sharedIndex, indexFile)
	}
	if _, err := os.Stat(sharedIndex); err != nil {
		if err := downloadTo(idx.URL, sharedIndex, 0, nil); err != nil {
			return err
		}
		mirrorFile(sharedIndex, indexFile)
	}
	if name := featherAssetIndexArg(m.MinecraftArguments); name != "" && name != indexID {
		data, _ := os.ReadFile(indexFile)
		os.WriteFile(filepath.Join(indexesDir, name+".json"), data, 0o644)
	}
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return err
	}
	var vindex struct {
		Objects map[string]struct {
			Hash string `json:"hash"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(data, &vindex); err != nil {
		return err
	}
	sharedObjects := filepath.Join(assetsDir(), "objects")
	instObjects := filepath.Join(instDir, "assets", "objects")
	jobs := make([]job, 0, len(vindex.Objects))
	var total int64
	for _, o := range vindex.Objects {
		if len(o.Hash) < 2 {
			continue
		}
		rel := filepath.Join(o.Hash[:2], o.Hash)
		shared := filepath.Join(sharedObjects, rel)
		if inst, err := os.Stat(filepath.Join(instObjects, rel)); err == nil && inst.Size() > 0 {
			mirrorFile(filepath.Join(instObjects, rel), shared)
		}
		jobs = append(jobs, job{
			url:  fmt.Sprintf("https://resources.download.minecraft.net/%s/%s", o.Hash[:2], o.Hash),
			dest: shared,
			size: o.Size,
		})
		total += o.Size
	}
	tr := newTracker("feather-assets", onProgress)
	tr.setTotal(int64(len(jobs)), total)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	tr.emit()
	if firstErr != nil {
		return firstErr
	}
	for _, j := range jobs {
		rel, err := filepath.Rel(sharedObjects, j.dest)
		if err != nil {
			continue
		}
		mirrorFile(j.dest, filepath.Join(instObjects, rel))
	}
	return nil
}

var assetIndexArgRe = regexp.MustCompile(`--assetIndex\s+(\S+)`)

func featherAssetIndexArg(args string) string {
	m := assetIndexArgRe.FindStringSubmatch(args)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func featherEnsureClientAssets(m *featherManifest, client, instDir string, onProgress ProgressFn) error {
	idx := m.AssetsIndex
	if idx.URL == "" {
		return nil
	}
	idxPath := filepath.Join(featherBaseDir(client), "assets", idx.Name)
	if _, err := os.Stat(idxPath); err != nil {
		if err := downloadTo(idx.URL, idxPath, 0, nil); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return err
	}
	var items []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	base := featherAssetBaseURL
	if client == "dawn" {
		base = dawnAssetBaseURL
	}
	sharedBase := filepath.Join(featherBaseDir(client), "extracted", idx.Name)
	instBase := filepath.Join(instDir, "feather")
	jobs := make([]job, 0, len(items))
	var total int64
	for _, it := range items {
		url := it.URL
		if url == "" {
			url = base + it.Name
		}
		dest, ok := safeExtractPath(sharedBase, it.Name)
		if !ok {
			continue
		}
		if inst, ok := safeExtractPath(instBase, it.Name); ok {
			if fi, err := os.Stat(inst); err == nil && fi.Size() > 0 {
				mirrorFile(inst, dest)
			}
		}
		size := int64(0)
		jobs = append(jobs, job{url: url, dest: dest, size: size})
	}
	tr := newTracker("feather-ui-assets", onProgress)
	tr.setTotal(int64(len(jobs)), total)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	tr.emit()
	if firstErr != nil {
		return firstErr
	}
	for _, j := range jobs {
		rel, err := filepath.Rel(sharedBase, j.dest)
		if err != nil {
			continue
		}
		if inst, ok := safeExtractPath(instBase, rel); ok {
			mirrorFile(j.dest, inst)
		}
	}
	return nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	os.MkdirAll(filepath.Dir(dest), 0o755)
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func extractNativesFromJar(jar, dir string, allowed map[string]bool) {
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if name == "" || f.FileInfo().IsDir() {
			continue
		}
		if allowed != nil {
			if !allowed[name] || strings.HasPrefix(name, "META-INF/") {
				continue
			}
		} else if !strings.HasSuffix(strings.ToLower(name), ".dll") &&
			!strings.HasSuffix(strings.ToLower(name), ".so") &&
			!strings.HasSuffix(strings.ToLower(name), ".dylib") &&
			!strings.HasSuffix(strings.ToLower(name), ".jnilib") {
			continue
		}
		target, ok := safeExtractPath(dir, name)
		if !ok {
			continue
		}
		if _, err := os.Stat(target); err == nil {
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(target)
		if err == nil {
			io.Copy(out, rc)
			out.Close()
		}
		rc.Close()
	}
}
