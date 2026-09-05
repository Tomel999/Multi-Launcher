package panorama

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func fetchFromJar(version, jarURL string, urls *[6]string) error {
	tmpJar := filepath.Join(versionDir(version), version+".jar")
	if err := downloadFile(jarURL, tmpJar); err != nil {
		return err
	}
	defer os.Remove(tmpJar)

	r, err := zip.OpenReader(tmpJar)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		for i := 0; i < 6; i++ {
			if isFace(f.Name, i) {
				if err := extractTo(f, facePath(version, i)); err != nil {
					return err
				}
				urls[i] = faceURL(version, i)
			}
		}
		if strings.HasSuffix(f.Name, "gui/background.png") {
			if err := extractTo(f, facePath(version, 0)); err != nil {
				return err
			}
			urls[0] = faceURL(version, 0)
		}
	}
	return nil
}

func isFace(name string, i int) bool {
	return strings.HasSuffix(name, fmt.Sprintf("panorama_%d.png", i)) ||
		strings.HasSuffix(name, fmt.Sprintf("panorama%d.png", i))
}

func extractTo(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
