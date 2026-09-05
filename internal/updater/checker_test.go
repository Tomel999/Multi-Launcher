package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// releaseJSON renders a minimal release payload with the given tag and asset
// names.
func releaseJSON(tag string, assetNames ...string) []byte {
	assets := make([]map[string]any, 0, len(assetNames))
	for _, n := range assetNames {
		assets = append(assets, map[string]any{
			"name":                 n,
			"browser_download_url": "https://example.invalid/" + n,
			"size":                 1024,
		})
	}
	body, err := json.Marshal(map[string]any{
		"tag_name":     tag,
		"name":         "Multi Launcher " + tag,
		"body":         "### Changelog\n- fixed things",
		"html_url":     "https://github.com/Tomel999/Multi-Launcher/releases/tag/" + tag,
		"published_at": "2026-09-01T10:20:30Z",
		"draft":        false,
		"prerelease":   false,
		"assets":       assets,
	})
	if err != nil {
		panic(err)
	}
	return body
}

// serveRelease spins up an API stub that always answers with payload.
func serveRelease(t *testing.T, status int, payload []byte) *Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	c := NewChecker("Tomel999", "Multi-Launcher", "v1.0.0")
	c.Client.APIBase = srv.URL
	c.Client.Token = ""
	return c
}

func TestCheckForUpdateUpToDate(t *testing.T) {
	// The release carries the current version for every platform.
	c := serveRelease(t, http.StatusOK, releaseJSON("v1.0.0",
		"Multi-Launcher-windows-amd64.exe", "Multi-Launcher-darwin-arm64.zip",
		"Multi-Launcher-linux-amd64.tar.gz"))

	info, err := c.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info != nil {
		t.Fatalf("expected no update, got %+v", info)
	}
}

func TestCheckForUpdateOlderReleaseIsIgnored(t *testing.T) {
	c := serveRelease(t, http.StatusOK, releaseJSON("v0.9.0", "Multi-Launcher-windows-amd64.exe"))
	c.CurrentVersion = "v1.0.0"

	info, err := c.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info != nil {
		t.Fatalf("an older release must not be offered, got %+v", info)
	}
}

func TestCheckForUpdateAvailable(t *testing.T) {
	c := serveRelease(t, http.StatusOK, releaseJSON("v1.2.0",
		"Multi-Launcher-windows-amd64.exe",
		"Multi-Launcher-windows-amd64.exe.sha256",
		"Multi-Launcher-darwin-arm64.zip",
		"Multi-Launcher-linux-amd64.tar.gz"))
	c.CurrentVersion = "v1.0.0"

	info, err := c.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info == nil {
		t.Fatal("expected an update to be offered")
	}
	if info.Version != "v1.2.0" {
		t.Errorf("Version = %q, want v1.2.0", info.Version)
	}
	if info.TagName != "v1.2.0" {
		t.Errorf("TagName = %q", info.TagName)
	}
	if info.Current != "v1.0.0" {
		t.Errorf("Current = %q", info.Current)
	}
	if !strings.Contains(info.Changelog, "fixed things") {
		t.Errorf("Changelog = %q", info.Changelog)
	}
	if !strings.Contains(info.ReleaseURL, "releases/tag/v1.2.0") {
		t.Errorf("ReleaseURL = %q", info.ReleaseURL)
	}
	if want := "2026-09-01T10:20:30Z"; info.PublishedAt != want {
		t.Errorf("PublishedAt = %q, want %q", info.PublishedAt, want)
	}
	// AssetName must match this host's platform.
	wantAsset, err := AssetNameFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("unsupported host platform: %v", err)
	}
	if info.AssetName != wantAsset {
		t.Errorf("AssetName = %q, want %q", info.AssetName, wantAsset)
	}
	if info.Size != 1024 {
		t.Errorf("Size = %d, want 1024", info.Size)
	}
}

func TestCheckForUpdateDevBuild(t *testing.T) {
	// A hand-built binary has no real version; any tagged release is newer.
	c := serveRelease(t, http.StatusOK, releaseJSON("v0.1.0", "Multi-Launcher-windows-amd64.exe"))
	c.CurrentVersion = "dev"

	info, err := c.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info == nil {
		t.Fatal("a dev build should be offered the latest release")
	}
	if info.Current != "dev" {
		t.Errorf("Current = %q", info.Current)
	}
}

func TestCheckForUpdateMissingPlatformAsset(t *testing.T) {
	// Only a Windows build was published, and the tests may run elsewhere; use
	// a platform we can force by constructing the checker with a release the
	// host cannot match.
	c := serveRelease(t, http.StatusOK, releaseJSON("v2.0.0", "Multi-Launcher-plan9-amd64.zip"))
	c.CurrentVersion = "v1.0.0"

	_, err := c.CheckForUpdate(context.Background())
	if !IsKind(err, KindNoAsset) {
		t.Fatalf("error kind = %v, want no_asset (err=%v)", errKind(err), err)
	}
}

func TestCheckForUpdateNoReleasePublished(t *testing.T) {
	c := serveRelease(t, http.StatusNotFound, []byte(`{"message":"Not Found"}`))
	_, err := c.CheckForUpdate(context.Background())
	if !IsKind(err, KindNotFound) {
		t.Fatalf("error kind = %v, want not_found (err=%v)", errKind(err), err)
	}
}

func TestCheckForUpdateDraftReleaseIgnored(t *testing.T) {
	payload := []byte(`{"tag_name":"v9.9.9","draft":true,"assets":[]}`)
	c := serveRelease(t, http.StatusOK, payload)
	info, err := c.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if info != nil {
		t.Errorf("a draft release must not be offered, got %+v", info)
	}
}

func TestCheckForUpdateUnconfiguredRepo(t *testing.T) {
	c := NewChecker("", "", "v1.0.0")
	if _, err := c.CheckForUpdate(context.Background()); err == nil {
		t.Error("expected an error when owner/repo are unset")
	} else if !IsKind(err, KindHTTP) {
		t.Errorf("error kind = %v, want http", errKind(err))
	}
}

func TestCheckerNewUpdaterSharesClient(t *testing.T) {
	c := NewChecker("Tomel999", "Multi-Launcher", "v1.0.0")
	u := c.NewUpdater()
	if u == nil || u.client != c.Client {
		t.Error("NewUpdater should share the checker's client")
	}

	c.Client = nil
	if c.NewUpdater().client == nil {
		t.Error("NewUpdater must build a client when the checker has none")
	}
}

func TestUpdateInfoJSONShape(t *testing.T) {
	// The frontend reads these exact keys.
	info := UpdateInfo{
		Current: "v1.0.0", Version: "v1.2.0", TagName: "v1.2.0", Name: "n",
		Changelog: "text", PublishedAt: "2026-09-01T10:20:30Z", ReleaseURL: "https://x",
		AssetName: "Multi-Launcher-windows-amd64.exe", Size: 42,
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{`"current"`, `"version"`, `"tagName"`, `"changelog"`,
		`"publishedAt"`, `"releaseUrl"`, `"assetName"`, `"size"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("marshalled payload %s is missing %s", b, key)
		}
	}
	_ = fmt.Sprint(info.Name)
}
