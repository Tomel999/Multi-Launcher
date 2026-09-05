// Package version exposes the launcher's build version and helpers for
// comparing semantic versions.
//
// The version is baked in at link time so a plain `go build` produces a
// "dev" build while release pipelines stamp a real tag:
//
//	go build -ldflags "-X 'multilauncherwails/internal/version.Version=v1.2.3'"
package version

import (
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

// strictSemver is the official semver.org regexp for a complete
// major.minor.patch version with optional prerelease and build metadata.
//
// It is used instead of semver.IsValid because golang.org/x/mod/semver
// deliberately accepts *partial* versions ("v1", "v1.2") for module paths,
// which are ambiguous for a launcher release tag and would make "v1.2" and
// "v1.2.0" compare as different versions.
var strictSemver = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// Version is the semantic version of this build. It defaults to "dev", which
// is deliberately not a valid semver string so that dev builds are treated as
// "unknown" rather than as a comparable release.
//
// Override at build time:
//
//	wails build -ldflags "-X 'multilauncherwails/internal/version.Version=v1.2.3'"
var Version = "dev"

// Normalize returns the canonical semver form understood by this package: a
// leading "v" is added when missing and surrounding whitespace is trimmed.
// Values that are already prefixed are returned unchanged.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// IsValid reports whether v is a complete semantic version
// (major.minor.patch, optional "-prerelease" and "+build") after
// normalization. Partial forms such as "v1.2" are rejected.
func IsValid(v string) bool {
	return strictSemver.MatchString(Normalize(v))
}

// Compare returns -1, 0 or +1 depending on whether current sorts before,
// equal to, or after latest. Both values may carry an optional leading "v".
// A valid version always sorts after an invalid one; two invalid versions
// compare by their normalized form.
func Compare(current, latest string) int {
	cur, lat := Normalize(current), Normalize(latest)
	curOK, latOK := IsValid(cur), IsValid(lat)
	switch {
	case curOK && latOK:
		return semver.Compare(cur, lat)
	case curOK:
		// current is a real release, latest is junk -> current sorts after.
		return 1
	case latOK:
		// current is junk, latest is a real release -> current sorts before.
		return -1
	default:
		return strings.Compare(cur, lat)
	}
}

// IsNewer reports whether latest represents a version strictly newer than
// current, i.e. whether an update should be offered.
//
// Policy for non-semver input (most importantly the default "dev" build):
//
//   - If latest is not a valid semver, no update is ever offered. A garbage
//     tag must never trigger an update prompt.
//   - If current is not valid semver (e.g. "dev") but latest is, an update IS
//     offered: a hand-built binary has no meaningful version, so any tagged
//     release is considered an improvement.
//
// Prerelease ordering follows semver rules, so v1.2.3-beta.1 < v1.2.3.
func IsNewer(current, latest string) bool {
	if !IsValid(latest) {
		return false
	}
	return Compare(current, latest) < 0
}

// IsDev reports whether the current build carries no real version, which
// callers can use to skip automatic update checks on developer machines.
func IsDev(v string) bool {
	return !IsValid(v)
}
