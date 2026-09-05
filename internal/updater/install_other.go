//go:build !windows && !darwin && !linux

package updater

import (
	"fmt"
	"runtime"
)

// Install is unsupported outside Windows, macOS and Linux.
//
// Keeping a real definition (instead of leaving Install undefined) means the
// package still compiles on any platform, and Go callers get a clear runtime
// error rather than a build failure.
func Install(downloadedPath string) error {
	return NewError(KindInstall, fmt.Sprintf(
		"automatic updates are not supported on %s/%s", runtime.GOOS, runtime.GOARCH))
}

func relaunch(string) error {
	return NewError(KindInstall, "relaunch is not supported on this platform")
}
