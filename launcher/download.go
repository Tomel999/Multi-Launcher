package launcher

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"multilauncherwails/launcher/plugin"
)

// maxConcurrency is the hard ceiling for parallel downloads. SetDownloadConcurrency
// clamps to this, and the auto scaler never grows past it.
const maxConcurrency = 32

// readIdleTimeout aborts a download that makes no read progress for this long.
// Without it a server that stops sending mid-body would block the goroutine
// forever, holding a concurrency slot. Declared as a variable only so tests can
// shorten it.
var readIdleTimeout = 60 * time.Second

var downloadConcurrency int64

// installCanceled is set when the user cancels an in-progress game file
// installation. Checked between and during downloads.
var installCanceled atomic.Bool

// installCtx is the parent context for every in-flight download. Cancelling it
// is what actually unblocks a goroutine sitting in resp.Body.Read() - checking
// installCanceled alone can only stop the *next* read, so a stalled server
// would keep the goroutine (and its concurrency slot) forever.
var (
	cancelMu    sync.Mutex
	installCtx  context.Context    = context.Background()
	stopInstall context.CancelFunc = func() {}
)

var ErrInstallCanceled = errors.New("installation canceled")

// CancelInstall requests cancellation of any running installation/launch
// preparation downloads. Safe to call at any time.
func CancelInstall() {
	installCanceled.Store(true)
	cancelMu.Lock()
	stopInstall()
	cancelMu.Unlock()
}

// ResetInstallCancel clears the cancel flag. Call before starting a new launch.
func ResetInstallCancel() {
	installCanceled.Store(false)
	cancelMu.Lock()
	ctx, stop := context.WithCancel(context.Background())
	installCtx, stopInstall = ctx, stop
	cancelMu.Unlock()
}

func currentInstallCtx() context.Context {
	cancelMu.Lock()
	defer cancelMu.Unlock()
	return installCtx
}

// The client is shared by every downloader. Several hosts can be in flight at
// the same time (Lunar fetches artifacts, textures and UI in parallel alongside
// Mojang assets), so the global idle pool is sized above the per-host cap to
// avoid churning connections. There is deliberately no Client.Timeout: a large
// JDK or client jar legitimately takes minutes, and per-read stalls are handled
// by readIdleTimeout instead.
var downloadClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   maxConcurrency,
		MaxConnsPerHost:       maxConcurrency,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
	},
}

func SetDownloadConcurrency(n int) {
	if n < 0 {
		n = 0
	}
	if n > maxConcurrency {
		n = maxConcurrency
	}
	atomic.StoreInt64(&downloadConcurrency, int64(n))
}

func GetDownloadConcurrency() int {
	return int(atomic.LoadInt64(&downloadConcurrency))
}

type dynamicSem struct {
	mu    sync.Mutex
	cond  *sync.Cond
	used  int
	limit int
}

func newDynamicSem(n int) *dynamicSem {
	s := &dynamicSem{limit: n}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *dynamicSem) acquire() {
	s.mu.Lock()
	for s.used >= s.limit {
		s.cond.Wait()
	}
	s.used++
	s.mu.Unlock()
}

func (s *dynamicSem) release() {
	s.mu.Lock()
	s.used--
	s.cond.Signal()
	s.mu.Unlock()
}

func (s *dynamicSem) setLimit(n int) {
	s.mu.Lock()
	if n < 1 {
		n = 1
	}
	s.limit = n
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *dynamicSem) currentLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}

type ProgressFn func(p Progress)

type tracker struct {
	mu           sync.Mutex
	phase        string
	current      int64
	total        int64
	bytes        int64
	totalBytes   int64
	file         string
	files        map[string]*fileProg
	speed        int64
	last         time.Time
	lastProgress time.Time
	windowBytes  int64
	started      time.Time
	lastEmit     time.Time
	fn           ProgressFn
}

// speedStale is how long a speed measurement stays meaningful. Beyond this the
// tracker reports zero rather than a value computed before a stall.
const speedStale = 2 * time.Second

type fileProg struct {
	bytes     int64
	size      int64
	speed     int64
	lastBytes int64
	lastTime  time.Time
}

func newTracker(phase string, fn ProgressFn) *tracker {
	now := time.Now()
	return &tracker{phase: phase, fn: fn, last: now, started: now, lastEmit: now, files: make(map[string]*fileProg)}
}

