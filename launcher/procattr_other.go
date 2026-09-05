//go:build !windows

package launcher

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}

func windowlessJava(path string) string { return path }
