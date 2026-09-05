//go:build linux

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Install replaces the running binary with the downloaded update and
// relaunches it.
//
// The asset is either a .tar.gz containing the launcher binary (preferred) or
// the binary/AppImage itself. Whatever the form, the new file is staged in the
// same directory as the running executable and then renamed over it. On Linux
// a rename over a running executable is allowed and atomic, so no helper
// script is needed.
func Install(downloadedPath string) error {
	if err := RejectIfManaged(); err != nil {
		return err
	}
	exe, err := installBinary(downloadedPath)
	if err != nil {
		return err
	}

	if err := relaunch(exe); err != nil {
		return WrapError(KindInstall, "the update was installed but could not be relaunched", err)
	}
	return nil
}

// InstallWithoutRelaunch swaps in the downloaded update but does not restart
// the app. It is used by the shutdown hook: the user is quitting, so spawning
// a fresh instance would be wrong.
func InstallWithoutRelaunch(downloadedPath string) error {
	if err := RejectIfManaged(); err != nil {
		return err
	}
	_, err := installBinary(downloadedPath)
	return err
}

// installBinary extracts the downloaded update (when archived), stages it
// next to the running executable and renames it over. It returns the
// executable path for the caller to relaunch.
func installBinary(downloadedPath string) (string, error) {
	exe, err := currentExecutable()
	if err != nil {
		return "", WrapError(KindInstall, "cannot locate the running executable", err)
	}

	newBinary := downloadedPath
	tmp := ""
	switch {
	case strings.HasSuffix(downloadedPath, ".tar.gz"), strings.HasSuffix(downloadedPath, ".tgz"):
		tmp, err = os.MkdirTemp("", "multilauncher-update-")
		if err != nil {
			return "", WrapError(KindInstall, "cannot create a temporary extraction directory", err)
		}
		defer os.RemoveAll(tmp)

		extractDir := filepath.Join(tmp, "extract")
		if err := os.MkdirAll(extractDir, 0o755); err != nil {
			return "", WrapError(KindInstall, "cannot create the extraction directory", err)
		}
		if err := extractTarGz(downloadedPath, extractDir); err != nil {
			return "", WrapError(KindInstall, fmt.Sprintf("cannot extract %s", filepath.Base(downloadedPath)), err)
		}
		newBinary, err = findBinary(extractDir, filepath.Base(exe))
		if err != nil {
			return "", err
		}
	}

	info, err := os.Stat(newBinary)
	if err != nil {
		return "", WrapError(KindInstall, "the downloaded update is missing", err)
	}
	if info.IsDir() {
		return "", NewError(KindInstall, "the downloaded update is a directory, not an executable")
	}

	// Stage next to the target so the final rename is atomic and stays on one
	// filesystem.
	staged := stagingPath(exe)
	if abs, err := filepath.Abs(newBinary); err == nil && abs != staged {
		if err := copyFile(newBinary, staged, 0o755); err != nil {
			_ = os.Remove(staged)
			return "", WrapError(KindInstall, "cannot stage the update next to the running binary", err)
		}
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		_ = os.Remove(staged)
		return "", WrapError(KindInstall, "cannot make the update executable", err)
	}

	// Atomic on POSIX: the running process keeps its old inode until it exits,
	// and any new launch picks up the replacement.
	if err := os.Rename(staged, exe); err != nil {
		_ = os.Remove(staged)
		return "", WrapError(KindInstall, fmt.Sprintf(
			"cannot replace %s (the install directory may be read-only)", exe), err)
	}

	return exe, nil
}

// relaunch starts a fresh instance of the updated binary with the same
// arguments, detached from this process so it survives our exit.
func relaunch(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = filepath.Dir(exe)
	cmd.Env = os.Environ()
	// Detach into a new session so the child is not killed when this process
	// group goes away.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