func (t *tracker) setTotal(total, totalBytes int64) {
	t.mu.Lock()
	t.total, t.totalBytes = total, totalBytes
	t.mu.Unlock()
	t.emit()
}

func (t *tracker) adoptContentLength(total, clen int64) {
	t.mu.Lock()
	if t.totalBytes != 0 {
		t.mu.Unlock()
		return
	}
	if t.total == 0 {
		t.total = total
	}
	t.totalBytes = clen
	t.mu.Unlock()
	t.emit()
}

func (t *tracker) addBytes(file string, size, n int64) {
	t.mu.Lock()
	t.file = file
	now := time.Now()
	if fp, ok := t.files[file]; ok {
		fp.bytes += n
		if size > 0 {
			fp.size = size
		}
		dt := now.Sub(fp.lastTime).Seconds()
		if dt >= 0.5 {
			fp.speed = int64(float64(fp.bytes-fp.lastBytes) / dt)
			fp.lastBytes = fp.bytes
			fp.lastTime = now
		}
	} else {
		t.files[file] = &fileProg{bytes: n, size: size, lastBytes: n, lastTime: now}
	}
	t.bytes += n
	t.windowBytes += n
	t.lastProgress = now
	dt := now.Sub(t.last).Seconds()
	if dt >= 0.5 {
		t.speed = int64(float64(t.windowBytes) / dt)
		t.windowBytes = 0
		t.last = now
	}
	shouldEmit := now.Sub(t.lastEmit) >= 150*time.Millisecond
	if shouldEmit {
		t.lastEmit = now
	}
	t.mu.Unlock()
	if shouldEmit {
		t.emit()
	}
}

func (t *tracker) fileDone(file string) {
	t.mu.Lock()
	t.current++
	delete(t.files, file)
	if t.file == file {
		t.file = ""
	}
	now := time.Now()
	shouldEmit := now.Sub(t.lastEmit) >= 150*time.Millisecond
	if shouldEmit {
		t.lastEmit = now
	}
	t.mu.Unlock()
	if shouldEmit {
		t.emit()
	}
}

// currentSpeed returns the aggregate throughput in bytes per second, or zero if
// the measurement has gone stale. Reporting zero matters: the concurrency
// scaler polls this, and acting on a value computed before a stall would make
// it tune against a download that is no longer moving.
func (t *tracker) currentSpeed() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.lastProgress) > speedStale {
		return 0
	}
	return t.speed
}

// creditBytes adds to the byte counter without marking a file done. Used for
// files already on disk: their bytes are part of the job's total even though
// they never cross the network, so the byte progress stays in step with the
// file progress.
func (t *tracker) creditBytes(n int64) {
	if t == nil || n <= 0 {
		return
	}
	t.mu.Lock()
	t.bytes += n
	t.mu.Unlock()
}

