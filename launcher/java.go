package launcher

import (
	"archive/zip"
	"hash/crc32"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type temurinAsset struct {
	Binary struct {
		Package struct {
			Link string `json:"link"`
			Size int64  `json:"size"`
		} `json:"package"`
	} `json:"binary"`
}

// javaFetch is the JSON fetcher used by EnsureJava. Tests override it so JDK
// resolution logic can be exercised without any network access.
var javaFetch = fetchJSON

func javaDir() string {
	if javaDirOverride != "" {
		return javaDirOverride
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, ".multilauncher", "java")
	os.MkdirAll(p, 0o755)
	if entries, err := os.ReadDir(p); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasSuffix(e.Name(), ".tmp") {
				os.RemoveAll(filepath.Join(p, e.Name()))
			}
		}
	}
	return p
}

// javaDirOverride lets tests point javaDir() at a temp directory so the JDK
// resolution logic can be exercised without touching the real user profile.
// SetRootForTests-style: assign, run, restore.
var javaDirOverride string

// SetJavaDirForTests redirects javaDir() to dir for the duration of the test.
// Returns a restore function the caller must defer. Avoids mutating shared
// state across parallel subtests.
func SetJavaDirForTests(dir string) func() {
	orig := javaDirOverride
	javaDirOverride = dir
	return func() { javaDirOverride = orig }
}

func javaMajorDir(major int) string {
	return filepath.Join(javaDir(), fmt.Sprintf("jdk%d", major))
}

func installedJava(major int) string {
	for _, name := range javaBinaryNames() {
		p := filepath.Join(javaMajorDir(major), "bin", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func javaBinaryNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"javaw.exe", "java.exe", "java"}
	}
	return []string{"java", "javaw.exe", "java.exe"}
}

// newestInstalledJavaAtLeast returns the highest installed bundled JDK whose
// major is >= major, or "". Modern MC runs fine on a newer JVM, so an existing
// jdk25 satisfies a request for 21 and spares the download. Legacy majors
// (<17) must match exactly — old Minecraft breaks on new JVMs.
func newestInstalledJavaAtLeast(javaRoot string, major int) string {
	entries, err := os.ReadDir(javaRoot)
	if err != nil {
		return ""
	}
	best, bestMajor := "", 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var m int
		if _, err := fmt.Sscanf(e.Name(), "jdk%d", &m); err != nil || m < major || m <= bestMajor {
			continue
		}
		for _, exe := range javaBinaryNames() {
			p := filepath.Join(javaRoot, e.Name(), "bin", exe)
			if _, err := os.Stat(p); err == nil {
				best, bestMajor = p, m
				break
			}
		}
	}
	return best
}

// RequiredJavaMajor returns the Java major demanded by the version's Mojang
// manifest (javaVersion.majorVersion). The manifest is REQUIRED: without it
// the version cannot be launched — we never guess Java from the version id,
// because a future "26.2" does not mean Java 26 and wrong guesses produce
// broken launches that are hard to diagnose.
func RequiredJavaMajor(v *VersionMeta) (int, error) {
	if v.JavaVersion.MajorVersion > 0 {
		return v.JavaVersion.MajorVersion, nil
	}
	return 0, fmt.Errorf(
		"manifest for %q has no javaVersion.majorVersion — cannot determine required Java; "+
			"delete versions/%s/%s.json so it is re-downloaded, then try again",
		v.ID, v.ID, v.ID)
}

// requiredJavaMajor resolves the manifest-mandated Java major for a version.
// Sources, in order: the per-version JSON (javaVersion.majorVersion), then
// version_manifest_v2's embedded javaVersion for that id. Both are Mojang
// manifest data — we never guess from the version id. Fails when neither is
// available: the version then simply cannot be launched.
func requiredJavaMajor(version string) (int, error) {
	if vm, err := GetVersionMeta(version); err == nil {
		return RequiredJavaMajor(vm)
	}
	// Per-version JSON unavailable (offline, transient error) — fall back to
	// the embedded javaVersion in the v2 manifest, if it is already available.
	if m, merr := GetManifest(); merr == nil {
		for _, e := range m.Versions {
			if e.ID == version && e.JavaVersion.MajorVersion > 0 {
				return e.JavaVersion.MajorVersion, nil
			}
		}
	}
	return 0, fmt.Errorf("cannot resolve required Java for %s: Mojang manifest has no javaVersion for this version", version)
}

func adoptiumArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x64"
}

func adoptiumOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "linux":
		return "linux"
	default:
		return "windows"
	}
}

func EnsureJava(major int, onProgress ProgressFn) (string, error) {
	if p := installedJava(major); p != "" {
		return p, nil
	}
	root := javaDir()
	if major >= 17 {
		if p := newestInstalledJavaAtLeast(root, major); p != "" {
			return p, nil
		}
	}
	url := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?os=%s&architecture=%s&image_type=jdk&vendor=eclipse", major, adoptiumOS(), adoptiumArch())
	var assets []temurinAsset
	if err := javaFetch(url, &assets); err != nil {
		return "", fmt.Errorf("temurin: %v", err)
	}
	if len(assets) == 0 {
		return "", fmt.Errorf("temurin: no assets in API response for java %d", major)
	}
	zipURL := assets[0].Binary.Package.Link
	zipSize := assets[0].Binary.Package.Size
	dest := filepath.Join(root, fmt.Sprintf("temurin%d.zip", major))

	tr := newTracker("java"+fmt.Sprint(major), onProgress)
	tr.setTotal(1, zipSize)
	if err := downloadTo(zipURL, dest, zipSize, tr); err != nil {
		return "", err
	}
	tmp := filepath.Join(root, fmt.Sprintf("jdk%d.tmp", major))
	os.RemoveAll(tmp)
	os.MkdirAll(tmp, 0o755)
	if err := extractZipTree(dest, tmp); err != nil {
		os.RemoveAll(tmp)
		if strings.Contains(err.Error(), "zip:") || strings.Contains(err.Error(), "not a valid") {
			os.Remove(dest)
			return "", fmt.Errorf("unpack java: %v (corrupted zip removed, will re-download)", err)
		}
		return "", fmt.Errorf("unpack java: %v (zip kept at %s for retry)", err, dest)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("unpack java: cannot read tmp: %v — zip kept at %s", err, dest)
	}
	if len(entries) == 0 {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("unpack java: empty archive — zip kept at %s", dest)
	}
	var src string
	if len(entries) == 1 && entries[0].IsDir() {
		src = filepath.Join(tmp, entries[0].Name())
	} else {
		src = tmp
	}
	target := filepath.Join(root, fmt.Sprintf("jdk%d", major))
	if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("unpack java: cannot clean %s: %v — close the running game and try again (zip kept at %s)", target, err, dest)
	}
	renameErr := error(nil)
	if src == tmp {
		renameErr = os.Rename(tmp, target)
	} else {
		renameErr = os.Rename(src, target)
		if renameErr == nil {
			os.RemoveAll(tmp)
		}
	}
	if renameErr != nil {
		if err := copyDirRecursive(src, target); err != nil {
			os.RemoveAll(tmp)
			os.RemoveAll(target)
			return "", fmt.Errorf("unpack java: rename %s → %s: %v; copy also failed: %v — zip kept at %s", src, target, renameErr, err, dest)
		}
		os.RemoveAll(tmp)
		if src != tmp {
			os.RemoveAll(src)
		}
	}
	os.Remove(dest)
	if runtime.GOOS != "windows" {
		for _, name := range []string{"java", "javaw"} {
			p := filepath.Join(target, "bin", name)
			os.Chmod(p, 0o755)
		}
	}
	p := installedJava(major)
	if p == "" {
		return "", fmt.Errorf("unpack java: bin/java not found for jdk%d", major)
	}
	return p, nil
}

func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func extractZipTree(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(os.PathSeparator)) || name == ".." {
			continue
		}
		out := filepath.Join(dest, name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(out, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(out), 0o755)
		if zipEntryAlreadyExtracted(out, f) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(out)
		if err != nil {
			rc.Close()
			return fmt.Errorf("cannot write %s: %w — close the running game and try again", name, err)
		}
		if _, err := io.Copy(w, rc); err != nil {
			rc.Close()
			w.Close()
			return err
		}
		rc.Close()
		w.Close()
	}
	return nil
}

