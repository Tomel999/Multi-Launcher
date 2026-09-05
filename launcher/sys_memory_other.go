//go:build !windows

package launcher

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func SystemMemoryGB() int {
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "MemTotal:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil && kb > 0 {
							gb := int(kb / 1024 / 1024)
							if gb < 1 {
								gb = 1
							}
							return gb
						}
					}
				}
			}
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
			if bytes, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil && bytes > 0 {
				gb := int(bytes / 1024 / 1024 / 1024)
				if gb < 1 {
					gb = 1
				}
				return gb
			}
		}
	}
	return 16
}