func (t *tracker) emit() {
	t.mu.Lock()
	files := make([]FileProgress, 0, len(t.files))
	for name, fp := range t.files {
		files = append(files, FileProgress{Name: name, Bytes: fp.bytes, Size: fp.size, Speed: fp.speed})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	p := Progress{
		Phase:      t.phase,
		Current:    t.current,
		Total:      t.total,
		Bytes:      t.bytes,
		TotalBytes: t.totalBytes,
		Speed:      t.speed,
		File:       t.file,
		Files:      files,
	}
	t.mu.Unlock()
	if t.fn != nil {
		t.fn(p)
	}
	plugin.Bus.Emit("download.progress", p)
}

// downloadError carries the HTTP status so callers can tell a permanent failure
// from a transient one. That distinction drives two decisions: whether another
// attempt is worth making, and whether the .part file is still usable.
type downloadError struct {
	status int
	err    error
}

func (e *downloadError) Error() string { return e.err.Error() }
func (e *downloadError) Unwrap() error { return e.err }

// retryable reports whether another attempt could plausibly succeed. A 404 will
// still be a 404; a 503 or a reset connection may not be.
func (e *downloadError) retryable() bool {
	switch {
	case e.status == 0:
		return true // transport-level failure: DNS, reset, timeout, stall
	case e.status == http.StatusRequestTimeout, e.status == http.StatusTooManyRequests:
		return true
	default:
		return e.status >= 500
	}
}

func downloadTo(url, dest string, size int64, tr *tracker) error {
	for _, r := range plugin.Bus.Call("download.beforeStart", plugin.DownloadEvent{URL: url, Dest: dest}) {
		if cancel, _ := r.Value["cancel"].(bool); cancel {
			reason, _ := r.Value["reason"].(string)
			return fmt.Errorf("download canceled by plugin %s: %s", r.PluginID, reason)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if installCanceled.Load() {
		return ErrInstallCanceled
	}
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 && (size <= 0 || fi.Size() == size) {
		// The target is already complete, so any leftover .part is garbage from
		// an earlier interrupted run and can never be resumed into it.
		os.Remove(dest + ".part")
		if tr != nil {
			tr.fileDone(filepath.Base(dest))
		}
		return nil
	}
	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter. Without jitter every stream that
			// failed at the same moment would retry in lockstep.
			base := time.Duration(1<<uint(attempt-1)) * 250 * time.Millisecond
			time.Sleep(base + time.Duration(rand.Int64N(int64(base))))
		}
		lastErr = downloadOnce(url, dest, size, tr)
		if lastErr == nil {
			return nil
		}
		if errors.Is(lastErr, ErrInstallCanceled) {
			// Keep the .part file: the user may want to resume later.
			return lastErr
		}
		var de *downloadError
		if errors.As(lastErr, &de) && !de.retryable() {
			// Permanent failure - this partial file can never be completed.
			os.Remove(dest + ".part")
			return lastErr
		}
	}
	// Out of attempts. Keep the .part file so the next attempt (or the next
	// launch) resumes from it instead of restarting at byte zero.
	return lastErr
}

func downloadOnce(url, dest string, size int64, tr *tracker) error {
	ctx, cancel := context.WithCancel(currentInstallCtx())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	tmp := dest + ".part"
	var start int64
	if size > 0 {
		if fi, err := os.Stat(tmp); err == nil && fi.Size() > 0 && fi.Size() < size {
			start = fi.Size()
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
		}
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return &downloadError{err: fmt.Errorf("download %s: %w", url, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return &downloadError{status: resp.StatusCode, err: fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)}
	}
	appending := start > 0 && resp.StatusCode == http.StatusPartialContent
	if tr != nil && resp.ContentLength > 0 {
		total := resp.ContentLength
		if appending {
			total += start
		}
		tr.adoptContentLength(1, total)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appending {
		flags |= os.O_APPEND
	} else {
		// The server ignored our Range request, so the partial file is not a
		// valid prefix of what it is about to send. Start over.
		flags |= os.O_TRUNC
		start = 0
	}
	out, err := os.OpenFile(tmp, flags, 0o644)
	if err != nil {
		return &downloadError{err: err}
	}
	// The wrapper enforces cancellation and the idle timeout. It is applied even
	// when tr is nil, so callers that do not report progress still abort.
	src := newCountingReader(resp.Body, tr, filepath.Base(dest), size, readIdleTimeout, cancel)
	defer src.stop()
	if tr != nil && start > 0 {
		tr.addBytes(src.file, size, start)
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		if installCanceled.Load() || errors.Is(err, ErrInstallCanceled) {
			return ErrInstallCanceled
		}
		// Keep tmp: the next attempt resumes from it via Range.
		return &downloadError{err: fmt.Errorf("download %s: %w", url, err)}
	}
	out.Close()
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return &downloadError{err: err}
	}
	if tr != nil {
		tr.fileDone(filepath.Base(dest))
	}
	plugin.Bus.Emit("download.completed", plugin.DownloadEvent{URL: url, Dest: dest})
	return nil
}

func aggregateProgress(fn ProgressFn) (ProgressFn, func()) {
	if fn == nil {
		return nil, func() {}
	}
	var mu sync.Mutex
	states := map[string]Progress{}
	emit := func() {
		mu.Lock()
		var p Progress
		for _, s := range states {
			p.Current += s.Current
			p.Total += s.Total
			p.Bytes += s.Bytes
			p.TotalBytes += s.TotalBytes
			if s.Speed > p.Speed {
				p.Speed = s.Speed
			}
			p.Files = append(p.Files, s.Files...)
		}
		mu.Unlock()
		p.Phase = "clients"
		fn(p)
	}
	wrapped := func(s Progress) {
		mu.Lock()
		states[s.Phase] = s
		mu.Unlock()
		emit()
	}
	return wrapped, emit
}

// countingReader reports progress for a single download and guards against the
// two ways one can hang:
//
//   - the user cancelling - checked before each read, so we stop pulling bytes
//   - a server that stops sending - the idle timer cancels the request context,
//     which unblocks the read that is already in flight
//
// The cancel check alone is not enough: it only runs *between* reads, so a
// stalled body would block io.Copy forever and hold its concurrency slot.
type countingReader struct {
	r    io.Reader
	tr   *tracker // nil when the caller does not report progress
	file string
	size int64

	idle   time.Duration
	timer  *time.Timer
	cancel context.CancelFunc
}

func newCountingReader(r io.Reader, tr *tracker, file string, size int64, idle time.Duration, cancel context.CancelFunc) *countingReader {
	c := &countingReader{r: r, tr: tr, file: file, size: size, idle: idle, cancel: cancel}
	if idle > 0 && cancel != nil {
		c.timer = time.AfterFunc(idle, cancel)
	}
	return c
}

func (c *countingReader) Read(p []byte) (int, error) {
	if installCanceled.Load() {
		return 0, ErrInstallCanceled
	}
	if c.timer != nil {
		// Reset before (not after) the read: the whole point is to fire while
		// the read below is blocked.
		c.timer.Reset(c.idle)
	}
	n, err := c.r.Read(p)
	if n > 0 && c.tr != nil {
		c.tr.addBytes(c.file, c.size, int64(n))
	}
	return n, err
}

// stop releases the idle timer.
func (c *countingReader) stop() {
	if c.timer != nil {
		c.timer.Stop()
	}
}

func mavenPath(name string) string {
	parts := strings.Split(name, ":")
	group, artifact, version := parts[0], parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) > 3 && parts[3] != "" {
		file += "-" + parts[3]
	}
	return strings.Join([]string{strings.ReplaceAll(group, ".", "/"), artifact, version, file + ".jar"}, "/")
}

func libraryArtifact(lib Library) (Artifact, bool) {
	if lib.Downloads.Artifact.URL != "" || lib.Downloads.Artifact.Path != "" {
		return lib.Downloads.Artifact, true
	}
	path := mavenPath(lib.Name)
	base := lib.URL
	if base == "" {
		base = libraryBaseURL
	}
	return Artifact{Path: path, URL: strings.TrimSuffix(base, "/") + "/" + filepath.ToSlash(path)}, true
}

func libraryDest(a Artifact) string {
	return filepath.Join(librariesDir(), filepath.FromSlash(a.Path))
}

type job struct {
	url  string
	dest string
	size int64
	sha1 string
}

func modernNativesClassifier() string {
	return ":natives-" + osName()
}

func modernNativesClassifiers() []string {
	base := osName()
	if base == "osx" {
		return []string{":natives-macos", ":natives-macos-arm64", ":natives-osx", ":natives-osx-arm64", ":natives-" + base}
	}
	if base == "linux" {
		return []string{":natives-linux", ":natives-" + base}
	}
	return []string{":natives-" + base}
}

func isModernNatives(lib Library) bool {
	return strings.Contains(lib.Name, ":natives-")
}

func matchesModernNatives(name string) bool {
	for _, c := range modernNativesClassifiers() {
		if strings.HasSuffix(name, c) {
			return true
		}
	}
	return false
}

func InstallLibraries(v *VersionMeta, onProgress ProgressFn) (classpath []string, natives []string, err error) {
	var (
		jobs       []job
		nativeJars []Artifact
		totalBytes int64
	)
	for _, lib := range v.Libraries {
		if !rulesAllow(lib.Rules) {
			continue
		}
		if isModernNatives(lib) {
			if matchesModernNatives(lib.Name) {
				if art, ok := libraryArtifact(lib); ok {
					dest := libraryDest(art)
					nativeJars = append(nativeJars, art)
					jobs = append(jobs, job{art.URL, dest, art.Size, art.SHA1})
					totalBytes += art.Size
				}
			}
			continue
		}
		if art, ok := libraryArtifact(lib); ok {
			dest := libraryDest(art)
			classpath = append(classpath, dest)
			jobs = append(jobs, job{art.URL, dest, art.Size, art.SHA1})
			totalBytes += art.Size
		}
		for classifier, nat := range lib.Downloads.Classifiers {
			nc, hasNative := lib.Natives[osName()]
			if hasNative && classifier == nc {
				dest := libraryDest(nat)
				nativeJars = append(nativeJars, nat)
				jobs = append(jobs, job{nat.URL, dest, nat.Size, nat.SHA1})
				totalBytes += nat.Size
			}
		}
	}
	tr := newTracker("libraries", onProgress)
	tr.setTotal(int64(len(jobs)), totalBytes)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	if firstErr != nil {
		return nil, nil, firstErr
	}
	ndir := nativesDir(v.ID)
	for _, nat := range nativeJars {
		if err := extractZip(libraryDest(nat), ndir); err != nil {
			return nil, nil, fmt.Errorf("extract natives %s: %w", filepath.Base(nat.Path), err)
		}
	}
	tr.emit()
	return classpath, natives, nil
}

// workItem is a job together with the result of the cheap pre-pass. A file that
// is already on disk still needs verifying, but it must not occupy a download
// slot just to be hashed.
type workItem struct {
	j      job
	cached bool
}

func runJobs(jobs []job, tr *tracker, firstErr *error) {
	// Drop duplicates: several libraries can resolve to the same path.
	seen := make(map[string]bool, len(jobs))
	uniq := jobs[:0]
	for _, j := range jobs {
		if seen[j.dest] {
			continue
		}
		seen[j.dest] = true
		uniq = append(uniq, j)
	}
	jobs = uniq

	var mu sync.Mutex
	limit := GetDownloadConcurrency()
	auto := limit <= 0
	if auto {
		limit = 8
	}
	sem := newDynamicSem(limit)

	done := make(chan struct{})
	if auto {
		go scaleConcurrency(sem, tr, done)
	} else {
		// Without this the manual slider would only apply to the next batch.
		go watchConcurrency(sem, done)
	}

	// A bounded pool instead of one goroutine per file. An asset index carries
	// thousands of objects, and previously the semaphore was the only thing
	// keeping that many live goroutines in check.
	work := make(chan workItem)
	var wg sync.WaitGroup
	workers := maxConcurrency
	if n := len(jobs); n < workers {
		workers = n
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				runJob(item, sem, tr, &mu, firstErr)
			}
		}()
	}
	for _, j := range jobs {
		cached := false
		if fi, err := os.Stat(j.dest); err == nil && fi.Size() > 0 && (j.size <= 0 || fi.Size() == j.size) {
			cached = true
			tr.creditBytes(j.size)
		}
		work <- workItem{j: j, cached: cached}
	}
	close(work)
	wg.Wait()
	close(done)
}

