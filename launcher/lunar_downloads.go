package launcher

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// lunarChannelStamp identifies the build channel the cached Lunar artifacts
// were fetched for. Lunar's server serves canary (preview) builds when
// canary_preference is "NEUTRAL" — the previous default — so builds cached
// under that behaviour are canary even though branch was "master".
const lunarChannelStamp = "stable-optout"

// lunarEnsureStableChannel invalidates cached Lunar builds that were
// downloaded while the launcher still sent canary_preference=NEUTRAL. The
// artifact cache never re-downloads a cached build (Lunar serves a rotating
// set of builds under the same artifact names), so canary builds cached
// earlier would otherwise be reused forever. A marker file records the
// channel; when it is missing or stale the offline folder is wiped once and
// the stable builds are re-downloaded.
func lunarEnsureStableChannel(base string, log LogFn) {
	marker := filepath.Join(base, ".build-channel")
	if data, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(data)) == lunarChannelStamp {
		return
	}
	log("  Build channel pinned to stable — re-downloading Lunar files once")
	os.RemoveAll(base)
	os.MkdirAll(base, 0o755)
	_ = os.WriteFile(marker, []byte(lunarChannelStamp), 0o644)
}

// lunarDownloadArtifacts mirrors lunar.py ArtifactManager.download. Files are
// cached by artifact name and validated against a sidecar manifest
// (.artifact-hashes.json) that records the sha1 of the content actually
// downloaded. Lunar's launch API returns an unstable per-request sha1/size
// (the CDN serves a rotating set of valid builds), so the API's hashes cannot
// be used for cache validation — but a *locally recorded* hash of the fetched
// bytes can. This catches non-empty-but-truncated downloads (interrupted
// connections, disk-full) that a size==0 check would miss, while still never
// re-downloading a cached build.
// Returns (classpathJars, classpathNames, externalsNames, nativesDirs).
func lunarDownloadArtifacts(artifacts []lunarArtifact, log LogFn, onProgress ProgressFn) ([]string, []string, []string, []string, error) {
	var classpathJars, classpathNames, externalsNames, nativesDirs []string
	base := lunarOfflineDir()
	lunarEnsureStableChannel(base, log)
	tr := newTracker("lunar", onProgress)
	var totalBytes int64
	for _, art := range artifacts {
		if art.URL != "" {
			totalBytes += art.Size
		}
	}
	tr.setTotal(int64(len(artifacts)), totalBytes)
	cachePath := filepath.Join(base, ".artifact-hashes.json")
	cache := map[string]string{}
	if data, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	saveCache := func() {
		if data, err := json.Marshal(cache); err == nil {
			_ = os.WriteFile(cachePath, data, 0o644)
		}
	}
	defer saveCache()
	for _, art := range artifacts {
		localPath := filepath.Join(base, filepath.FromSlash(art.Name))
		os.MkdirAll(filepath.Dir(localPath), 0o755)
		// Lunar's launch API returns an unstable per-request sha1/size (the
		// CDN serves a rotating set of valid builds), so those fields cannot
		// be used for cache validation *every run*. Instead:
		//   - If our sidecar manifest records a hash for this artifact, the
		//     file must match it (catches truncated/corrupt downloads).
		//   - Otherwise (first run after an upgrade, lost manifest), fall
		//     back to the API sha1 once: stale files from old launcher
		//     versions are replaced, matching files are adopted into the
		//     manifest. From then on the recorded hash rules.
		need := false
		if fi, err := os.Stat(localPath); err != nil || fi.Size() == 0 {
			need = true
			if err != nil {
				log("  Updating " + art.Name + " (not cached)")
			} else {
				log("  Updating " + art.Name + " (corrupt/empty)")
			}
		} else if want, ok := cache[art.Name]; ok && want != "" {
			if h, err := sha1File(localPath); err != nil || !strings.EqualFold(h, want) {
				need = true
				log("  Updating " + art.Name + " (hash mismatch — re-downloading)")
			}
		} else if !ok {
			if h, err := sha1File(localPath); err != nil {
				need = true
				log("  Updating " + art.Name + " (unreadable)")
			} else if art.SHA1 != "" && !strings.EqualFold(h, art.SHA1) {
				need = true
				log("  Updating " + art.Name + " (stale, API sha1 mismatch)")
			} else {
				// Adopt the existing file into the manifest so future runs
				// validate against this recorded hash.
				cache[art.Name] = strings.ToLower(h)
			}
		}
		if need {
			if art.URL == "" {
				log("  No URL for " + art.Name + ", skipping")
				tr.fileDone(filepath.Base(localPath))
				continue
			}
			// Drop the old file so downloadTo cannot skip it on a size match.
			os.Remove(localPath)
			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				if err := downloadTo(art.URL, localPath, art.Size, tr); err != nil {
					lastErr = err
					log(fmt.Sprintf("    Attempt %d: %v", attempt+1, err))
					continue
				}
				lastErr = nil
				break
			}
			if lastErr != nil {
				return nil, nil, nil, nil, fmt.Errorf("failed to download %s: %w", art.Name, lastErr)
			}
			if fi, err := os.Stat(localPath); err != nil || fi.Size() == 0 {
				os.Remove(localPath)
				return nil, nil, nil, nil, fmt.Errorf("download produced no file for %s", art.Name)
			}
			// Record what we actually downloaded so future cache hits can be
			// verified even though the API's own hashes are unstable.
			if h, err := sha1File(localPath); err == nil {
				cache[art.Name] = h
			}
		} else {
			tr.fileDone(filepath.Base(localPath))
		}
		switch art.Type {
		case "CLASS_PATH":
			classpathJars = append(classpathJars, localPath)
			classpathNames = append(classpathNames, art.Name)
		case "EXTERNAL_FILE":
			externalsNames = append(externalsNames, art.Name)
		case "NATIVES":
			nativesDir := filepath.Join(base, "natives")
			os.MkdirAll(nativesDir, 0o755)
			sum := sha1.Sum([]byte(art.Name))
			marker := filepath.Join(nativesDir, ".extracted_"+hex.EncodeToString(sum[:])[:8])
			if _, err := os.Stat(marker); err != nil {
				log("    Extracting: " + art.Name)
				if err := extractZipTree(localPath, nativesDir); err != nil {
					return nil, nil, nil, nil, fmt.Errorf("extract natives %s: %w", art.Name, err)
				}
				os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
			}
			if !slices.Contains(nativesDirs, nativesDir) {
				nativesDirs = append(nativesDirs, nativesDir)
			}
		}
	}
	return classpathJars, classpathNames, externalsNames, nativesDirs, nil
}

