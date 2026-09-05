package updater

import (
	"os"
	"os/exec"
	"strings"
)

// Managed describes an installation owned by a system package manager rather
// than by the updater. Such builds cannot replace their own binary: the files
// are root-owned (AUR, .deb, .rpm), live on a read-only mount (Flatpak, Snap)
// or are tracked by another tool (Homebrew). The only correct update path is
// through that manager, so the updater must never attempt a self-install.
type Managed struct {
	// ID is the machine-readable manager: flatpak, snap, aur, brew, system
	// or programfiles. It is part of the frontend contract.
	ID string `json:"id"`
	// Name is the human-readable manager name shown in the UI.
	Name string `json:"name"`
	// Command is the exact shell command that updates the app, shown when the
	// manager cannot be driven automatically.
	Command string `json:"command"`
}

// managedError reports that a self-install was refused because the app is
// manager-owned. The message carries the update command so the UI can show it
// verbatim.
func managedError(m *Managed) *Error {
	return NewError(KindManaged, "installed via "+m.Name+"; update with: "+m.Command)
}

// RejectIfManaged returns a KindManaged error when the running app is owned by
// a package manager. Every Install entry point calls it first so a managed
// build can never be half-replaced by the updater.
func RejectIfManaged() error {
	if m := DetectManagedExe(); m != nil {
		return managedError(m)
	}
	return nil
}

// DetectManagedExe reports how the running app was installed, or nil for a
// portable (updater-owned) build.
func DetectManagedExe() *Managed {
	exe, err := currentExecutable()
	if err != nil {
		return nil
	}
	return DetectManaged(exe, os.Getenv, fileExists)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DetectManaged classifies an executable path. getenv and exists are
// parameters so tests can simulate sandboxes without root or env changes.
func DetectManaged(exe string, getenv func(string) string, exists func(string) bool) *Managed {
	if id := getenv("FLATPAK_ID"); id != "" {
		return &Managed{ID: "flatpak", Name: "Flatpak", Command: "flatpak update " + id}
	}
	if name := getenv("SNAP_INSTANCE_NAME"); name != "" {
		return &Managed{ID: "snap", Name: "Snap", Command: "snap refresh " + name}
	}
	if name := getenv("SNAP_NAME"); name != "" {
		return &Managed{ID: "snap", Name: "Snap", Command: "snap refresh " + name}
	}

	lower := strings.ToLower(exe)
	if strings.Contains(lower, "/caskroom/") || strings.Contains(lower, "/cellar/") {
		return &Managed{ID: "brew", Name: "Homebrew", Command: "brew upgrade --cask multilauncher"}
	}
	if strings.HasPrefix(lower, "/snap/") {
		return &Managed{ID: "snap", Name: "Snap", Command: "snap refresh multilauncher"}
	}
	if strings.HasPrefix(exe, "/usr/") || exe == "/usr" {
		if exists("/etc/arch-release") || exists("/etc/artix-release") {
			return &Managed{ID: "aur", Name: "AUR", Command: "yay -Syu multilauncher"}
		}
		return &Managed{ID: "system", Name: "system package manager", Command: "update via your system package manager"}
	}
	if strings.Contains(lower, "program files") {
		return &Managed{ID: "programfiles", Name: "system installer", Command: "reinstall from the latest release"}
	}
	return nil
}

// TryHostUpdate attempts a package-manager update from inside a sandbox where
// the manager can be driven on the host. It currently handles Flatpak via
// flatpak-spawn: the manifest must allow talking to org.freedesktop.Flatpak
// for this to work.
//
// It returns handled=false when no host update applies (caller falls back to
// the modal). When handled=true, the host manager took over (err is nil on
// success) and there is nothing staged and no modal to show.
func TryHostUpdate(m *Managed) (handled bool, err error) {
	if m == nil || m.ID != "flatpak" {
		return false, nil
	}
	if _, err := exec.LookPath("flatpak-spawn"); err != nil {
		return false, nil
	}
	parts := strings.Fields(m.Command)
	args := append([]string{"--host"}, parts...)
	cmd := exec.Command("flatpak-spawn", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return true, WrapError(KindInstall, "host package-manager update failed", &hostError{msg: strings.TrimSpace(string(out)), err: err})
	}
	return true, nil
}

// hostError carries host-command output alongside its cause.
type hostError struct {
	msg string
	err error
}

func (e *hostError) Error() string {
	if e.msg != "" {
		return e.msg + ": " + e.err.Error()
	}
	return e.err.Error()
}

func (e *hostError) Unwrap() error { return e.err }
