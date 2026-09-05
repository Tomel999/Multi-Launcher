package updater

import (
	"os/exec"
	"strings"
	"testing"
)

// envOf builds a getenv stub from a map.
func envOf(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func noFiles(string) bool { return false }

func TestDetectManagedPortable(t *testing.T) {
	cases := []string{
		"/home/user/Multi Launcher/Multi Launcher",
		"/home/user/.local/bin/multilauncher",
		"/tmp/test-binary",
		`C:\Users\user\MultiLauncher\Multi Launcher.exe`,
		"/Applications/Multi Launcher.app/Contents/MacOS/Multi Launcher",
		"/opt/multilauncher/multilauncher",
	}
	for _, exe := range cases {
		if m := DetectManaged(exe, envOf(nil), noFiles); m != nil {
			t.Errorf("DetectManaged(%q) = %+v, want nil (portable)", exe, m)
		}
	}
}

func TestDetectManagedFlatpak(t *testing.T) {
	m := DetectManaged("/app/bin/multilauncher",
		envOf(map[string]string{"FLATPAK_ID": "io.github.Tomel999.MultiLauncher"}), noFiles)
	if m == nil {
		t.Fatal("expected a flatpak detection")
	}
	if m.ID != "flatpak" {
		t.Errorf("ID = %q, want flatpak", m.ID)
	}
	if !strings.Contains(m.Command, "io.github.Tomel999.MultiLauncher") {
		t.Errorf("Command = %q, should carry the app ID from FLATPAK_ID", m.Command)
	}
}

func TestDetectManagedSnap(t *testing.T) {
	m := DetectManaged("/snap/bin/multilauncher",
		envOf(map[string]string{"SNAP_NAME": "multilauncher", "SNAP_INSTANCE_NAME": "multilauncher"}), noFiles)
	if m == nil || m.ID != "snap" {
		t.Fatalf("expected a snap detection, got %+v", m)
	}
}

func TestDetectManagedAUR(t *testing.T) {
	exists := func(p string) bool { return p == "/etc/arch-release" }
	m := DetectManaged("/usr/bin/multilauncher", envOf(nil), exists)
	if m == nil || m.ID != "aur" {
		t.Fatalf("expected an AUR detection, got %+v", m)
	}
	if !strings.Contains(m.Command, "yay") {
		t.Errorf("Command = %q, want a yay command", m.Command)
	}
}

func TestDetectManagedSystemPackage(t *testing.T) {
	m := DetectManaged("/usr/bin/multilauncher", envOf(nil), noFiles)
	if m == nil || m.ID != "system" {
		t.Fatalf("expected a system detection, got %+v", m)
	}
}

func TestDetectManagedBrew(t *testing.T) {
	exe := "/opt/homebrew/Caskroom/multi-launcher/1.2.3/Multi Launcher.app/Contents/MacOS/Multi Launcher"
	m := DetectManaged(exe, envOf(nil), noFiles)
	if m == nil || m.ID != "brew" {
		t.Fatalf("expected a brew detection, got %+v", m)
	}
}

func TestDetectManagedProgramFiles(t *testing.T) {
	exe := `C:\Program Files\Multi Launcher\Multi Launcher.exe`
	m := DetectManaged(exe, envOf(nil), noFiles)
	if m == nil || m.ID != "programfiles" {
		t.Fatalf("expected a programfiles detection, got %+v", m)
	}
}

func TestTryHostUpdateNotApplicable(t *testing.T) {
	for _, m := range []*Managed{nil, {ID: "aur"}, {ID: "system"}, {ID: "snap"}, {ID: "brew"}} {
		handled, err := TryHostUpdate(m)
		if handled || err != nil {
			t.Errorf("TryHostUpdate(%+v) = (%v, %v), want (false, nil)", m, handled, err)
		}
	}
}

func TestTryHostUpdateFlatpakWithoutSpawn(t *testing.T) {
	// flatpak-spawn does not exist on the test machine (nor on Windows/macOS
	// runners), so this must cleanly decline instead of failing.
	m := &Managed{ID: "flatpak", Name: "Flatpak", Command: "flatpak update io.example.App"}
	if _, err := exec.LookPath("flatpak-spawn"); err == nil {
		t.Skip("flatpak-spawn is installed; cannot test the missing-binary path")
	}
	handled, err := TryHostUpdate(m)
	if handled || err != nil {
		t.Errorf("TryHostUpdate = (%v, %v), want (false, nil) without flatpak-spawn", handled, err)
	}
}

func TestManagedErrorCarriesCommand(t *testing.T) {
	m := &Managed{ID: "aur", Name: "AUR", Command: "yay -Syu multilauncher"}
	err := managedError(m)
	if !IsKind(err, KindManaged) {
		t.Fatalf("kind = %v, want managed", errKind(err))
	}
	if !strings.Contains(err.Error(), "yay -Syu multilauncher") {
		t.Errorf("error %q should carry the update command", err.Error())
	}
}
