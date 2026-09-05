package updater

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// sampleRelease is a trimmed-down copy of a real GitHub release payload.
const sampleRelease = `{
  "url": "https://api.github.com/repos/Tomel999/Multi-Launcher/releases/1",
  "html_url": "https://github.com/Tomel999/Multi-Launcher/releases/tag/v1.2.0",
  "id": 1,
  "tag_name": "v1.2.0",
  "name": "Multi Launcher v1.2.0",
  "body": "## What's new\r\n- Faster downloads\r\n- Fixed a crash",
  "draft": false,
  "prerelease": false,
  "published_at": "2026-09-01T10:20:30Z",
  "assets": [
    {
      "name": "Multi-Launcher-windows-amd64.exe",
      "browser_download_url": "https://github.com/Tomel999/Multi-Launcher/releases/download/v1.2.0/Multi-Launcher-windows-amd64.exe",
      "size": 12345678
    },
    {
      "name": "Multi-Launcher-windows-amd64.exe.sha256",
      "browser_download_url": "https://github.com/Tomel999/Multi-Launcher/releases/download/v1.2.0/Multi-Launcher-windows-amd64.exe.sha256",
      "size": 96
    },
    {
      "name": "Multi-Launcher-darwin-arm64.zip",
      "browser_download_url": "https://github.com/Tomel999/Multi-Launcher/releases/download/v1.2.0/Multi-Launcher-darwin-arm64.zip",
      "size": 23456789
    }
  ]
}`

// newTestClient returns a client wired to srv plus the server itself.
func newTestClient(t *testing.T, srv *httptest.Server) *GitHubClient {
	t.Helper()
	c := NewGitHubClient("Tomel999", "Multi-Launcher")
	c.APIBase = srv.URL
	// Avoid inheriting a developer's real token during tests.
	c.Token = ""
	return c
}

func TestLatestReleaseParsesPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/repos/Tomel999/Multi-Launcher/releases/latest" {
			t.Errorf("unexpected path %q", got)
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing GitHub Accept header: %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleRelease))
	}))
	defer srv.Close()

	rel, err := newTestClient(t, srv).LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if rel.TagName != "v1.2.0" {
		t.Errorf("TagName = %q, want v1.2.0", rel.TagName)
	}
	if rel.Name != "Multi Launcher v1.2.0" {
		t.Errorf("Name = %q", rel.Name)
	}
	if !strings.Contains(rel.Body, "Faster downloads") {
		t.Errorf("Body = %q, want markdown changelog", rel.Body)
	}
	if rel.HTMLURL == "" {
		t.Error("HTMLURL should be populated")
	}
	want := time.Date(2026, 9, 1, 10, 20, 30, 0, time.UTC)
	if !rel.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v, want %v", rel.PublishedAt, want)
	}
	if len(rel.Assets) != 3 {
		t.Fatalf("Assets len = %d, want 3", len(rel.Assets))
	}
	if rel.Assets[0].Name != "Multi-Launcher-windows-amd64.exe" {
		t.Errorf("Assets[0].Name = %q", rel.Assets[0].Name)
	}
	if rel.Assets[0].Size != 12345678 {
		t.Errorf("Assets[0].Size = %d", rel.Assets[0].Size)
	}
	if !strings.HasSuffix(rel.Assets[0].URL, "Multi-Launcher-windows-amd64.exe") {
		t.Errorf("Assets[0].URL = %q", rel.Assets[0].URL)
	}
}

func TestLatestReleaseSendsTokenOnlyWhenSet(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(sampleRelease))
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).LatestRelease(context.Background()); err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if sawAuth != "" {
		t.Errorf("Authorization header should be absent, got %q", sawAuth)
	}

	c := newTestClient(t, srv)
	c.Token = "ghp_example"
	if _, err := c.LatestRelease(context.Background()); err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if sawAuth != "Bearer ghp_example" {
		t.Errorf("Authorization = %q, want %q", sawAuth, "Bearer ghp_example")
	}
}

func TestLatestReleaseErrorKinds(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		want    Kind
	}{
		{
			name:   "not found",
			status: http.StatusNotFound,
			body:   `{"message":"Not Found"}`,
			want:   KindNotFound,
		},
		{
			name:    "rate limited",
			status:  http.StatusForbidden,
			body:    `{"message":"API rate limit exceeded"}`,
			headers: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1800000000"},
			want:    KindRateLimit,
		},
		{
			name:   "too many requests",
			status: http.StatusTooManyRequests,
			body:   `{"message":"Too Many Requests"}`,
			want:   KindRateLimit,
		},
		{
			name:   "server error",
			status: http.StatusInternalServerError,
			body:   `{"message":"boom"}`,
			want:   KindHTTP,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range c.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			_, err := newTestClient(t, srv).LatestRelease(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if !IsKind(err, c.want) {
				t.Fatalf("error kind = %v, want %v (err=%v)", errKind(err), c.want, err)
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatal("expected a typed *Error")
			}
			if e.Status != c.status {
				t.Errorf("Status = %d, want %d", e.Status, c.status)
			}
		})
	}
}

func TestLatestReleaseMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": `))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).LatestRelease(context.Background())
	if !IsKind(err, KindDecode) {
		t.Fatalf("error kind = %v, want decode (err=%v)", errKind(err), err)
	}
}

func TestLatestReleaseMissingTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"no tag","assets":[]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).LatestRelease(context.Background())
	if !IsKind(err, KindDecode) {
		t.Fatalf("error kind = %v, want decode (err=%v)", errKind(err), err)
	}
}

func TestLatestReleaseNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // nothing is listening any more

	_, err := newTestClient(t, srv).LatestRelease(context.Background())
	if !IsKind(err, KindNetwork) {
		t.Fatalf("error kind = %v, want network (err=%v)", errKind(err), err)
	}
}

func TestLatestReleaseContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte(sampleRelease))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestClient(t, srv).LatestRelease(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsKind(err, KindNetwork) {
		t.Fatalf("error kind = %v, want network (err=%v)", errKind(err), err)
	}
}

func TestFetchAsset(t *testing.T) {
	const payload = "hello world"
	var gotUA, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.Token = "ghp_example" // must NOT be forwarded to the download host

	a := Asset{Name: "x.bin", URL: srv.URL + "/download/x.bin"}
	data, err := c.FetchAssetBytes(context.Background(), a, 1024)
	if err != nil {
		t.Fatalf("FetchAssetBytes: %v", err)
	}
	if string(data) != payload {
		t.Errorf("data = %q, want %q", data, payload)
	}
	if gotAuth != "" {
		t.Errorf("Authorization must not be sent to the download host, got %q", gotAuth)
	}
	if gotUA == "" {
		t.Error("User-Agent should be set")
	}
}

func TestFetchAssetErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(strings.Repeat("a", 100)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	if _, err := c.FetchAssetBytes(context.Background(), Asset{Name: "no-url"}, 10); !IsKind(err, KindNetwork) {
		t.Errorf("empty URL: kind = %v, want network", errKind(err))
	}
	if _, err := c.FetchAssetBytes(context.Background(), Asset{Name: "missing", URL: srv.URL + "/missing"}, 10); !IsKind(err, KindNotFound) {
		t.Errorf("404: kind = %v, want not_found", errKind(err))
	}
	if _, err := c.FetchAssetBytes(context.Background(), Asset{Name: "big", URL: srv.URL + "/ok"}, 10); !IsKind(err, KindDecode) {
		t.Errorf("oversize: kind = %v, want decode", errKind(err))
	}
}

func TestTokenFromEnv(t *testing.T) {
	for _, name := range EnvTokenNames {
		_ = os.Unsetenv(name)
	}
	if got := TokenFromEnv(); got != "" {
		t.Errorf("TokenFromEnv with empty env = %q, want empty", got)
	}

	t.Setenv("GITHUB_TOKEN", "from-github-token")
	if got := TokenFromEnv(); got != "from-github-token" {
		t.Errorf("TokenFromEnv = %q, want from-github-token", got)
	}

	t.Setenv("GH_TOKEN", "from-gh-token")
	if got := TokenFromEnv(); got != "from-gh-token" {
		t.Errorf("GH_TOKEN should win, got %q", got)
	}
}

func TestAssetByNameAndChecksumName(t *testing.T) {
	var rel Release
	if err := json.Unmarshal([]byte(sampleRelease), &rel); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	a, ok := rel.AssetByName("Multi-Launcher-darwin-arm64.zip")
	if !ok {
		t.Fatal("expected to find the darwin asset")
	}
	if a.Size != 23456789 {
		t.Errorf("Size = %d", a.Size)
	}
	if got := a.ChecksumName(); got != "Multi-Launcher-darwin-arm64.zip.sha256" {
		t.Errorf("ChecksumName = %q", got)
	}
	if _, ok := rel.AssetByName("nope"); ok {
		t.Error("unexpected match for a missing asset")
	}

	var nilRel *Release
	if _, ok := nilRel.AssetByName("x"); ok {
		t.Error("nil release should not match")
	}
}

func TestErrorFormatting(t *testing.T) {
	e := WrapError(KindNetwork, "update check failed", context.DeadlineExceeded).WithStatus(0)
	if !strings.Contains(e.Error(), "network") || !strings.Contains(e.Error(), "context deadline exceeded") {
		t.Errorf("Error() = %q", e.Error())
	}
	if !IsKind(e, KindNetwork) {
		t.Error("IsKind should find the wrapped kind")
	}
	if IsKind(nil, KindNetwork) {
		t.Error("IsKind(nil) must be false")
	}
}

// helpers ------------------------------------------------------------------

func errKind(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return Kind("(not an updater error)")
}
