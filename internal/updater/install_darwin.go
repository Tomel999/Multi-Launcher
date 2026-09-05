//go:build darwin

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Install replaces the running .app bundle with the downloaded update and
// relaunches it.
//
// The downloaded asset must be a .zip containing exactly one *.app bundle.
// The bundle is extracted to a temporary directory, the current bundle is
// moved aside, the new one is copied into its place, and the app is relaunched
// with `open -n` (a fresh instance, so the user is not left without a window).
//
// Copies go through `ditto` when available, because it preserves the resource
// forks and extended attributes that a signed bundle depends on. Nothing is
// re-signed: the release pipeline is responsible for producing a signed
// bundle, and re-signing locally would invalidate that work.
//
// If the new bundle cannot be put in place, the previous one is restored.
func Install(downloadedPath string) error {
	if err := RejectIfManaged(); err != nil {
		return err
	}
	bundle, err := installBundle(downloadedPath)
	if err != nil {
		return err
	}

	if err := relaunch(bundle); err != nil {
		// The new version is installed; a failed relaunch is recoverable by
		// opening the app manually, so report it rather than rolling back.
		return WrapError(KindInstall, "the update was installed but could not be relaunched", err)
	}
	return nil
}

// InstallWithoutRelaunch swaps in the downloaded update but does not reopen
// the app. It is used by the shutdown hook: the user is quitting, so opening
// a fresh instance would be wrong.
func InstallWithoutRelaunch(downloadedPath string) error {
	if err := RejectIfManaged(); err != nil {
		return err
	}
	_, err := installBundle(downloadedPath)
	return err
}

// installBundle extracts the downloaded .zip, swaps the new .app bundle into
// place and cleans up. It returns the bundle path for the caller to relaunch.
func installBundle(downloadedPath string) (string, error) {
	exe, err := currentExecutable()
	if err != nil {
		return "", WrapError(KindInstall, "cannot locate the running executable", err)
	}
	bundle, err := bundleRoot(exe)
	if err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp("", "multilauncher-update-")
	if err != nil {
		return "", WrapError(KindInstall, "cannot create a temporary extraction directory", err)
	}
	defer os.RemoveAll(tmp)

	extractDir := filepath.Join(tmp, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", WrapError(KindInstall, "cannot create the extraction directory", err)
	}
	if err := unzip(downloadedPath, extractDir); err != nil {
		return "", WrapError(KindInstall, fmt.Sprintf("cannot unzip %s", filepath.Base(downloadedPath)), err)
	}
	newBundle, err := findAppBundle(extractDir)
	if err != nil {
		return "", err
	}

	backup := bundle + ".old"
	_ = os.RemoveAll(backup)

	// Move the running bundle aside. macOS does not lock a running binary the
	// way Windows does, so the rename succeeds immediately.
	if err := os.Rename(bundle, backup); err != nil {
		return "", WrapError(KindInstall, fmt.Sprintf("cannot replace %s (try moving the app out of a read-only location)", bundle), err)
	}

	if err := copyBundle(newBundle, bundle); err != nil {
		// Restore the previous bundle so the user is never left with nothing.
		if rerr := os.Rename(backup, bundle); rerr != nil {
			return "", WrapError(KindInstall,
				fmt.Sprintf("update failed (%v) AND restoring the previous version failed", err), rerr)
		}
		return "", WrapError(KindInstall, "cannot install the new application bundle", err)
	}

	// The update succeeded, so the staged download and the old bundle go away.
	_ = os.RemoveAll(backup)
	_ = os.Remove(downloadedPath)

	return bundle, nil
}

// copyBundle copies a .app bundle to dest, preferring ditto.
func copyBundle(src, dest string) error {
	if ditto, err := exec.LookPath("ditto"); err == nil {
		if out, err := exec.Command(ditto, src, dest).CombinedOutput(); err == nil {
			return nil
		} else if len(out) > 0 {
			// Fall through to the pure-Go copy, which is good enough for an
			// unsigned or ad-hoc-signed bundle.
			_ = out
		}
	}
	return copyTree(src, dest)
}

// relaunch opens a new instance of the (possibly replaced) bundle. `open -n`
// starts a second instance rather than activating a running one, which is what
// we want because the old instance is about to quit.
func relaunch(bundle string) error {
	return exec.Command("open", "-n", bundle).Start()
}
