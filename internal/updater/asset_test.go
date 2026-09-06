package updater

import (
	"runtime"
	"strings"
	"testing"
)

// fullRelease contains an asset for every supported platform plus its
// checksum sidecar, mirroring what the release pipeline must publish.
func fullRelease() Release {
	platforms := []struct{ goos, goarch, ext string }{
		{"windows", "amd64", ".exe"},
		{"windows", "arm64", ".exe"},
		{"darwin", "amd64", ".zip"},
		{"darwin", "arm64", ".zip"},
		{"linux", "amd64", ".tar.gz"},
		{"linux", "arm64", ".tar.gz"},
	}
	r := Release{TagName: "v1.2.0"}
	for _, p := range platforms {
		name := AssetPrefix + "-" + p.goos + "-" + p.goarch + p.ext
		r.Assets = append(r.Assets,
			Asset{Name: name, URL: "https://example.invalid/" + name, Size: 1024},
			Asset{Name: name + ".sha256", URL: "https://example.invalid/" + name + ".sha256", Size: 96},
		)
	}
	// Unrelated files must be ignored rather than matched.
	r.Assets = append(r.Assets,
		Asset{Name: "Source code (zip)", URL: "https://example.invalid/src.zip"},
		Asset{Name: "launcher-windows-amd64.exe", URL: "https://example.invalid/old-name.exe"},
	)
	return r
}