// lunarExtractWebosr unpacks ul-resources.zip into natives/web (Ultralight).
func lunarExtractWebosr(log LogFn) {
	zipPath := filepath.Join(lunarOfflineDir(), "ul-resources.zip")
	webDir := filepath.Join(lunarOfflineDir(), "natives", "web")
	marker := filepath.Join(webDir, ".extracted")
	if _, err := os.Stat(zipPath); err != nil {
		return
	}
	if _, err := os.Stat(marker); err == nil {
		return
	}
	os.MkdirAll(webDir, 0o755)
	log("  Extracting ul-resources -> natives/web")
	if err := extractZipTree(zipPath, webDir); err != nil {
		log("  WebOSR extract failed: " + err.Error())
		return
	}
	os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
}

// lunarDownloadTextures mirrors lunar.py _download_textures.
func lunarDownloadTextures(t *struct {
	IndexURL  string `json:"indexUrl"`
	BaseURL   string `json:"baseUrl"`
	IndexSHA1 string `json:"indexSha1"`
}, log LogFn, onProgress ProgressFn) error {
	if t == nil || t.IndexURL == "" || t.BaseURL == "" {
		return nil
	}
	dir := lunarTexturesDir()
	indexFile := filepath.Join(dir, "index.json")
	needIndex := true
	if fi, err := os.Stat(indexFile); err == nil && fi.Size() > 0 {
		if t.IndexSHA1 == "" {
			needIndex = false
		} else if h, err := sha1File(indexFile); err == nil && h == t.IndexSHA1 {
			needIndex = false
		}
	}
	if needIndex {
		log("  Downloading Lunar textures...")
		if err := downloadTo(t.IndexURL, indexFile, 0, nil); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return err
	}
	var failures []string
	tr := newTracker("lunar-textures", onProgress)
	tr.setTotal(int64(len(strings.Split(string(data), "\n"))), 0)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			tr.fileDone("")
			continue
		}
		path, hsh := fields[0], fields[1]
		objPath := filepath.Join(dir, filepath.FromSlash(path))
		if h, err := sha1File(objPath); err == nil && h == hsh {
			tr.fileDone(filepath.Base(objPath))
			continue
		}
		os.MkdirAll(filepath.Dir(objPath), 0o755)
		if err := downloadTo(strings.TrimSuffix(t.BaseURL, "/")+"/"+hsh, objPath, 0, tr); err != nil {
			failures = append(failures, path)
			tr.fileDone(filepath.Base(objPath))
			continue
		}
		if h, err := sha1File(objPath); err != nil || h != hsh {
			os.Remove(objPath)
			failures = append(failures, path)
		}
		tr.fileDone(filepath.Base(objPath))
	}
	if len(failures) > 0 {
		return fmt.Errorf("failed to download %d Lunar textures", len(failures))
	}
	return nil
}