// runJob downloads one file and verifies it. The concurrency semaphore is held
// only around the network phase - hashing is disk-bound, and holding a download
// slot while reading a file back would starve workers that still have bytes to
// pull.
func runJob(item workItem, sem *dynamicSem, tr *tracker, mu *sync.Mutex, firstErr *error) {
	j := item.j
	fail := func(err error) {
		mu.Lock()
		if *firstErr == nil {
			*firstErr = err
		}
		mu.Unlock()
	}

	if !item.cached {
		if installCanceled.Load() {
			fail(ErrInstallCanceled)
			return
		}
		sem.acquire()
		err := downloadTo(j.url, j.dest, j.size, tr)
		sem.release()
		if err != nil {
			fail(err)
			return
		}
	}
	if j.sha1 != "" {
		if h, err := sha1File(j.dest); err != nil || h != j.sha1 {
			os.Remove(j.dest)
			fail(fmt.Errorf("sha1 mismatch for %s", filepath.Base(j.dest)))
			return
		}
	}
	if item.cached && tr != nil {
		tr.fileDone(filepath.Base(j.dest))
	}
}

// watchConcurrency keeps a manual concurrency setting live for the duration of
// a batch, so moving the slider affects the running download rather than only
// the next one.
func watchConcurrency(sem *dynamicSem, done chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			want := GetDownloadConcurrency()
			if want < 1 {
				want = 1
			}
			if want != sem.currentLimit() {
				sem.setLimit(want)
			}
		}
	}
}

