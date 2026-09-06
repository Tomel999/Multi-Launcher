package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// payloadFor builds a deterministic payload of n bytes.
func payloadFor(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + (i % 26))
	}
	return b
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// downloadFixture spins up an HTTP server serving one asset plus its checksum
// sidecar, and returns a release/client wired to it.
type downloadFixture struct {
	server    *httptest.Server
	release   *Release
	client    *GitHubClient
	assetName string
	payload   []byte
	// checksum, when non-nil, overrides the served .sha256 content.
	checksum []byte
	// omitChecksum makes the sidecar return 404.
	omitChecksum bool
	// serveShort truncates the asset body to this many bytes.
	serveShort int
}

func newDownloadFixture(t *testing.T, payload []byte) *downloadFixture {
	t.Helper()
	f := &downloadFixture{payload: payload, assetName: "Multi-Launcher-linux-amd64.tar.gz"}

	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		body := f.payload
		if f.serveShort > 0 && f.serveShort < len(body) {
			body = body[:f.serveShort]
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/asset.sha256", func(w http.ResponseWriter, r *http.Request) {
		if f.omitChecksum {
			http.NotFound(w, r)
			return
		}
		sum := f.checksum
		if sum == nil {
			sum = []byte(sha256Hex(f.payload) + "  " + f.assetName + "\n")
		}
		_, _ = w.Write(sum)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	f.release = &Release{
		TagName: "v1.2.0",
		Assets: []Asset{
			{Name: f.assetName, URL: f.server.URL + "/asset", Size: int64(len(payload))},
			{Name: f.assetName + ".sha256", URL: f.server.URL + "/asset.sha256", Size: 96},
		},
	}
	f.client = NewGitHubClient("Tomel999", "Multi-Launcher")
	f.client.APIBase = f.server.URL
	f.client.Token = ""
	return f
}

func (f *downloadFixture) asset() Asset { return f.release.Assets[0] }

func (f *downloadFixture) updater() *Updater {
	u := NewUpdater(f.client)
	u.SetRelease(f.release)
	return u
}

// recorder is a thread-safe progress collector.
type recorder struct {
	mu    sync.Mutex
	calls [][2]int64
}

func (r *recorder) on(downloaded, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, [2]int64{downloaded, total})
}

func (r *recorder) snapshot() [][2]int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][2]int64, len(r.calls))
	copy(out, r.calls)
	return out
}

func TestDownloadSuccess(t *testing.T) {
	payload := payloadFor(int(progressBytes*2) + 1234) // force several callbacks
	f := newDownloadFixture(t, payload)
	dest := filepath.Join(t.TempDir(), "staged", "update.tar.gz")

	rec := &recorder{}
	if err := f.updater().Download(context.Background(), f.asset(), dest, rec.on); err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded %d bytes, want %d", len(got), len(payload))
	}
	if sum := sha256Hex(got); sum != sha256Hex(payload) {
		t.Errorf("content hash = %s, want %s", sum, sha256Hex(payload))
	}

	// No staging files may survive a successful download.
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(dest) {
			t.Errorf("leftover file %q in the update directory", e.Name())
		}
	}

	// Progress must be monotonic and end at 100%.
	calls := rec.snapshot()
	if len(calls) < 2 {
		t.Fatalf("expected multiple progress callbacks, got %d", len(calls))
	}
	var prev int64
	for i, c := range calls {
		if c[0] < prev {
			t.Errorf("progress went backwards at call %d: %d < %d", i, c[0], prev)
		}
		prev = c[0]
		if c[1] != int64(len(payload)) {
			t.Errorf("call %d total = %d, want %d", i, c[1], len(payload))
		}
	}
	if last := calls[len(calls)-1]; last[0] != int64(len(payload)) {
		t.Errorf("final progress = %d, want %d", last[0], len(payload))
	}
}

