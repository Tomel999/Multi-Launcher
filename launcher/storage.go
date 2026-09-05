package launcher

import (
	"os"
	"path/filepath"
	"strings"
)

type StorageCategory struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func DiskUsage() []StorageCategory {
	root := root()
	cats := []struct{ name, rel string }{
		{"versions", "versions"},
		{"libraries", "libraries"},
		{"assets", "assets"},
		{"natives", "natives"},
		{"loaders", "loaders"},
		{"instances", "instances"},
		{"lunar", "clients/lunar"},
		{"feather", "clients/feather"},
		{"dawn", "clients/dawn"},
	}
	out := make([]StorageCategory, 0, len(cats)+1)
	for _, c := range cats {
		p := filepath.Join(root, filepath.FromSlash(c.rel))
		out = append(out, StorageCategory{Name: c.name, Path: p, Size: dirSize(p)})
	}
	out = append(out, StorageCategory{Name: "java", Path: javaDir(), Size: dirSize(javaDir())})
	return out
}

func InstanceSize(name string) int64 {
	return dirSize(InstanceDir(name))
}

type CleanReport struct {
	Files int64 `json:"files"`
	Freed int64 `json:"freed"`
}

func CleanCache() CleanReport {
	var rep CleanReport
	filepath.Walk(root(), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".part") {
			rep.Freed += info.Size()
			rep.Files++
			os.Remove(p)
		}
		return nil
	})
	loaders := filepath.Join(root(), "loaders")
	if entries, err := os.ReadDir(loaders); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
				continue
			}
			if fi, err := e.Info(); err == nil {
				rep.Freed += fi.Size()
			}
			rep.Files++
			os.Remove(filepath.Join(loaders, e.Name()))
		}
	}
	return rep
}
