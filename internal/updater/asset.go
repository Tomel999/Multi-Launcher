package updater

import (
	"fmt"
	"runtime"
	"strings"
)

// AssetPrefix is the shared prefix of every release asset. It matches the
// "outputfilename" in wails.json ("Multi Launcher") with spaces replaced by
// hyphens so the names are safe to use in URLs and shell scripts.
//
// The full convention is documented in README.md next to this file and MUST be
// reproduced by the release/CI pipeline:
//
//	Multi-Launcher-<goos>-<goarch><ext>
//	Multi-Launcher-<goos>-<goarch><ext>.sha256
const AssetPrefix = "Multi-Launcher"

// assetExt maps a GOOS to the archive/binary extension the release pipeline
// produces for that platform.
//
//	Windows -> a portable .exe
//	macOS   -> a .zip containing the signed .app bundle
//	Linux   -> a .tar.gz containing the binary (or an AppImage, see Install)
var assetExt = map[string]string{
	"windows": ".exe",
	"darwin":  ".zip",
	"linux":   ".tar.gz",
}

// SupportedPlatforms lists every GOOS/GOARCH pair the updater can resolve. It
// is used for error messages and to keep tests exhaustive.
var SupportedPlatforms = []struct{ GOOS, GOARCH string }{
	{"windows", "amd64"},
	{"windows", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
}

// AssetNameFor builds the expected release asset file name for a platform,
// e.g. AssetNameFor("windows", "amd64") == "Multi-Launcher-windows-amd64.exe".
// An empty goarch or an unsupported goos yields an error.
func AssetNameFor(goos, goarch string) (string, error) {
	ext, ok := assetExt[goos]
	if !ok {
		return "", NewError(KindNoAsset, fmt.Sprintf(
			"unsupported operating system %q: the updater only supports %s",
			goos, strings.Join(supportedOS(), ", ")))
	}
	if goarch == "" {
		return "", NewError(KindNoAsset, "unknown CPU architecture for this build")
	}
	return AssetPrefix + "-" + goos + "-" + goarch + ext, nil
}

func supportedOS() []string {
	out := make([]string, 0, len(assetExt))
	for os := range assetExt {
		out = append(out, os)
	}
	return out
}

// SelectAssetFor returns the release asset matching the given GOOS/GOARCH.
// It is the platform-parameterised form of SelectAssetForPlatform and is what
// the tests exercise, since runtime.GOOS cannot vary within one binary.
func SelectAssetFor(r Release, goos, goarch string) (Asset, error) {
	name, err := AssetNameFor(goos, goarch)
	if err != nil {
		return Asset{}, err
	}
	a, ok := r.AssetByName(name)
	if !ok {
		return Asset{}, NewError(KindNoAsset, fmt.Sprintf(
			"release %s has no %s/%s build: expected an asset named %q (published assets: %s)",
			r.TagName, goos, goarch, name, assetList(r)))
	}
	if a.URL == "" {
		return Asset{}, NewError(KindNoAsset,
			fmt.Sprintf("asset %q in release %s has no download URL", name, r.TagName))
	}
	return a, nil
}

// SelectAssetForPlatform resolves the asset for the running binary's platform
// using runtime.GOOS and runtime.GOARCH.
func SelectAssetForPlatform(r Release) (Asset, error) {
	return SelectAssetFor(r, runtime.GOOS, runtime.GOARCH)
}

// ChecksumAssetFor returns the .sha256 sidecar asset for the given binary
// asset, which the downloader uses to verify integrity before installing.
func ChecksumAssetFor(r Release, a Asset) (Asset, error) {
	name := a.ChecksumName()
	c, ok := r.AssetByName(name)
	if !ok {
		return Asset{}, NewError(KindChecksum, fmt.Sprintf(
			"release %s is missing the checksum file %q for %s; it cannot be installed safely",
			r.TagName, name, a.Name))
	}
	return c, nil
}

// assetList renders the published asset names for use in error messages so a
// misconfigured pipeline is obvious from the log.
func assetList(r Release) string {
	if len(r.Assets) == 0 {
		return "none"
	}
	names := make([]string, 0, len(r.Assets))
	for _, a := range r.Assets {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}
