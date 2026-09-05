package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultAPIBase is the public GitHub REST API root. It is exported so tests
// can point a client at an httptest.Server.
const DefaultAPIBase = "https://api.github.com"

// EnvTokenNames lists the environment variables consulted for an optional
// GitHub token, in priority order. A token is never required: it only raises
// the unauthenticated rate limit from 60 to 5000 requests/hour. Tokens are
// read from the environment at client-construction time and are never
// hardcoded or persisted.
var EnvTokenNames = []string{"GH_TOKEN", "GITHUB_TOKEN"}

// TokenFromEnv returns the first non-empty GitHub token found in the
// environment, or "" when none is set.
func TokenFromEnv() string {
	for _, name := range EnvTokenNames {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// Asset is one downloadable file attached to a GitHub release.
type Asset struct {
	// Name is the file name, e.g. "Multi-Launcher-windows-amd64.exe".
	Name string `json:"name"`
	// URL is the browser download URL from the API payload.
	URL string `json:"browser_download_url"`
	// Size is the asset size in bytes as reported by GitHub.
	Size int64 `json:"size"`
}

// ChecksumName returns the name of the sidecar file holding this asset's
// SHA-256 digest, e.g. "Multi-Launcher-windows-amd64.exe.sha256".
func (a Asset) ChecksumName() string { return a.Name + ".sha256" }

// Release is the subset of a GitHub release payload the updater needs.
type Release struct {
	// TagName is the git tag, e.g. "v1.2.3". This is the version source of
	// truth; Name is only a human-readable title.
	TagName string `json:"tag_name"`
	// Name is the optional release title.
	Name string `json:"name"`
	// Body is the release description in markdown — shown verbatim as the
	// changelog in the update dialog.
	Body string `json:"body"`
	// PublishedAt is the release publication timestamp.
	PublishedAt time.Time `json:"published_at"`
	// HTMLURL is the human-facing release page, offered as a fallback link.
	HTMLURL    string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// AssetByName returns the release asset with the exact given file name.
func (r *Release) AssetByName(name string) (Asset, bool) {
	if r == nil {
		return Asset{}, false
	}
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// GitHubClient talks to the GitHub Releases API. All calls are context
// cancellable and bounded by a timeout.
//
// A zero Token means unauthenticated access, which is rate limited to 60
// requests/hour per IP — acceptable for one check per app launch.
type GitHubClient struct {
	// Owner is the GitHub user or organisation, e.g. "Tomel999".
	Owner string
	// Repo is the repository name, e.g. "Multi-Launcher".
	Repo string
	// Token is an optional personal access token (read-only is enough).
	Token string
	// APIBase overrides the API root; defaults to DefaultAPIBase.
	APIBase string
	// HTTP is the client used for API calls. Defaults to a 10s-timeout client.
	HTTP *http.Client
	// DownloadHTTP is the client used to fetch release binaries. It
	// deliberately has no overall timeout: downloads are large and are bounded
	// by the caller's context instead.
	DownloadHTTP *http.Client
}

// NewGitHubClient builds a client for owner/repo. The optional token is read
// from GH_TOKEN (falling back to GITHUB_TOKEN) unless one is supplied.
func NewGitHubClient(owner, repo string) *GitHubClient {
	return &GitHubClient{
		Owner:        owner,
		Repo:         repo,
		Token:        TokenFromEnv(),
		APIBase:      DefaultAPIBase,
		HTTP:         &http.Client{Timeout: 10 * time.Second},
		DownloadHTTP: &http.Client{},
	}
}

func (c *GitHubClient) apiBase() string {
	if c.APIBase == "" {
		return DefaultAPIBase
	}
	return strings.TrimRight(c.APIBase, "/")
}

func (c *GitHubClient) apiClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *GitHubClient) downloadClient() *http.Client {
	if c.DownloadHTTP != nil {
		return c.DownloadHTTP
	}
	return http.DefaultClient
}

// LatestReleaseURL is the endpoint this client queries.
func (c *GitHubClient) LatestReleaseURL() string {
	return fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.apiBase(), c.Owner, c.Repo)
}

// LatestRelease fetches the newest non-draft, non-prerelease release.
//
// Errors are typed: KindNotFound when no release has been published yet,
// KindRateLimit on 403/429, KindHTTP for other non-2xx responses, KindDecode
// for malformed JSON, and KindNetwork for transport failures.
func (c *GitHubClient) LatestRelease(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.LatestReleaseURL(), nil)
	if err != nil {
		return nil, WrapError(KindNetwork, "cannot build update request", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Multi-Launcher-Updater")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.apiClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, WrapError(KindNetwork, "update check cancelled", ctx.Err())
		}
		return nil, WrapError(KindNetwork, "update check failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.httpError(resp)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, WrapError(KindDecode, "cannot parse release metadata", err)
	}
	if rel.TagName == "" {
		return nil, NewError(KindDecode, "release payload has no tag_name")
	}
	return &rel, nil
}

// httpError converts a non-200 API response into a typed error, including the
// server's own message when it can be extracted.
func (c *GitHubClient) httpError(resp *http.Response) error {
	kind := httpKindFor(resp.StatusCode)

	// GitHub signals an exhausted unauthenticated budget with 403 and
	// X-RateLimit-Remaining: 0; treat any 403/429 as a rate limit.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(body))

	var apiErr struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		msg = apiErr.Message
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}

	switch kind {
	case KindNotFound:
		return NewError(KindNotFound, "no published release found").
			WithStatus(resp.StatusCode)
	case KindRateLimit:
		reset := resp.Header.Get("X-RateLimit-Reset")
		if reset != "" {
			if ts, err := strconv.ParseInt(reset, 10, 64); err == nil && ts > 0 {
				msg += fmt.Sprintf(" (limit resets at %s)", time.Unix(ts, 0).UTC().Format(time.RFC1123))
			}
		}
		if c.Token == "" {
			msg += " — set GH_TOKEN to raise the rate limit"
		}
		return NewError(KindRateLimit, msg).WithStatus(resp.StatusCode)
	default:
		return NewError(KindHTTP, msg).WithStatus(resp.StatusCode)
	}
}

// FetchAsset opens a streaming reader over a release asset's download URL.
//
// The Authorization header is intentionally omitted: browser_download_url
// redirects to a separate object-storage host that must not receive the
// token. The caller owns closing the returned body.
func (c *GitHubClient) FetchAsset(ctx context.Context, a Asset) (io.ReadCloser, error) {
	if a.URL == "" {
		return nil, NewError(KindNetwork, "asset has no download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, WrapError(KindNetwork, "cannot build download request", err)
	}
	req.Header.Set("User-Agent", "Multi-Launcher-Updater")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.downloadClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, WrapError(KindNetwork, "download cancelled", ctx.Err())
		}
		return nil, WrapError(KindNetwork, "download failed", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, NewError(httpKindFor(resp.StatusCode),
			fmt.Sprintf("download of %s failed", a.Name)).WithStatus(resp.StatusCode)
	}
	return resp.Body, nil
}

// FetchAssetBytes downloads a small asset (used for .sha256 sidecars) fully
// into memory, capped at maxBytes to avoid pulling a binary by mistake.
func (c *GitHubClient) FetchAssetBytes(ctx context.Context, a Asset, maxBytes int64) ([]byte, error) {
	rc, err := c.FetchAsset(ctx, a)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return nil, WrapError(KindNetwork, "cannot read asset", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, NewError(KindDecode, "asset exceeds size limit")
	}
	return data, nil
}
