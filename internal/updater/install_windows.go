//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// scriptMaxWaitSeconds bounds how long the update script waits for this
// process to exit before giving up, so a hung launcher cannot leave the
// updater spinning forever.
const scriptMaxWaitSeconds = 120

// Install replaces the running .exe and relaunches it.
//
// Windows locks a running executable, so the file cannot be overwritten
// in place. Instead Install writes a small batch script to the temp directory
// and starts it detached. The script waits for this process to exit, moves the
// old binary aside, moves the staged download into place, relaunches the app,
// and finally deletes both the staged file and itself.
//
// Install returns as soon as the script is running; the caller must exit
// promptly so the script can proceed.
func Install(downloadedPath string) error {
	return installExe(downloadedPath, true)
}

// InstallWithoutRelaunch swaps in the downloaded update but does not restart
// the app. It is used by the shutdown hook: the user is quitting, so spawning
// a fresh instance would be wrong.
func InstallWithoutRelaunch(downloadedPath string) error {
	return installExe(downloadedPath, false)
}

func installExe(downloadedPath string, relaunch bool) error {
	if err := RejectIfManaged(); err != nil {
		return err
	}
	target, err := currentExecutable()
	if err != nil {
		return WrapError(KindInstall, "cannot locate the running executable", err)
	}
	source, err := filepath.Abs(downloadedPath)
	if err != nil {
		return WrapError(KindInstall, "cannot resolve the downloaded update", err)
	}
	if info, err := os.Stat(source); err != nil {
		return WrapError(KindInstall, "the downloaded update is missing", err)
	} else if info.Size() == 0 {
		return NewError(KindInstall, "the downloaded update is empty")
	}
	if strings.EqualFold(source, target) {
		return NewError(KindInstall, "the update is already at the installation path")
	}

	script := updateScript(target, source, relaunch)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("multilauncher-update-%d.bat", os.Getpid()))
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return WrapError(KindInstall, "cannot write the update script", err)
	}

	cmd := exec.Command("cmd.exe", "/C", path)
	cmd.Dir = filepath.Dir(target)
	// Hide the console window and detach from this process so the script
	// survives our exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(path)
		return WrapError(KindInstall, "cannot start the update script", err)
	}
	return nil
}

// relaunch is a no-op on Windows: the batch script owns the relaunch, because
// it has to happen after this process has exited.
func relaunch(string) error { return nil }

// updateScript builds the batch file that performs the swap.
//
// The wait loop does not poll the process table. It repeatedly tries to MOVE
// the running .exe, which Windows refuses while any process holds it open.
// That is both simpler and more reliable than matching a PID, and it also
// covers the case where the binary is held by something else (an antivirus
// scan, for example).
func updateScript(target, source string, relaunch bool) string {
	dir := filepath.Dir(target)
	backup := target + ".old"

	var b strings.Builder
	b.WriteString("@echo off\r\n")
	b.WriteString("setlocal\r\n")
	b.WriteString(fmt.Sprintf("set \"SRC=%s\"\r\n", source))
	b.WriteString(fmt.Sprintf("set \"DST=%s\"\r\n", target))
	b.WriteString(fmt.Sprintf("set \"OLD=%s\"\r\n", backup))
	b.WriteString(fmt.Sprintf("cd /D \"%s\"\r\n", dir))
	b.WriteString("set TRIES=0\r\n")
	b.WriteString("\r\n")
	b.WriteString(":wait\r\n")
	b.WriteString("set /A TRIES+=1\r\n")
	b.WriteString(fmt.Sprintf("if %%TRIES%% GTR %d goto fail\r\n", scriptMaxWaitSeconds))
	// Succeeds only once the running binary has been released.
	b.WriteString("2>nul move /Y \"%DST%\" \"%OLD%\"\r\n")
	b.WriteString("if not errorlevel 1 goto replaced\r\n")
	b.WriteString("timeout /t 1 /nobreak >nul 2>&1\r\n")
	b.WriteString("goto wait\r\n")
	b.WriteString("\r\n")
	b.WriteString(":replaced\r\n")
	b.WriteString("move /Y \"%SRC%\" \"%DST%\"\r\n")
	b.WriteString("if errorlevel 1 goto restore\r\n")
	b.WriteString("del /F /Q \"%OLD%\" >nul 2>&1\r\n")
	if relaunch {
		b.WriteString("start \"\" \"%DST%\"\r\n")
	}
	b.WriteString("del /F /Q \"%~f0\" >nul 2>&1\r\n")
	b.WriteString("endlocal\r\n")
	b.WriteString("exit /b 0\r\n")
	b.WriteString("\r\n")
	b.WriteString(":restore\r\n")
	// Put the previous binary back so the user is never left without an app.
	b.WriteString("move /Y \"%OLD%\" \"%DST%\" >nul 2>&1\r\n")
	b.WriteString("endlocal\r\n")
	b.WriteString("exit /b 1\r\n")
	b.WriteString("\r\n")
	b.WriteString(":fail\r\n")
	b.WriteString("endlocal\r\n")
	b.WriteString("exit /b 1\r\n")
	return b.String()
}
