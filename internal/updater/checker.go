package updater

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"multilauncherwails/internal/version"
)

// UpdateInfo is the payload handed to the frontend when a newer release
// exists. It is emitted verbatim as the "update:available" Wails event, so
// the JSON tags are part of the frontend contract.
type UpdateInfo struct {
	// Current is the running build's version string.
	Current string `json:"current"`
	// Version is the new version, normalized to a leading "v" (e.g. "v1.2.3").
	Version string `json:"version"`
	// TagName is the raw git tag as published on GitHub.
	TagName string `json:"tagName"`
	// Name is the optional human-readable release title.
	Name string `json:"name"`
	// Changelog is the release body (markdown) rendered as preformatted text
	// by the UI.
	Changelog string `json:"changelog"`
	// PublishedAt is when the release went out, as an RFC 3339 string. It is
	// a string rather than a time.Time so the generated TypeScript binding is
	// a plain string instead of `any`.
	PublishedAt string `json:"publishedAt"`
	// ReleaseURL is the release page, offered so users can inspect it first.
	ReleaseURL string `json:"releaseUrl"`
	// AssetName is the file that will be downloaded for this platform.
	AssetName string `json:"assetName"`
	// Size is the download size in bytes; 0 if GitHub did not report one.
	Size int64 `json:"size"`
	// Managed is non-nil when the running app is owned by a system package
	// manager and cannot self-update. The UI must show Manager.Command
	// instead of an "Update now" button.
	Managed *Managed `json:"managed,omitempty"`
}

// Checker looks for a newer release on GitHub Releases and describes it in
// frontend-friendly terms. It performs no I/O beyond a single API call and is
// safe for concurrent use.
type Checker struct {
	// Owner is the GitHub user or organisation, e.g. "Tomel999".
	Owner string
	// Repo is the repository name, e.g. "Multi-Launcher".
	Repo string
	// CurrentVersion is the running build's version, e.g. "v1.2.3" or "dev".
	CurrentVersion string
	// Client performs the API calls.
	Client *GitHubClient

	mu          sync.Mutex
	lastRelease *Release
}

// LastRelease returns the release inspected by the most recent CheckForUpdate
// call, or nil if none succeeded yet. The Updater needs it to locate an
// asset's "<name>.sha256" sidecar.
func (c *Checker) LastRelease() *Release {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastRelease
}

func (c *Checker) setLastRelease(r *Release) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastRelease = r
}

// NewChecker builds a checker for owner/repo using the default GitHub client.
func NewChecker(owner, repo, currentVersion string) *Checker {
	return &Checker{
		Owner:          owner,
		Repo:           repo,
		CurrentVersion: currentVersion,
		Client:         NewGitHubClient(owner, repo),
	}
}

// CheckForUpdate queries GitHub for the latest release and compares it to the
// running version.
//
// It returns:
//   - (nil, nil) when the app is already up to date, or when the latest
//     release is not newer. This is the common case and must be silent.
//   - (*UpdateInfo, nil) when a newer release is available for this platform.
//   - (nil, error) when the check could not be completed. Errors are typed
//     (*Error) so callers can distinguish "no release published yet" from
//     network or rate-limit failures; none of them are fatal.
func (c *Checker) CheckForUpdate(ctx context.Context) (*UpdateInfo, error) {
	if c.Owner == "" || c.Repo == "" {
		return nil, NewError(KindHTTP, "update repository is not configured")
	}
	client := c.Client
	if client == nil {
		client = NewGitHubClient(c.Owner, c.Repo)
	}

	rel, err := client.LatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	if rel == nil || rel.Draft {
		return nil, nil
	}
	c.setLastRelease(rel)
	if !version.IsNewer(c.CurrentVersion, rel.TagName) {
		return nil, nil
	}

	asset, err := SelectAssetForPlatform(*rel)
	if err != nil {
		// A newer release exists but ships no build for this platform. Not an
		// error the user can act on, so it is reported as a typed no-asset
		// error and swallowed by the caller.
		return nil, err
	}

	info := &UpdateInfo{
		Current:     c.CurrentVersion,
		Version:     version.Normalize(rel.TagName),
		TagName:     rel.TagName,
		Name:        rel.Name,
		Changelog:   strings.TrimSpace(rel.Body),
		PublishedAt: rel.PublishedAt.UTC().Format(time.RFC3339),
		ReleaseURL:  rel.HTMLURL,
		AssetName:   asset.Name,
		Size:        asset.Size,
	}
	return info, nil
}

// NewUpdater returns an Updater sharing this checker's GitHub client, ready to
// download the assets of the release that CheckForUpdate inspected.
func (c *Checker) NewUpdater() *Updater {
	client := c.Client
	if client == nil {
		client = NewGitHubClient(c.Owner, c.Repo)
	}
	return NewUpdater(client)
}

// Updater downloads and stages a release asset, then hands it to the
// platform-specific Install routine.
//
// It keeps a reference to the Release so Download can locate the matching
// "<asset>.sha256" sidecar and verify integrity before anything is installed.
type Updater struct {
	client *GitHubClient

	mu      sync.Mutex
	release *Release

	// ChecksumDisabled skips SHA-256 verification. It exists for local
	// testing only and must never be enabled in a release build.
	ChecksumDisabled bool
}

// NewUpdater builds an Updater over an existing GitHub client.
func NewUpdater(client *GitHubClient) *Updater {
	return &Updater{client: client}
}

// SetRelease records the release whose assets will be downloaded. It must be
// called before Download when checksum verification is required (the default).
func (u *Updater) SetRelease(r *Release) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.release = r
}

// checksumAsset resolves the .sha256 sidecar for an asset from the recorded
// release.
func (u *Updater) checksumAsset(a Asset) (Asset, error) {
	u.mu.Lock()
	rel := u.release
	u.mu.Unlock()
	if rel == nil {
		return Asset{}, NewError(KindChecksum, fmt.Sprintf(
			"cannot verify %s: no release metadata is loaded", a.Name))
	}
	return ChecksumAssetFor(*rel, a)
}
