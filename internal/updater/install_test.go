package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "out")
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"simple", "a.txt", filepath.Join(root, "a.txt"), false},
		{"nested", "a/b/c.txt", filepath.Join(root, "a", "b", "c.txt"), false},
		{"dot slash", "./a.txt", filepath.Join(root, "a.txt"), false},
		{"parent escape", "../evil", "", true},
		{"nested escape", "a/../../evil", "", true},
		{"absolute unix", "/etc/passwd", filepath.Join(root, "etc", "passwd"), false},
		{"absolute windows", "C:\\evil", filepath.Join(root, "C:\\evil"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := safeJoin(root, c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("safeJoin(%q) = %q, want error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeJoin(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("safeJoin(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// writeZip builds a zip containing a regular file, a directory, a symlink and
// an executable file.
func writeZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer f.Close()
	w := zip.NewWriter(f)

	stored := func(name, content string, mode os.FileMode) {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(mode)
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatalf("CreateHeader(%s): %v", name, err)
		}
		_, _ = fw.Write([]byte(content))
	}

	h := &zip.FileHeader{Name: "Payload/"}
	h.SetMode(os.ModeDir | 0o755)
	if _, err := w.CreateHeader(h); err != nil {
		t.Fatalf("CreateHeader dir: %v", err)
	}
	stored("Payload/Readme.md", "hello", 0o644)
	stored("Payload/run.sh", "#!/bin/sh\n", 0o755)

	// A symlink entry: the file content is the link target.
	h = &zip.FileHeader{Name: "Payload/link"}
	h.SetMode(os.ModeSymlink | 0o777)
	fw, err := w.CreateHeader(h)
	if err != nil {
		t.Fatalf("CreateHeader symlink: %v", err)
	}
	_, _ = fw.Write([]byte("run.sh"))

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestUnzip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "u.zip")
	writeZip(t, src)
	dest := filepath.Join(t.TempDir(), "out")

	if err := unzip(src, dest); err != nil {
		t.Fatalf("unzip: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dest, "Payload", "Readme.md")); err != nil || string(got) != "hello" {
		t.Errorf("Readme.md = %q, err=%v", got, err)
	}
	// Directory entries must be recreated.
	if fi, err := os.Stat(filepath.Join(dest, "Payload")); err != nil || !fi.IsDir() {
		t.Errorf("Payload dir missing: %v", err)
	}
	// The executable bit must survive.
	fi, err := os.Stat(filepath.Join(dest, "Payload", "run.sh"))
	if err != nil {
		t.Fatalf("Stat run.sh: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
		t.Errorf("run.sh mode = %v, want executable", fi.Mode())
	}
	// Symlinks must be recreated as symlinks rather than copied.
	if runtime.GOOS != "windows" {
		link := filepath.Join(dest, "Payload", "link")
		li, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("Lstat link: %v", err)
		}
		if li.Mode()&os.ModeSymlink == 0 {
			t.Errorf("link should be a symlink, mode = %v", li.Mode())
		}
		if target, err := os.Readlink(link); err != nil || target != "run.sh" {
			t.Errorf("link target = %q, err=%v", target, err)
		}
	}
}

func TestUnzipRejectsMissingFile(t *testing.T) {
	if err := unzip(filepath.Join(t.TempDir(), "nope.zip"), t.TempDir()); err == nil {
		t.Error("expected an error for a missing archive")
	}
}

// writeTarGz builds a .tar.gz with a nested directory, a regular file and an
// executable file.
func writeTarGz(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}

	add(&tar.Header{Name: "Multi-Launcher/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "Multi-Launcher/README", Typeflag: tar.TypeReg, Mode: 0o644}, "docs")
	add(&tar.Header{Name: "Multi-Launcher/Multi Launcher", Typeflag: tar.TypeReg, Mode: 0o755}, "ELF-binary")

	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz.Close: %v", err)
	}
}

func TestExtractTarGz(t *testing.T) {
	src := filepath.Join(t.TempDir(), "u.tar.gz")
	writeTarGz(t, src)
	dest := filepath.Join(t.TempDir(), "out")

	if err := extractTarGz(src, dest); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	bin := filepath.Join(dest, "Multi-Launcher", "Multi Launcher")
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "ELF-binary" {
		t.Errorf("binary content = %q", got)
	}
	fi, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
		t.Errorf("extracted binary mode = %v, want executable", fi.Mode())
	}
}

func TestFindAppBundle(t *testing.T) {
	root := t.TempDir()
	if _, err := findAppBundle(root); !IsKind(err, KindInstall) {
		t.Errorf("empty dir: kind = %v, want install", errKind(err))
	}

	// A single bundle, wrapped in an extra directory as archives often are.
	app := filepath.Join(root, "payload", "Multi Launcher.app", "Contents", "MacOS")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := findAppBundle(root)
	if err != nil {
		t.Fatalf("findAppBundle: %v", err)
	}
	if filepath.Base(got) != "Multi Launcher.app" {
		t.Errorf("found %q", got)
	}

	// Two bundles is ambiguous and must be rejected.
	if err := os.MkdirAll(filepath.Join(root, "other", "Second.app"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := findAppBundle(root); !IsKind(err, KindInstall) {
		t.Errorf("two bundles: kind = %v, want install", errKind(err))
	}
}

func TestBundleRoot(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "Applications", "Multi Launcher.app")
	exe := filepath.Join(base, "Contents", "MacOS", "Multi Launcher")
	got, err := bundleRoot(exe)
	if err != nil {
		t.Fatalf("bundleRoot: %v", err)
	}
	if got != base {
		t.Errorf("bundleRoot = %q, want %q", got, base)
	}

	if _, err := bundleRoot(filepath.Join(string(filepath.Separator), "usr", "bin", "launcher")); !IsKind(err, KindInstall) {
		t.Errorf("non-bundle path: kind = %v, want install", errKind(err))
	}
}

func TestFindBinary(t *testing.T) {
	root := t.TempDir()
	if _, err := findBinary(root, "x"); !IsKind(err, KindInstall) {
		t.Errorf("empty dir: kind = %v, want install", errKind(err))
	}

	// Match by name wins over the executable fallback.
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	readme := filepath.Join(root, "bin", "README")
	if err := os.WriteFile(readme, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	other := filepath.Join(root, "bin", "helper")
	if err := os.WriteFile(other, []byte("x"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, err := findBinary(root, "README"); err != nil || filepath.Base(got) != "README" {
		t.Errorf("findBinary by name = %q, err=%v", got, err)
	}
	// With no name match, the executable file is preferred — but Windows has
	// no exec bit, so there it degrades to "the first regular file found".
	got, err := findBinary(root, "nope")
	if err != nil {
		t.Fatalf("findBinary: %v", err)
	}
	wantBase := "helper"
	if runtime.GOOS == "windows" {
		wantBase = "README"
	}
	if filepath.Base(got) != wantBase {
		t.Errorf("findBinary fallback = %q, want %q", got, wantBase)
	}

	// A second file with the wanted name is ambiguous.
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := findBinary(root, "README"); !IsKind(err, KindInstall) {
		t.Errorf("ambiguous: kind = %v, want install", errKind(err))
	}
}

func TestCopyFileAndTree(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	for name, want := range map[string]string{"a.txt": "A", filepath.Join("sub", "b.txt"): "B"} {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	// copyFile creates missing parent directories.
	target := filepath.Join(t.TempDir(), "x", "y", "z.bin")
	if err := copyFile(filepath.Join(src, "a.txt"), target, 0o600); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "A" {
		t.Errorf("copyFile content = %q", got)
	}

	if err := copyFile(filepath.Join(src, "missing"), target, 0o600); err == nil {
		t.Error("copyFile of a missing source should fail")
	}
}

func TestStagingPath(t *testing.T) {
	got := stagingPath(filepath.Join("/opt", "launcher", "Multi Launcher"))
	want := filepath.Join("/opt", "launcher", ".Multi Launcher.new")
	if got != want {
		t.Errorf("stagingPath = %q, want %q", got, want)
	}
	// The staging file must share the target's directory so the rename is
	// atomic and does not cross filesystems.
	if filepath.Dir(got) != filepath.Dir(filepath.Join("/opt", "launcher", "Multi Launcher")) {
		t.Error("staging path must live in the same directory as the target")
	}
}

func TestCurrentExecutable(t *testing.T) {
	got, err := currentExecutable()
	if err != nil {
		t.Fatalf("currentExecutable: %v", err)
	}
	if got == "" {
		t.Fatal("currentExecutable returned an empty path")
	}
	if !strings.Contains(got, string(filepath.Separator)) {
		t.Errorf("currentExecutable = %q, want an absolute path", got)
	}
}
