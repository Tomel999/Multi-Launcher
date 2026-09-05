package plugin

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func InspectZip(zipPath string) (*Manifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open plugin archive: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "plugin.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		return ParseManifest(data)
	}
	return nil, fmt.Errorf("archive has no plugin.json at top level")
}

const maxExtractBytes = 256 << 20

func InstallFromZip(zipPath, destDir string) error {
	if _, err := InspectZip(zipPath); err != nil {
		return err
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open plugin archive: %w", err)
	}
	defer zr.Close()

	if len(zr.File) > maxArchiveEntries {
		return fmt.Errorf("archive has too many entries (%d, max %d)", len(zr.File), maxArchiveEntries)
	}

	var total int64
	for _, f := range zr.File {
		rel, err := safePluginPath(f.Name, f.FileInfo().Mode())
		if err != nil {
			return err
		}
		out := filepath.Join(destDir, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(out)
		if err != nil {
			rc.Close()
			return err
		}
		n, copyErr := io.Copy(w, io.LimitReader(rc, maxExtractBytes-total+1))
		rc.Close()
		w.Close()
		total += n
		if copyErr != nil {
			return copyErr
		}
		if total > maxExtractBytes {
			os.RemoveAll(destDir)
			return fmt.Errorf("archive too large (over %d bytes uncompressed)", maxExtractBytes)
		}
	}
	return nil
}

func safePluginPath(name string, mode os.FileMode) (string, error) {
	if mode&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink in archive: %s", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path in archive: %s", name)
	}
	return clean, nil
}
