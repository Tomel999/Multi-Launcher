//go:build windows

package launcher

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}

func windowlessJava(path string) string {
	if strings.EqualFold(filepath.Base(path), "java.exe") {
		return filepath.Join(filepath.Dir(path), "javaw.exe")
	}
	return path
}
