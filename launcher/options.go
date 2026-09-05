package launcher

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Option struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func optionsPath(instName string) string {
	return filepath.Join(InstanceDir(instName), "options.txt")
}

func GetOptions(instName string) []Option {
	data, err := os.ReadFile(optionsPath(instName))
	if err != nil {
		return []Option{}
	}
	out := []Option{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out = append(out, Option{Key: key, Value: val})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func SaveOptions(instName string, opts []Option) error {
	var b strings.Builder
	for _, o := range opts {
		if strings.TrimSpace(o.Key) == "" {
			continue
		}
		b.WriteString(o.Key)
		b.WriteString(":")
		b.WriteString(o.Value)
		b.WriteString("\n")
	}
	return os.WriteFile(optionsPath(instName), []byte(b.String()), 0o644)
}
