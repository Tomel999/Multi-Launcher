package launcher

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Screenshot struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

func screenshotsDir(instName string) string {
	p := filepath.Join(InstanceDir(instName), "screenshots")
	os.MkdirAll(p, 0o755)
	return p
}

func ListScreenshots(instName string) []Screenshot {
	dir := screenshotsDir(instName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Screenshot{}
	}
	out := []Screenshot{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Screenshot{
			Name:     e.Name(),
			Size:     info.Size(),
			Modified: info.ModTime().UnixMilli(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out
}

func GetScreenshot(instName, name string) (string, error) {
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid screenshot name")
	}
	data, err := os.ReadFile(filepath.Join(screenshotsDir(instName), name))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func DeleteScreenshot(instName, name string) error {
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid screenshot name")
	}
	return os.Remove(filepath.Join(screenshotsDir(instName), name))
}