// lunarDownloadUI mirrors lunar.py _download_ui.
func lunarDownloadUI(u *struct {
	SourceURL  string `json:"sourceUrl"`
	SourceSHA1 string `json:"sourceSha1"`
	Assets     struct {
		IndexURL  string `json:"indexUrl"`
		BaseURL   string `json:"baseUrl"`
		IndexSHA1 string `json:"indexSha1"`
	} `json:"assets"`
}, log LogFn, onProgress ProgressFn) error {
	if u == nil || u.SourceURL == "" || u.SourceSHA1 == "" {
		return nil
	}
	uiDir := filepath.Join(lunarUIDir(), u.SourceSHA1)
	marker := filepath.Join(uiDir, ".sha1")
	if data, err := os.ReadFile(marker); err != nil || strings.TrimSpace(string(data)) != u.SourceSHA1 {
		log("  Downloading UI...")
		tmpZip := filepath.Join(lunarUIDir(), u.SourceSHA1+".zip")
		if err := downloadTo(u.SourceURL, tmpZip, 0, nil); err != nil {
			return err
		}
		if h, err := sha1File(tmpZip); err != nil || h != u.SourceSHA1 {
			os.Remove(tmpZip)
			return fmt.Errorf("UI zip sha1 mismatch")
		}
		os.RemoveAll(uiDir)
		os.MkdirAll(uiDir, 0o755)
		if err := extractZipTree(tmpZip, uiDir); err != nil {
			os.Remove(tmpZip)
			return err
		}
		os.Remove(tmpZip)
		os.WriteFile(marker, []byte(u.SourceSHA1), 0o644)
	}
	if u.Assets.IndexURL == "" || u.Assets.BaseURL == "" {
		return nil
	}
	indexFile := filepath.Join(uiDir, "assetIndex")
	needIndex := true
	if fi, err := os.Stat(indexFile); err == nil && fi.Size() > 0 {
		if u.Assets.IndexSHA1 == "" {
			needIndex = false
		} else if h, err := sha1File(indexFile); err == nil && h == u.Assets.IndexSHA1 {
			needIndex = false
		}
	}
	if needIndex {
		if err := downloadTo(u.Assets.IndexURL, indexFile, 0, nil); err != nil {
			return err
		}
		if u.Assets.IndexSHA1 != "" {
			if h, err := sha1File(indexFile); err != nil || h != u.Assets.IndexSHA1 {
				os.Remove(indexFile)
				return fmt.Errorf("UI asset index sha1 mismatch")
			}
		}
	}
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return err
	}
	tr := newTracker("lunar-ui", onProgress)
	tr.setTotal(int64(len(strings.Split(string(data), "\n"))), 0)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			tr.fileDone("")
			continue
		}
		path, hsh := fields[0], fields[1]
		assetPath := filepath.Join(uiDir, "assets", filepath.FromSlash(path))
		if h, err := sha1File(assetPath); err == nil && h == hsh {
			tr.fileDone(filepath.Base(assetPath))
			continue
		}
		os.MkdirAll(filepath.Dir(assetPath), 0o755)
		if err := downloadTo(strings.TrimSuffix(u.Assets.BaseURL, "/")+"/"+hsh, assetPath, 0, tr); err != nil {
			return fmt.Errorf("UI asset download failed: %s", path)
		}
		if h, err := sha1File(assetPath); err != nil || h != hsh {
			os.Remove(assetPath)
			return fmt.Errorf("UI asset sha1 mismatch: %s", path)
		}
		tr.fileDone(filepath.Base(assetPath))
	}
	log("  UI dir: " + uiDir)
	return nil
}

// lunarLegacyModsDir is the pre-per-instance shared mods location:
// PROFILES_DIR/<ver>/mods/<module>-<ver>. Kept read-only as a one-time
// migration source: files found there are hardlinked into a fresh instance
// mods dir so nothing is re-downloaded after the move.
func lunarLegacyModsDir(version, module string) string {
	return filepath.Join(lunarProfilesDir(), version, "mods", module+"-"+version)
}

func emptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) == 0
}

