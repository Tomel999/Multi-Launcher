package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"multilauncherwails/internal/updater"
	"multilauncherwails/internal/version"
)

// These tests cover the updater glue added to app.go: the environment switches
// that keep the startup check silent and non-fatal, and the wiring that hands
// the frontend a usable checker.

func TestEnvFlag(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"anything", true},
	}
	for _, c := range cases {
		t.Setenv("MULTILAUNCHER_TEST_FLAG", c.in)
		if got := envFlag("MULTILAUNCHER_TEST_FLAG"); got != c.want {
			t.Errorf("envFlag(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestUpdateChecksEnabled(t *testing.T) {
	// The shipped default is "dev", so dev-build behaviour is what runs here.
	if !version.IsDev(version.Version) {
		t.Skip("this test asserts dev-build behaviour; version is stamped")
	}

	t.Run("disabled by NO_UPDATE_CHECK", func(t *testing.T) {
		t.Setenv("MULTILAUNCHER_NO_UPDATE_CHECK", "1")
		t.Setenv("MULTILAUNCHER_UPDATE_CHECK_DEV", "1")
		if updateChecksEnabled() {
			t.Error("MULTILAUNCHER_NO_UPDATE_CHECK must win over the dev opt-in")
		}
	})

	t.Run("dev build off by default", func(t *testing.T) {
		t.Setenv("MULTILAUNCHER_NO_UPDATE_CHECK", "")
		t.Setenv("MULTILAUNCHER_UPDATE_CHECK_DEV", "")
		if updateChecksEnabled() {
			t.Error("a dev build must not auto-check unless explicitly opted in")
		}
	})

	t.Run("dev build opt-in", func(t *testing.T) {
		t.Setenv("MULTILAUNCHER_NO_UPDATE_CHECK", "")
		t.Setenv("MULTILAUNCHER_UPDATE_CHECK_DEV", "1")
		if !updateChecksEnabled() {
			t.Error("MULTILAUNCHER_UPDATE_CHECK_DEV=1 should enable the check")
		}
	})
}

func TestAutoUpdateEnabled(t *testing.T) {
	t.Run("on by default", func(t *testing.T) {
		t.Setenv("MULTILAUNCHER_NO_AUTO_UPDATE", "")
		if !autoUpdateEnabled() {
			t.Error("silent auto-update should be on unless explicitly disabled")
		}
	})
	for _, v := range []string{"1", "true", "yes"} {
		t.Setenv("MULTILAUNCHER_NO_AUTO_UPDATE", v)
		if autoUpdateEnabled() {
			t.Errorf("MULTILAUNCHER_NO_AUTO_UPDATE=%q should switch to notify mode", v)
		}
	}
}

func TestUpdateCacheDir(t *testing.T) {
	dir, err := updateCacheDir()
	if err != nil {
		t.Fatalf("updateCacheDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("updateCacheDir = %q, want an absolute path", dir)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(%s): %v", dir, err)
	}
	if !fi.IsDir() {
		t.Errorf("%s should be a directory", dir)
	}
	// The staging area must be writable, otherwise every download fails.
	probe := filepath.Join(dir, ".write-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		t.Fatalf("cache dir is not writable: %v", err)
	}
	_ = os.Remove(probe)
}

func TestNewAppWiresUpdateChecker(t *testing.T) {
	a := NewApp()
	if a.updateChecker == nil {
		t.Fatal("NewApp must build an update checker")
	}
	if a.updateChecker.Owner != updateOwner || a.updateChecker.Repo != updateRepo {
		t.Errorf("checker repo = %s/%s, want %s/%s",
			a.updateChecker.Owner, a.updateChecker.Repo, updateOwner, updateRepo)
	}
	if a.updateChecker.CurrentVersion != version.Version {
		t.Errorf("CurrentVersion = %q, want %q",
			a.updateChecker.CurrentVersion, version.Version)
	}
	if a.updateChecker.Client == nil {
		t.Error("checker client must be set")
	}
}

func TestGetAppVersion(t *testing.T) {
	if got := (&App{}).GetAppVersion(); got != version.Version {
		t.Errorf("GetAppVersion = %q, want %q", got, version.Version)
	}
}

func TestCheckForUpdateWithoutChecker(t *testing.T) {
	a := &App{} // no checker
	if _, err := a.checkForUpdate(context.Background()); err == nil {
		t.Error("expected an error when the checker is missing")
	}
}

// A launcher with no checker must not crash the startup path, even though
// scheduleUpdateCheck only calls it when checks are enabled.
func TestScheduleUpdateCheckDisabledIsANoOp(t *testing.T) {
	t.Setenv("MULTILAUNCHER_NO_UPDATE_CHECK", "1")
	a := NewApp()
	a.ctx = context.Background()
	a.scheduleUpdateCheck() // must return without panicking or blocking
}

func TestUpdateProgressPercent(t *testing.T) {
	// Mirrors the arithmetic in DownloadUpdate's progress callback.
	pct := func(downloaded, total int64) int {
		if total <= 0 {
			return 0
		}
		p := int(downloaded * 100 / total)
		if p > 100 {
			p = 100
		}
		return p
	}
	cases := []struct {
		down, total int64
		want        int
	}{
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		{150, 100, 100}, // never exceed 100
		{10, 0, 0},      // unknown total
	}
	for _, c := range cases {
		if got := pct(c.down, c.total); got != c.want {
			t.Errorf("percent(%d, %d) = %d, want %d", c.down, c.total, got, c.want)
		}
	}
}

func TestUpdateErrorPayloadShape(t *testing.T) {
	// The frontend switches on these fields.
	e := UpdateError{Message: "boom", Kind: string(updater.KindChecksum), Stage: "download"}
	if e.Kind != "checksum" {
		t.Errorf("Kind = %q", e.Kind)
	}
}