// scaleConcurrency tunes download parallelism while a batch runs, the way
// congestion control does: grow while throughput genuinely improves, shrink when
// it degrades, and hold on the plateau.
//
// The plateau is the case that matters. Once the link is saturated, more
// connections add queueing delay but do not reduce throughput. Comparing each
// sample against the previous one - as this used to - makes a flat plateau look
// like an improvement in roughly half of all samples, so the limit ratchets to
// the ceiling and never backs off. Comparing against a drifting reference, and
// requiring a real gain before growing, settles at the plateau instead.
func scaleConcurrency(sem *dynamicSem, tr *tracker, done chan struct{}) {
	if tr == nil {
		return
	}
	const (
		sampleEvery = 750 * time.Millisecond
		growAbove   = 1.10 // need a genuine improvement before adding connections
		shrinkBelow = 0.90
	)
	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()
	var ref int64
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			s := tr.currentSpeed()
			if s <= 0 {
				continue // no fresh measurement - nothing to act on
			}
			if ref == 0 {
				ref = s
				continue
			}
			lim := sem.currentLimit()
			switch {
			case s > int64(float64(ref)*growAbove) && lim < maxConcurrency:
				sem.setLimit(lim + 2)
				ref = s
			case s < int64(float64(ref)*shrinkBelow) && lim > 1:
				sem.setLimit(lim / 2)
				ref = s
			default:
				// Plateau or noise: hold the limit and let the reference drift
				// towards the current rate, so a real slowdown is still noticed
				// after a long stable period.
				ref = ref*3/4 + s/4
			}
		}
	}
}