func TestDownloadChecksumMismatchLeavesInstallationIntact(t *testing.T) {
	payload := payloadFor(4096)
	f := newDownloadFixture(t, payload)
	f.checksum = []byte(strings.Repeat("b", 64) + "  " + f.assetName + "\n")

	dest := filepath.Join(t.TempDir(), "update.tar.gz")
	const existing = "ORIGINAL INSTALL"
	if err := os.WriteFile(dest, []byte(existing), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := f.updater().Download(context.Background(), f.asset(), dest, nil)
	if !IsKind(err, KindChecksum) {
		t.Fatalf("error kind = %v, want checksum (err=%v)", errKind(err), err)
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q should explain the mismatch", err.Error())
	}

	// The existing installation must be untouched.
	if got, _ := os.ReadFile(dest); string(got) != existing {
		t.Errorf("existing file was modified: %q", got)
	}
	// And the bad download must not be left behind.
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 {
		t.Errorf("temp file was not cleaned up: %d entries", len(entries))
	}
}

func TestDownloadMissingChecksumFailsClosed(t *testing.T) {
	f := newDownloadFixture(t, payloadFor(512))
	f.omitChecksum = true

	dest := filepath.Join(t.TempDir(), "update.tar.gz")
	err := f.updater().Download(context.Background(), f.asset(), dest, nil)
	if !IsKind(err, KindChecksum) {
		t.Fatalf("error kind = %v, want checksum (err=%v)", errKind(err), err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("dest must not be created when verification is impossible")
	}
}

func TestDownloadWithoutReleaseFailsClosed(t *testing.T) {
	f := newDownloadFixture(t, payloadFor(512))
	u := NewUpdater(f.client) // no SetRelease
	err := u.Download(context.Background(), f.asset(), filepath.Join(t.TempDir(), "x"), nil)
	if !IsKind(err, KindChecksum) {
		t.Fatalf("error kind = %v, want checksum (err=%v)", errKind(err), err)
	}
}

func TestDownloadChecksumDisabled(t *testing.T) {
	f := newDownloadFixture(t, payloadFor(1024))
	f.omitChecksum = true // would otherwise fail
	u := f.updater()
	u.ChecksumDisabled = true
	dest := filepath.Join(t.TempDir(), "update.tar.gz")
	if err := u.Download(context.Background(), f.asset(), dest, nil); err != nil {
		t.Fatalf("Download with verification disabled: %v", err)
	}
}

func TestDownloadTruncatedResponse(t *testing.T) {
	payload := payloadFor(4096)
	f := newDownloadFixture(t, payload)
	f.serveShort = 1000 // server stops early, but asset.Size still says 4096

	dest := filepath.Join(t.TempDir(), "update.tar.gz")
	err := f.updater().Download(context.Background(), f.asset(), dest, nil)
	if !IsKind(err, KindNetwork) {
		t.Fatalf("error kind = %v, want network (err=%v)", errKind(err), err)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error %q should mention truncation", err.Error())
	}
}

func TestDownloadCancelled(t *testing.T) {
	// A handler that stalls so the cancel wins.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := NewGitHubClient("o", "r")
	client.Token = ""
	u := NewUpdater(client)
	a := Asset{Name: "x.bin", URL: srv.URL + "/slow", Size: 1 << 20}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := u.Download(ctx, a, filepath.Join(t.TempDir(), "x.bin"), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsKind(err, KindNetwork) {
		t.Fatalf("error kind = %v, want network (err=%v)", errKind(err), err)
	}
}

func TestDownloadRejectsBadInput(t *testing.T) {
	f := newDownloadFixture(t, payloadFor(16))
	dest := filepath.Join(t.TempDir(), "x")
	if err := f.updater().Download(context.Background(), Asset{Name: "no-url"}, dest, nil); !IsKind(err, KindNetwork) {
		t.Errorf("empty URL: kind = %v, want network", errKind(err))
	}
	if err := NewUpdater(nil).Download(context.Background(), f.asset(), dest, nil); !IsKind(err, KindNetwork) {
		t.Errorf("nil client: kind = %v, want network", errKind(err))
	}
}

func TestDownloadCreatesParentDirectory(t *testing.T) {
	f := newDownloadFixture(t, payloadFor(64))
	dest := filepath.Join(t.TempDir(), "a", "b", "c", "update.zip")
	if err := f.updater().Download(context.Background(), f.asset(), dest, nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("dest should exist: %v", err)
	}
}

func TestParseChecksum(t *testing.T) {
	digest := strings.Repeat("A1b2", 16) // 64 hex chars, mixed case
	lower := strings.ToLower(digest)

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"gnu coreutils", digest + "  file.tar.gz\n", lower, false},
		{"single space", digest + " file.tar.gz", lower, false},
		{"binary mode", digest + " *file.tar.gz", lower, false},
		{"bare digest", digest, lower, false},
		{"bare with trailing newline", digest + "\n", lower, false},
		{"leading whitespace", "  " + digest + "  f\n", lower, false},
		{"empty", "", "", true},
		{"whitespace only", "   \n", "", true},
		{"too short", strings.Repeat("a", 63), "", true},
		{"too long", strings.Repeat("a", 65) + "  f", "", true},
		{"non hex", strings.Repeat("z", 64), "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseChecksum([]byte(c.in), "file.tar.gz")
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseChecksum(%q) = %q, want error", c.in, got)
				}
				if !IsKind(err, KindChecksum) {
					t.Errorf("error kind = %v, want checksum", errKind(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChecksum(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("ParseChecksum(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.bin")
	data := payloadFor(2048)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256: %v", err)
	}
	if want := sha256Hex(data); got != want {
		t.Errorf("FileSHA256 = %s, want %s", got, want)
	}
	if _, err := FileSHA256(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestProgressThrottling(t *testing.T) {
	// A payload far larger than progressBytes must not emit one callback per
	// chunk, but must still report the final total.
	payload := payloadFor(progressBytes * 8)
	f := newDownloadFixture(t, payload)
	rec := &recorder{}
	dest := filepath.Join(t.TempDir(), "u")
	if err := f.updater().Download(context.Background(), f.asset(), dest, rec.on); err != nil {
		t.Fatalf("Download: %v", err)
	}
	calls := rec.snapshot()
	// 2 MiB / 32 KiB chunks = 64 reads; we expect far fewer callbacks.
	if len(calls) > 20 {
		t.Errorf("progress is not throttled: %d callbacks for %d bytes", len(calls), len(payload))
	}
	if len(calls) < 2 {
		t.Errorf("expected at least a start and end callback, got %d", len(calls))
	}
	last := calls[len(calls)-1]
	if last[0] != int64(len(payload)) || last[1] != int64(len(payload)) {
		t.Errorf("final callback = %d/%d, want %d/%d", last[0], last[1], len(payload), len(payload))
	}
}

func TestCopyWithProgressNilCallback(t *testing.T) {
	// onProgress is optional; nothing should panic.
	n, err := copyWithProgress(context.Background(), &strings.Builder{},
		strings.NewReader("hello"), 5, nil)
	if err != nil {
		t.Fatalf("copyWithProgress: %v", err)
	}
	if n != 5 {
		t.Errorf("wrote %d bytes, want 5", n)
	}
}