func TestAssetNameFor(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{"windows", "amd64", "Multi-Launcher-windows-amd64.exe", false},
		{"windows", "arm64", "Multi-Launcher-windows-arm64.exe", false},
		{"darwin", "amd64", "Multi-Launcher-darwin-amd64.zip", false},
		{"darwin", "arm64", "Multi-Launcher-darwin-arm64.zip", false},
		{"linux", "amd64", "Multi-Launcher-linux-amd64.tar.gz", false},
		{"linux", "arm64", "Multi-Launcher-linux-arm64.tar.gz", false},
		{"plan9", "amd64", "", true},
		{"windows", "", "", true},
	}
	for _, c := range cases {
		got, err := AssetNameFor(c.goos, c.goarch)
		if c.wantErr {
			if err == nil {
				t.Errorf("AssetNameFor(%q,%q) = %q, want error", c.goos, c.goarch, got)
			} else if !IsKind(err, KindNoAsset) {
				t.Errorf("AssetNameFor(%q,%q) error kind = %v, want no_asset", c.goos, c.goarch, errKind(err))
			}
			continue
		}
		if err != nil {
			t.Errorf("AssetNameFor(%q,%q) unexpected error: %v", c.goos, c.goarch, err)
			continue
		}
		if got != c.want {
			t.Errorf("AssetNameFor(%q,%q) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestSelectAssetForAllPlatforms(t *testing.T) {
	rel := fullRelease()
	for _, p := range SupportedPlatforms {
		t.Run(p.GOOS+"/"+p.GOARCH, func(t *testing.T) {
			want, err := AssetNameFor(p.GOOS, p.GOARCH)
			if err != nil {
				t.Fatalf("AssetNameFor: %v", err)
			}
			got, err := SelectAssetFor(rel, p.GOOS, p.GOARCH)
			if err != nil {
				t.Fatalf("SelectAssetFor: %v", err)
			}
			if got.Name != want {
				t.Errorf("Name = %q, want %q", got.Name, want)
			}
			if got.URL == "" {
				t.Error("URL should be populated")
			}
		})
	}
}

func TestSelectAssetForIgnoresForeignAndLegacyNames(t *testing.T) {
	rel := fullRelease()
	got, err := SelectAssetFor(rel, "linux", "amd64")
	if err != nil {
		t.Fatalf("SelectAssetFor: %v", err)
	}
	if !strings.HasSuffix(got.Name, ".tar.gz") {
		t.Errorf("picked %q, want the tar.gz archive", got.Name)
	}
	if strings.Contains(got.Name, "Source code") {
		t.Error("must not pick the GitHub source archive")
	}
}

func TestSelectAssetForMissingAsset(t *testing.T) {
	rel := Release{TagName: "v9.9.9", Assets: []Asset{
		{Name: "Multi-Launcher-windows-amd64.exe", URL: "https://example.invalid/a"},
	}}

	_, err := SelectAssetFor(rel, "linux", "amd64")
	if err == nil {
		t.Fatal("expected an error for a platform with no asset")
	}
	if !IsKind(err, KindNoAsset) {
		t.Fatalf("error kind = %v, want no_asset", errKind(err))
	}
	// The message must be actionable: name the expected file and what was there.
	for _, want := range []string{"Multi-Launcher-linux-amd64.tar.gz", "v9.9.9", "Multi-Launcher-windows-amd64.exe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}

func TestSelectAssetForEmptyRelease(t *testing.T) {
	_, err := SelectAssetFor(Release{TagName: "v1.0.0"}, "windows", "amd64")
	if !IsKind(err, KindNoAsset) {
		t.Fatalf("error kind = %v, want no_asset", errKind(err))
	}
	if !strings.Contains(err.Error(), "none") {
		t.Errorf("error %q should report that no assets were published", err.Error())
	}
}

func TestSelectAssetForUnsupportedPlatform(t *testing.T) {
	_, err := SelectAssetFor(fullRelease(), "plan9", "amd64")
	if !IsKind(err, KindNoAsset) {
		t.Fatalf("error kind = %v, want no_asset", errKind(err))
	}
}

func TestSelectAssetForAssetWithoutURL(t *testing.T) {
	rel := Release{TagName: "v1.0.0", Assets: []Asset{
		{Name: "Multi-Launcher-linux-amd64.tar.gz"}, // no URL
	}}
	if _, err := SelectAssetFor(rel, "linux", "amd64"); !IsKind(err, KindNoAsset) {
		t.Fatalf("error kind = %v, want no_asset", errKind(err))
	}
}

func TestSelectAssetForPlatformUsesRuntime(t *testing.T) {
	// The current host must resolve; the exact name depends on where the test
	// runs, so just assert it matches the runtime pair.
	want, err := AssetNameFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("this platform (%s/%s) is not supported by the updater", runtime.GOOS, runtime.GOARCH)
	}
	got, err := SelectAssetForPlatform(fullRelease())
	if err != nil {
		t.Fatalf("SelectAssetForPlatform: %v", err)
	}
	if got.Name != want {
		t.Errorf("Name = %q, want %q", got.Name, want)
	}
}

func TestChecksumAssetFor(t *testing.T) {
	rel := fullRelease()
	a, err := SelectAssetFor(rel, "darwin", "arm64")
	if err != nil {
		t.Fatalf("SelectAssetFor: %v", err)
	}
	sum, err := ChecksumAssetFor(rel, a)
	if err != nil {
		t.Fatalf("ChecksumAssetFor: %v", err)
	}
	if want := "Multi-Launcher-darwin-arm64.zip.sha256"; sum.Name != want {
		t.Errorf("Name = %q, want %q", sum.Name, want)
	}
}

func TestChecksumAssetForMissingSidecar(t *testing.T) {
	rel := Release{TagName: "v1.0.0", Assets: []Asset{
		{Name: "Multi-Launcher-linux-amd64.tar.gz", URL: "https://example.invalid/a"},
	}}
	a, err := SelectAssetFor(rel, "linux", "amd64")
	if err != nil {
		t.Fatalf("SelectAssetFor: %v", err)
	}
	_, err = ChecksumAssetFor(rel, a)
	if !IsKind(err, KindChecksum) {
		t.Fatalf("error kind = %v, want checksum", errKind(err))
	}
	if !strings.Contains(err.Error(), ".sha256") {
		t.Errorf("error %q should name the missing checksum file", err.Error())
	}
}

