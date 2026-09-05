package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// This file holds the platform-independent install helpers. The Install entry
// point itself is defined once per platform, selected by build tag:
//
//	install_windows.go  — stages a self-deleting batch script (a running .exe
//	                      cannot overwrite itself)
//	install_darwin.go   — replaces the .app bundle, preserving signing layout
//	install_linux.go    — atomic rename over the running binary
//	install_other.go    — returns an error on unsupported platforms
//
// Install returns once the replacement is staged and the relaunch has been
// spawned. On Windows the swap happens asynchronously after this process
// exits, so the caller must quit promptly after Install returns.

// currentExecutable returns the absolute path of the running binary with
// symlinks resolved (relevant on Linux, where the launcher is often symlinked
// into /usr/local/bin).
func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// Fall back to the unresolved path: it is still the file that must be
		// replaced, and on Windows/macOS EvalSymlinks can fail oddly.
		return exe, nil
	}
	return resolved, nil
}

// stagingPath returns a hidden sibling of target inside the same directory.
// Keeping the staging file on the same filesystem is what makes the final
// rename atomic, and it is required on Linux to replace a running binary.
func stagingPath(target string) string {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	return filepath.Join(dir, "."+base+".new")
}

// safeJoin resolves name inside root and rejects paths that escape it. This
// guards against "zip slip" / malicious archive entries such as
// "../../etc/passwd".
func safeJoin(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(os.PathSeparator))
	}
	target := filepath.Join(root, clean)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != rootAbs && !strings.HasPrefix(targetAbs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
	}
	return target, nil
}

// unzip extracts a .zip archive to dest, preserving directory structure,
// symlinks and executable bits. Symlink preservation matters on macOS, where
// .app bundles are full of them.
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		path, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}
		info := f.FileInfo()
		switch {
		case info.IsDir():
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		case info.Mode()&os.ModeSymlink != 0:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			linkTarget, err := io.ReadAll(io.LimitReader(rc, 64<<10))
			rc.Close()
			if err != nil {
				return err
			}
			_ = os.Remove(path)
			if err := os.Symlink(string(linkTarget), path); err != nil {
				return err
			}
			continue
		default:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := extractZipFile(f, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractZipFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Preserve the executable bit recorded in the archive.
	if mode := f.FileInfo().Mode(); mode&0o111 != 0 {
		return os.Chmod(dest, 0o755)
	}
	return nil
}

// extractTarGz extracts a .tar.gz archive to dest, preserving symlinks and
// executable bits.
func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(hdr.Mode)|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
				os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
			if err := os.Chmod(path, os.FileMode(hdr.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			_ = os.Remove(path)
			if err := os.Symlink(hdr.Linkname, path); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			_ = os.Remove(path)
			if err := os.Link(hdr.Linkname, path); err != nil {
				return err
			}
		default:
			// Char devices, FIFOs and pax headers are not needed for a
			// launcher bundle; skip them rather than failing the update.
			continue
		}
	}
}

// findAppBundle locates a single *.app bundle inside root, tolerating an extra
// wrapping directory in the archive.
func findAppBundle(root string) (string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasSuffix(path, ".app") {
			found = append(found, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", NewError(KindInstall,
			"the downloaded macOS update does not contain a .app bundle")
	}
	if len(found) > 1 {
		return "", NewError(KindInstall,
			fmt.Sprintf("the downloaded macOS update contains %d .app bundles, expected 1", len(found)))
	}
	return found[0], nil
}

// bundleRoot walks up from an executable inside a .app bundle to the bundle
// directory itself, e.g.
//
//	/Applications/Multi Launcher.app/Contents/MacOS/Multi Launcher
//	  -> /Applications/Multi Launcher.app
func bundleRoot(exe string) (string, error) {
	dir := filepath.Dir(filepath.Clean(exe))
	for i := 0; i < 8; i++ {
		if strings.HasSuffix(dir, ".app") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", NewError(KindInstall, fmt.Sprintf(
		"cannot locate the .app bundle containing %s; the launcher appears not to run from a bundle", exe))
}

// findBinary locates the launcher executable inside an extracted archive.
// It prefers an entry matching wantName, then any regular executable file,
// then any regular file.
func findBinary(root, wantName string) (string, error) {
	var fallback, executable string
	var matches []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		base := filepath.Base(path)
		if base == wantName {
			matches = append(matches, path)
			return nil
		}
		if executable == "" && info.Mode()&0o111 != 0 {
			executable = path
			return nil
		}
		if fallback == "" {
			fallback = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return "", NewError(KindInstall, fmt.Sprintf(
			"the downloaded update contains %d files named %q, expected 1", len(matches), wantName))
	case executable != "":
		return executable, nil
	case fallback != "":
		return fallback, nil
	default:
		return "", NewError(KindInstall, "the downloaded update contains no files")
	}
}

// copyFile copies src to dst and applies mode to the result.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// copyTree recursively copies a directory tree, preserving file modes and
// symlinks. Used as the fallback when platform-native copy tools are missing.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		default:
			info, err := d.Info()
			if err != nil {
				return err
			}
			return copyFile(path, target, info.Mode()&0o777)
		}
	})
}
