package launcher

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These cover the two failure modes that made downloads unrecoverable:
//
//   - a cancel that could not interrupt a read already in flight, so a stalled
//     server kept the goroutine and its concurrency slot forever
//   - a retry loop that deleted the .part file, so every attempt restarted at
//     byte zero and the Range support never had anything to resume
//
// plus the retry policy and the idle timeout that replaced the permanent hang.

// stalledServer sends one chunk and then stops, simulating a connection that
// hangs mid-body. It unblocks when block is closed.
func stalledServer(block chan struct{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(1<<20))
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 4096))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-block
	}))
}

func TestDownloadSucceeds(t *testing.T) {
	body := []byte("hello launcher, this is a test payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	defer srv.Close()

	ResetInstallCancel()
	defer ResetInstallCancel()

	dest := filepath.Join(t.TempDir(), "f.bin")
	if err := downloadTo(srv.URL, dest, int64(len(body)), nil); err != nil {
		t.Fatalf("download failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("content mismatch: got %q", got)
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error(".part left behind after a successful download")
	}
}

func TestCancelInterruptsStalledRead(t *testing.T) {
	cases := []struct {
		name       string
		useTracker bool
	}{
		{"with progress tracker", true},
		{"without progress tracker", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := make(chan struct{})
			srv := stalledServer(block)
			defer srv.Close()
			defer close(block)

			ResetInstallCancel()
			defer ResetInstallCancel()

			var tr *tracker
			if tc.useTracker {
				tr = newTracker("test", nil)
			}
			dest := filepath.Join(t.TempDir(), "f.bin")

			done := make(chan error, 1)
			go func() { done <- downloadTo(srv.URL, dest, 1<<20, tr) }()

			time.Sleep(400 * time.Millisecond)
			CancelInstall()

			select {
			case err := <-done:
				if !errors.Is(err, ErrInstallCanceled) {
					t.Fatalf("got %v, want ErrInstallCanceled", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("download still blocked 3s after CancelInstall()")
			}

			// The partial file must survive a cancel, otherwise resuming later
			// means starting over.
			if _, err := os.Stat(dest + ".part"); err != nil {
				t.Error(".part was deleted on cancel, so the download cannot be resumed")
			}
		})
	}
}

func TestRetryResumesFromPartialFile(t *testing.T) {
	const (
		total = 1 << 20
		chunk = 65536
	)

	var (
		mu     sync.Mutex
		ranges []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rg := r.Header.Get("Range")
		mu.Lock()
		ranges = append(ranges, rg)
		mu.Unlock()

		start := int64(0)
		if v, ok := strings.CutPrefix(rg, "bytes="); ok {
			if n, err := strconv.ParseInt(strings.TrimSuffix(v, "-"), 10, 64); err == nil {
				start = n
			}
		}

		body := make([]byte, total)
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
			w.Header().Set("Content-Length", strconv.Itoa(total-int(start)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[start : start+chunk])
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(total))
			w.WriteHeader(http.StatusOK)
			w.Write(body[:chunk])
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Cut the connection mid-body: Content-Length promised more than sent.
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()

	ResetInstallCancel()
	defer ResetInstallCancel()

	dest := filepath.Join(t.TempDir(), "f.bin")
	if err := downloadTo(srv.URL, dest, total, nil); err == nil {
		t.Fatal("expected the truncated download to fail")
	}

	mu.Lock()
	defer mu.Unlock()
	const attempts = 3
	if len(ranges) != attempts {
		t.Fatalf("made %d attempts, want %d", len(ranges), attempts)
	}
	if ranges[0] != "" {
		t.Errorf("first attempt sent Range %q, want none", ranges[0])
	}
	for i := 1; i < len(ranges); i++ {
		want := "bytes=" + strconv.Itoa(i*chunk) + "-"
		if ranges[i] != want {
			t.Errorf("attempt %d sent Range %q, want %q", i+1, ranges[i], want)
		}
	}

	// Each attempt appended one chunk, which is only possible if the partial
	// file survived the previous failure.
	fi, err := os.Stat(dest + ".part")
	if err != nil {
		t.Fatalf(".part missing after failure, so nothing can be resumed: %v", err)
	}
	if want := int64(attempts * chunk); fi.Size() != want {
		t.Errorf(".part is %d bytes, want %d", fi.Size(), want)
	}
}

func TestPermanentErrorIsNotRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	ResetInstallCancel()
	defer ResetInstallCancel()

	err := downloadTo(srv.URL, filepath.Join(t.TempDir(), "f.bin"), 1024, nil)
	if err == nil {
		t.Fatal("expected an error for a 404")
	}
	var de *downloadError
	if !errors.As(err, &de) {
		t.Fatalf("got %T, want *downloadError", err)
	}
	if de.status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", de.status)
	}
	if de.retryable() {
		t.Error("a 404 must not be retryable")
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("server hit %d times, want exactly 1", n)
	}
}

func TestRetryableErrorsAreClassified(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{0, true}, // transport failure
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusNotFound, false},
		{http.StatusForbidden, false},
		{http.StatusGone, false},
		{http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		de := &downloadError{status: tc.status, err: errors.New("x")}
		if got := de.retryable(); got != tc.want {
			t.Errorf("status %d: retryable() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// The cache pre-pass must skip the network without skipping verification:
// an already-present file is never re-downloaded, but it is still hashed, so a
// corrupted cache entry is caught rather than silently used.
func TestRunJobsWarmCacheSkipsNetworkButStillVerifies(t *testing.T) {
	payload := []byte("cached payload")
	sum := sha1.Sum(payload)
	want := hex.EncodeToString(sum[:])

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Write(payload)
	}))
	defer srv.Close()

	ResetInstallCancel()
	defer ResetInstallCancel()
	SetDownloadConcurrency(4)
	defer SetDownloadConcurrency(8)

	dir := t.TempDir()
	jobs := make([]job, 0, 20)
	for i := 0; i < 20; i++ {
		jobs = append(jobs, job{
			url:  srv.URL,
			dest: filepath.Join(dir, "f"+strconv.Itoa(i)+".bin"),
			size: int64(len(payload)),
			sha1: want,
		})
	}

	run := func() error {
		var err error
		runJobs(jobs, newTracker("test", nil), &err)
		return err
	}

	if err := run(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 20 {
		t.Fatalf("first run made %d requests, want 20", n)
	}

	atomic.StoreInt32(&hits, 0)
	if err := run(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("warm cache made %d requests, want 0", n)
	}

	// Same length, different content: the size check passes, so only the hash
	// can catch it. This is what keeps the pre-pass from hiding corruption.
	if err := os.WriteFile(jobs[0].dest, []byte("cachXd payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Error("corrupted cache entry was not detected")
	}
}

func TestIdleTimeoutAbortsStalledDownload(t *testing.T) {
	old := readIdleTimeout
	readIdleTimeout = 300 * time.Millisecond
	defer func() { readIdleTimeout = old }()

	block := make(chan struct{})
	srv := stalledServer(block)
	defer srv.Close()
	defer close(block)

	ResetInstallCancel()
	defer ResetInstallCancel()

	start := time.Now()
	err := downloadTo(srv.URL, filepath.Join(t.TempDir(), "f.bin"), 1<<20, nil)
	if err == nil {
		t.Fatal("expected the stalled download to fail")
	}
	// Without the idle timeout this blocks until the OS TCP timeout, if ever.
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v to give up", d)
	}
}