func safeExtractPath(dir, rel string) (string, bool) {
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
		return "", false
	}
	return target, true
}

func extractZip(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		out := filepath.Join(dest, filepath.Base(f.Name))
		if _, err := os.Stat(out); err == nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(out)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(w, rc); err != nil {
			rc.Close()
			w.Close()
			return err
		}
		rc.Close()
		w.Close()
	}
	return nil
}

func InstallAssets(v *VersionMeta, onProgress ProgressFn) error {
	if v.AssetIndex.URL == "" {
		return fmt.Errorf("version %s has no asset index", v.ID)
	}
	indexes := filepath.Join(assetsDir(), "indexes")
	os.MkdirAll(indexes, 0o755)
	indexFile := filepath.Join(indexes, v.AssetIndex.ID+".json")
	var idx AssetIndex
	if data, err := os.ReadFile(indexFile); err == nil && json.Unmarshal(data, &idx) == nil {
	} else {
		resp, err := downloadClient.Get(v.AssetIndex.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("asset index: HTTP %d", resp.StatusCode)
		}
		out, err := os.Create(indexFile)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, resp.Body); err != nil {
			out.Close()
			return err
		}
		out.Close()
		if data, err := os.ReadFile(indexFile); err != nil {
			return err
		} else if err := json.Unmarshal(data, &idx); err != nil {
			return err
		}
	}

	jobs := make([]job, 0, len(idx.Objects))
	var totalBytes int64
	for _, o := range idx.Objects {
		if len(o.Hash) < 2 {
			continue
		}
		jobs = append(jobs, job{
			url:  fmt.Sprintf("%s/%s/%s", resourceURL, o.Hash[:2], o.Hash),
			dest: filepath.Join(assetsDir(), "objects", o.Hash[:2], o.Hash),
			size: o.Size,
			sha1: o.Hash,
		})
		totalBytes += o.Size
	}
	tr := newTracker("assets", onProgress)
	tr.setTotal(int64(len(jobs)), totalBytes)
	var firstErr error
	runJobs(jobs, tr, &firstErr)
	if firstErr != nil {
		return firstErr
	}
	tr.emit()
	return nil
}

func InstallClientJar(v *VersionMeta, onProgress ProgressFn) error {
	dest := clientJarPath(v.ID)
	tr := newTracker("client", onProgress)
	tr.setTotal(1, v.Downloads.Client.Size)
	if fi, err := os.Stat(dest); err == nil && fi.Size() == v.Downloads.Client.Size {
		tr.fileDone(filepath.Base(dest))
		tr.emit()
		return nil
	}
	if err := downloadTo(v.Downloads.Client.URL, dest, v.Downloads.Client.Size, tr); err != nil {
		return err
	}
	if v.Downloads.Client.SHA1 != "" {
		if h, err := sha1File(dest); err != nil || h != v.Downloads.Client.SHA1 {
			os.Remove(dest)
			return fmt.Errorf("client jar sha1 mismatch for %s", v.ID)
		}
	}
	tr.emit()
	return nil
}

func EnsureInstalled(version string, onProgress ProgressFn) (*VersionMeta, []string, error) {
	v, err := GetVersionMeta(version)
	if err != nil {
		return nil, nil, err
	}
	if err := InstallClientJar(v, onProgress); err != nil {
		return nil, nil, err
	}
	classpath, _, err := InstallLibraries(v, onProgress)
	if err != nil {
		return nil, nil, err
	}
	if err := InstallAssets(v, onProgress); err != nil {
		return nil, nil, err
	}
	return v, classpath, nil
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