// zipEntryAlreadyExtracted reports whether path already holds exactly the
// entry's bytes (same size, same CRC32). Re-extracting an unpacked tree then
// becomes a no-op, even when old files are still memory-mapped by a running
// game process.
func zipEntryAlreadyExtracted(path string, f *zip.File) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() != int64(f.UncompressedSize64) {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, file); err != nil {
		return false
	}
	return h.Sum32() == f.CRC32
}

func TestJava(path string) (string, error) {
	cmd := exec.Command(path, "-version")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ensureClientJava resolves the JDK used by Minecraft client launchers
// (Lunar, Feather, Dawn). The launcher keeps its downloaded JDKs in
// javaDir() — the same place vanilla MC looks. Clients must reuse that
// bundled JDK instead of triggering a fresh Adoptium download on every
// start. Resolution order:
//
//  1. Exact major match in javaDir() (no I/O beyond stat).
//  2. Any newer JDK already in javaDir() — modern MC/Forge/Fabric run fine
//     on a newer JVM, so an existing jdk25 satisfies a request for 21.
//  3. If nothing is installed yet (first run), download the requested major
//     once via EnsureJava.
//  4. If the download fails (no network, blocked host), fall back to any
//     JDK >= 17 in javaDir() — Feather/Dawn/Lunar all require 17+ anyway.
func ensureClientJava(major int, onProgress ProgressFn) (string, error) {
	root := javaDir()
	if p := installedJava(major); p != "" {
		return p, nil
	}
	if major >= 17 {
		if p := newestInstalledJavaAtLeast(root, major); p != "" {
			return p, nil
		}
	}
	if p, err := EnsureJava(major, onProgress); err == nil {
		return p, nil
	}
	// Last-ditch: any installed JDK >= 17. Better than failing outright —
	// but only for modern requests. Legacy majors (< 17) must match exactly:
	// old Minecraft breaks on new JVMs, so falling back would produce a
	// silently broken launch rather than a clear error.
	if major >= 17 {
		if p := newestInstalledJavaAtLeast(root, 17); p != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("bundled java %d not found and download failed — open the launcher once with network access so it can fetch a JDK", major)
}

// ensureBundledJava resolves the manifest-mandated JDK for the vanilla code
// path. The manifest is required — no version-id guessing.
func ensureBundledJava(version string, onProgress ProgressFn) (string, error) {
	major, err := requiredJavaMajor(version)
	if err != nil {
		return "", err
	}
	return EnsureJava(major, onProgress)
}

// parseJavaRuntime extracts the Java major version from a client-manifest
// runtime string. Feather/Dawn manifests use vendor-prefixed strings such as
// "zulu-25", "jdk-21.0.5+11", "java-21-openjdk", "temurin-21", "21". Returns
// 0 if the value is empty or no number >= 8 can be parsed — callers should
// fall back to a sane default in that case.
func parseJavaRuntime(runtime string) int {
	runtime = strings.TrimSpace(runtime)
	if runtime == "" {
		return 0
	}
	// Strip vendor prefixes: "zulu-25" -> "25", "jdk-21.0.5+11" -> "21.0.5+11"
	for _, p := range []string{"jdk-", "jre-", "java-", "temurin-", "zulu-", "openjdk-", "liberica-", "oracle-", "graalvm-", "corretto-"} {
		if strings.HasPrefix(strings.ToLower(runtime), p) {
			runtime = runtime[len(p):]
			break
		}
	}
	// If still no leading digit, give up.
	if runtime == "" || (runtime[0] < '0' || runtime[0] > '9') {
		return 0
	}
	// Take the run of digits at the start.
	end := 0
	for end < len(runtime) && runtime[end] >= '0' && runtime[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(runtime[:end])
	if err != nil || n < 8 {
		return 0
	}
	return n
}

// ensureJavaForRuntime picks the bundled JDK using the Runtime field of a
// client manifest (Feather/Dawn). It only RESOLVES an already-installed JDK
// in javaDir() — it does NOT trigger a fresh Adoptium download. If nothing
// matches, the caller falls back to ensureClientJava which may then fetch
// a JDK once. Splitting parse from resolve keeps the launcher's bundled-JDK
// invariant: clients reuse whatever JDK is already on disk.
func ensureJavaForRuntime(runtime string, onProgress ProgressFn) (string, error) {
	major := parseJavaRuntime(runtime)
	if major == 0 {
		major = 21
	}
	return ensureClientJava(major, onProgress)
}