// lunarInstallBaseModpack mirrors lunar.py _install_base_modpack (fabric/forge only).
func lunarInstallBaseModpack(instName, version, module string, bp *struct {
	MrpackURL string `json:"mrpackUrl"`
	Hash      string `json:"hash"`
}, log LogFn, onProgress ProgressFn) error {
	modsDir := LunarInstanceModsDir(instName, version, module)
	os.MkdirAll(modsDir, 0o755)
	if bp == nil || bp.MrpackURL == "" || (module != "fabric" && module != "forge") {
		return nil
	}
	if emptyDir(modsDir) {
		if entries, err := os.ReadDir(lunarLegacyModsDir(version, module)); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				dst := filepath.Join(modsDir, e.Name())
				if _, err := os.Stat(dst); err != nil {
					copyFile(filepath.Join(lunarLegacyModsDir(version, module), e.Name()), dst)
				}
			}
		}
	}
	profileDir := filepath.Join(lunarProfilesDir(), version)
	mrpack := filepath.Join(profileDir, "base-modpack.mrpack")
	need := true
	if fi, err := os.Stat(mrpack); err == nil && fi.Size() > 0 {
		if bp.Hash == "" {
			log("  Base modpack cache: no expected hash, keeping existing file")
			need = false
		} else if h, err := sha256File(mrpack); err == nil && strings.EqualFold(h, bp.Hash) {
			log("  Base modpack cache hit")
			need = false
		} else if err != nil {
			log(fmt.Sprintf("  Base modpack cache unreadable: %v", err))
		} else {
			log(fmt.Sprintf("  Base modpack cache stale (got %s, want %s)", h, bp.Hash))
		}
	} else {
		log("  Base modpack not cached")
	}
	if need {
		log("  Downloading official Lunar base modpack...")
		os.Remove(mrpack)
		if err := downloadTo(bp.MrpackURL, mrpack, 0, nil); err != nil {
			return err
		}
		if bp.Hash != "" {
			h, err := sha256File(mrpack)
			if err != nil {
				return err
			}
			if !strings.EqualFold(h, bp.Hash) {
				os.Remove(mrpack)
				return fmt.Errorf("base modpack sha256 mismatch (got %s, want %s)", h, bp.Hash)
			}
		}
	}
	zr, err := zip.OpenReader(mrpack)
	if err != nil {
		return err
	}
	defer zr.Close()
	var index struct {
		Dependencies map[string]string `json:"dependencies"`
		Files        []struct {
			Path      string   `json:"path"`
			Downloads []string `json:"downloads"`
			Hashes    struct {
				SHA1 string `json:"sha1"`
			} `json:"hashes"`
		} `json:"files"`
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = json.NewDecoder(rc).Decode(&index)
			rc.Close()
			if err != nil {
				return err
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("base modpack: modrinth.index.json not found")
	}
	packModule := ""
	if index.Dependencies["fabric-loader"] != "" || index.Dependencies["fabric"] != "" {
		packModule = "fabric"
	} else if index.Dependencies["forge"] != "" {
		packModule = "forge"
	}
	if packModule != module {
		log("  No official base modpack for module " + module)
		return nil
	}
	var installed []string
	var redownloaded int
	tr := newTracker("lunar-modpack", onProgress)
	tr.setTotal(int64(len(index.Files)), 0)
	for _, entry := range index.Files {
		path := filepath.ToSlash(entry.Path)
		parts := strings.Split(path, "/")
		if len(parts) == 0 || parts[0] != "mods" {
			tr.fileDone(filepath.Base(path))
			continue
		}
		escape := false
		for _, p := range parts {
			if p == ".." {
				escape = true
				break
			}
		}
		if escape {
			tr.fileDone(filepath.Base(path))
			continue
		}
		if len(entry.Downloads) == 0 || entry.Hashes.SHA1 == "" {
			return fmt.Errorf("invalid base modpack entry: %s", path)
		}
		dest := filepath.Join(modsDir, filepath.Join(parts[1:]...))
		os.MkdirAll(filepath.Dir(dest), 0o755)
		if h, err := sha1File(dest); err == nil && strings.EqualFold(h, entry.Hashes.SHA1) {
			installed = append(installed, filepath.Join(parts[1:]...))
			tr.fileDone(filepath.Base(dest))
			continue
		}
		redownloaded++
		log("    " + filepath.Base(dest))
		if err := downloadTo(entry.Downloads[0], dest, 0, tr); err != nil {
			return err
		}
		if h, err := sha1File(dest); err != nil || !strings.EqualFold(h, entry.Hashes.SHA1) {
			os.Remove(dest)
			return fmt.Errorf("sha1 mismatch for mod %s", filepath.Base(dest))
		}
		installed = append(installed, filepath.Join(parts[1:]...))
		tr.fileDone(filepath.Base(dest))
	}
	marker := filepath.Join(modsDir, ".base-modpack-files.json")
	var previous []string
	if data, err := os.ReadFile(marker); err == nil {
		_ = json.Unmarshal(data, &previous)
	}
	prevSet := make(map[string]bool, len(previous))
	for _, p := range previous {
		prevSet[p] = true
	}
	for _, p := range previous {
		if prevSet[p] && !slices.Contains(installed, p) {
			rel := filepath.FromSlash(p)
			if !filepath.IsAbs(rel) && !strings.Contains(rel, "..") {
				os.Remove(filepath.Join(modsDir, rel))
			}
		}
	}
	data, _ := json.MarshalIndent(installed, "", "  ")
	os.WriteFile(marker, data, 0o644)
	log(fmt.Sprintf("  Base modpack: %d mods in %s (%d re-downloaded)", len(installed), modsDir, redownloaded))
	return nil
}
