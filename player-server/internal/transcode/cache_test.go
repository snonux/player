package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

// fakeRunner is a Runner that writes a fixed payload instead of running
// ffmpeg. started receives one value per invocation; when release is non-nil
// the runner blocks until it is closed (or ctx is cancelled). hook, when set,
// runs just before the output is written.
type fakeRunner struct {
	calls   atomic.Int32
	payload string
	err     error
	started chan struct{}
	release chan struct{}
	hook    func(ctx context.Context, src Source)

	mu      sync.Mutex
	outputs []string
}

func (f *fakeRunner) Transcode(ctx context.Context, job Job) error {
	src, outputPath := job.Source, job.Output
	f.calls.Add(1)
	f.mu.Lock()
	f.outputs = append(f.outputs, outputPath)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.hook != nil {
		f.hook(ctx, src)
	}
	if f.err != nil {
		// Leave a partial file behind, like a crashed ffmpeg would.
		_ = os.WriteFile(outputPath, []byte("partial"), 0o644)
		return f.err
	}
	return os.WriteFile(outputPath, []byte(f.payload), 0o644)
}

// blocking returns a runner whose jobs wait until release is closed.
func blocking() *fakeRunner {
	return &fakeRunner{payload: "x", started: make(chan struct{}, 16), release: make(chan struct{})}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// plentyOfSpace is a FreeSpace stub, so tests do not depend on the machine's
// actual free disk space.
func plentyOfSpace(string) (int64, error) { return math.MaxInt64 / 2, nil }

// settleTimeout is how long tests wait for background jobs.
const settleTimeout = 10 * time.Second

// newSource creates a small source file for media id. Its mtime lies an hour
// back, so the cache does not treat it as "still being written".
func newSource(t *testing.T, id int64) Source {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clip.avi")
	if err := os.WriteFile(path, []byte("source"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	age(t, path, time.Hour)
	return Source{MediaID: id, Path: path, Kind: KindVideo}
}

// age sets a file's mtime to d ago.
func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	at := time.Now().Add(-d)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// stop closes a cache and waits for its jobs.
func stop(c *Cache) {
	c.Close()
	c.WaitTimeout(settleTimeout)
}

// newTestCache builds a cache in a temp dir, a mock clock set to now, and a
// source for media 7. The clock must only be advanced while no job runs.
func newTestCache(t *testing.T, runner Runner, opts Options) (*Cache, Source, *clock.MockClock) {
	t.Helper()
	if opts.Dir == "" {
		// A not-yet-existing subdirectory proves the cache creates it.
		opts.Dir = filepath.Join(t.TempDir(), "cache")
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 1 << 20
	}
	if opts.FreeSpace == nil {
		opts.FreeSpace = plentyOfSpace
	}
	clk := &clock.MockClock{T: time.Now()}
	c := NewCache(context.Background(), runner, clk, quietLogger(), opts)
	t.Cleanup(func() { stop(c) })
	return c, newSource(t, 7), clk
}

// waitIdle blocks until no job is in flight, so tests can assert on the
// final on-disk state without sleeping.
func waitIdle(t *testing.T, c *Cache) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		n := len(c.jobs)
		c.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("transcode jobs did not finish")
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// wantNames asserts the exact content of the cache directory.
func wantNames(t *testing.T, dir string, want ...string) {
	t.Helper()
	got := dirNames(t, dir)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("cache dir = %v, want %v", got, want)
	}
}

func TestCache_EnsureProducesAndReuses(t *testing.T) {
	runner := &fakeRunner{payload: "rendition"}
	c, src, _ := newTestCache(t, runner, Options{})

	first, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	got, err := os.ReadFile(first.Path)
	if err != nil || string(got) != "rendition" {
		t.Fatalf("rendition content = %q, %v", got, err)
	}
	if first.ContentType != "video/mp4" || filepath.Ext(first.Path) != ".mp4" {
		t.Errorf("unexpected rendition %+v", first)
	}
	if !renditionRe.MatchString(filepath.Base(first.Path)) {
		t.Errorf("rendition name %q does not match the cache's own pattern", filepath.Base(first.Path))
	}

	second, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if runner.calls.Load() != 1 {
		t.Errorf("runner calls = %d, want 1 (second request must hit the cache)", runner.calls.Load())
	}
	if second.Path != first.Path || second.ETag != first.ETag || first.ETag == "" {
		t.Errorf("rendition changed between requests: %+v vs %+v", first, second)
	}
	wantNames(t, c.opts.Dir, filepath.Base(first.Path))
}

func TestCache_AudioKind(t *testing.T) {
	c, src, _ := newTestCache(t, &fakeRunner{payload: "a"}, Options{})
	src.Kind = KindAudio
	r, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if r.ContentType != "audio/mp4" || filepath.Ext(r.Path) != ".m4a" {
		t.Errorf("unexpected audio rendition %+v", r)
	}
}

// A rendition that was evicted and produced again is a different encode and
// must not share an ETag with the old one (If-Range would splice them).
func TestCache_ETagChangesWhenRenditionIsRebuilt(t *testing.T) {
	c, src, _ := newTestCache(t, &fakeRunner{payload: "x"}, Options{})
	first, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond) // distinct publication time
	second, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure after eviction: %v", err)
	}
	if second.Path != first.Path || second.ETag == first.ETag {
		t.Errorf("rebuilt rendition: path %q/%q etag %q/%q", first.Path, second.Path, first.ETag, second.ETag)
	}
}

func TestCache_DeduplicatesConcurrentRequests(t *testing.T) {
	runner := blocking()
	c, src, _ := newTestCache(t, runner, Options{})

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = c.Ensure(context.Background(), src)
		}()
	}
	<-runner.started
	close(runner.release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("request %d: %v", i, err)
		}
	}
	if runner.calls.Load() != 1 {
		t.Errorf("runner calls = %d, want 1", runner.calls.Load())
	}
}

func TestCache_FailureBacksOff(t *testing.T) {
	boom := errors.New("ffmpeg exploded")
	runner := &fakeRunner{err: boom}
	c, src, clk := newTestCache(t, runner, Options{})
	ensure := func() error {
		_, err := c.Ensure(context.Background(), src)
		waitIdle(t, c)
		return err
	}

	if err := ensure(); !errors.Is(err, boom) {
		t.Fatalf("first attempt = %v, want %v", err, boom)
	}
	wantNames(t, c.opts.Dir) // no partial or temporary file left behind

	// Requests during the backoff do not start ffmpeg again.
	for range 3 {
		if err := ensure(); !errors.Is(err, ErrFailedRecently) {
			t.Fatalf("during backoff = %v, want ErrFailedRecently", err)
		}
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1 during backoff", runner.calls.Load())
	}

	// After the first backoff (1 min) one retry runs; it fails again and
	// the pause doubles.
	clk.T = clk.T.Add(failureBackoff + time.Second)
	if err := ensure(); !errors.Is(err, boom) {
		t.Fatalf("retry = %v, want %v", err, boom)
	}
	clk.T = clk.T.Add(failureBackoff + time.Second)
	if err := ensure(); !errors.Is(err, ErrFailedRecently) {
		t.Fatalf("second backoff must be longer, got %v", err)
	}

	// A later success clears the entry.
	clk.T = clk.T.Add(failureBackoff)
	runner.err, runner.payload = nil, "ok"
	if err := ensure(); err != nil {
		t.Fatalf("retry after second backoff: %v", err)
	}
	if runner.calls.Load() != 3 {
		t.Errorf("runner calls = %d, want 3", runner.calls.Load())
	}
}

func TestCache_BackoffIsCapped(t *testing.T) {
	c, _, _ := newTestCache(t, &fakeRunner{}, Options{})
	var last time.Duration
	for range 12 {
		last = c.recordFailure(1, failure{err: ErrFailedRecently}, 0)
	}
	if last != maxFailureBackoff {
		t.Errorf("backoff after many failures = %s, want cap %s", last, maxFailureBackoff)
	}
}

// The pause doubles per version of the source; the first failure of a new
// version starts over, also after a "source still changing" entry.
func TestCache_FailureCountRestartsForNewSourceVersion(t *testing.T) {
	c, _, _ := newTestCache(t, &fakeRunner{}, Options{})
	steps := []struct {
		f    failure
		want time.Duration
	}{
		{failure{err: ErrFailedRecently, name: "v1"}, failureBackoff},
		{failure{err: ErrFailedRecently, name: "v1"}, 2 * failureBackoff},
		{failure{err: ErrFailedRecently, name: "v2"}, failureBackoff},
		{failure{err: ErrSourceChanged}, failureBackoff},
		{failure{err: ErrSourceChanged}, 2 * failureBackoff},
		{failure{err: ErrFailedRecently, name: "v3"}, failureBackoff},
	}
	for i, st := range steps {
		if got := c.recordFailure(1, st.f, 0); got != st.want {
			t.Errorf("step %d (%q): pause = %s, want %s", i, st.f.name, got, st.want)
		}
	}
}

func TestCache_EmptyOutputIsFailure(t *testing.T) {
	c, src, _ := newTestCache(t, &fakeRunner{payload: ""}, Options{})
	if _, err := c.Ensure(context.Background(), src); err == nil {
		t.Fatal("expected error for empty rendition")
	}
	waitIdle(t, c)
	wantNames(t, c.opts.Dir)
}

func TestCache_PanickingRunnerIsReported(t *testing.T) {
	runner := &fakeRunner{payload: "x", hook: func(context.Context, Source) { panic("runner bug") }}
	c, src, _ := newTestCache(t, runner, Options{})
	_, err := c.Ensure(context.Background(), src)
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("Ensure error = %v, want panic report", err)
	}
	waitIdle(t, c)
	wantNames(t, c.opts.Dir)
}

func TestCache_CancelledRequestKeepsTranscoding(t *testing.T) {
	runner := blocking()
	c, src, _ := newTestCache(t, runner, Options{})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Ensure(ctx, src)
		errCh <- err
	}()
	<-runner.started
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Ensure error = %v, want context.Canceled", err)
	}

	// The job survives the request; once it finishes the rendition is
	// served from cache without a second transcode.
	close(runner.release)
	waitIdle(t, c)
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("Ensure after cancelled request: %v", err)
	}
	if runner.calls.Load() != 1 {
		t.Errorf("runner calls = %d, want 1", runner.calls.Load())
	}
}

func TestCache_PendingAfterWaitLimit(t *testing.T) {
	runner := blocking()
	c, src, _ := newTestCache(t, runner, Options{WaitLimit: 10 * time.Millisecond})

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrPending) {
		t.Fatalf("Ensure error = %v, want ErrPending", err)
	}
	// A retry while the job is still running joins it instead of starting
	// a second ffmpeg.
	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrPending) {
		t.Fatalf("retry error = %v, want ErrPending", err)
	}
	close(runner.release)
	waitIdle(t, c)
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("Ensure after job finished: %v", err)
	}
	if runner.calls.Load() != 1 {
		t.Errorf("runner calls = %d, want 1", runner.calls.Load())
	}
}

// isContextError reports whether err could be mistaken for the caller's own
// request being cancelled.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func TestCache_ShutdownAbortsJobs(t *testing.T) {
	runner := blocking()
	base, shutdown := context.WithCancel(context.Background())
	dir := t.TempDir()
	c := NewCache(base, runner, clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: 1 << 20, FreeSpace: plentyOfSpace})
	src := newSource(t, 1)

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Ensure(context.Background(), src)
		errCh <- err
	}()
	<-runner.started
	shutdown()
	if !c.WaitTimeout(settleTimeout) { // true only when the job goroutine is gone
		t.Fatal("job did not stop on shutdown")
	}

	err := <-errCh
	if !errors.Is(err, ErrAborted) || isContextError(err) {
		t.Fatalf("Ensure error = %v, want ErrAborted that is not a context error", err)
	}
	wantNames(t, dir) // temporary file cleaned up
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.failures) != 0 {
		t.Errorf("shutdown must not be remembered as a failure: %v", c.failures)
	}
}

func TestCache_JobTimeout(t *testing.T) {
	runner := blocking() // never released: only the timeout ends it
	c, src, clk := newTestCache(t, runner, Options{JobTimeout: 20 * time.Millisecond})
	ensure := func() error {
		_, err := c.Ensure(context.Background(), src)
		waitIdle(t, c)
		return err
	}

	if err := ensure(); err == nil || !strings.Contains(err.Error(), "timed out") || isContextError(err) {
		t.Fatalf("Ensure error = %v, want a timeout that is not a context error", err)
	}
	wantNames(t, c.opts.Dir)

	// A timed-out job burned its whole budget and would do so again, so it
	// is not retried after the ordinary 30 minute cap but a day later.
	clk.T = clk.T.Add(2 * maxFailureBackoff)
	if err := ensure(); !errors.Is(err, ErrFailedRecently) {
		t.Fatalf("an hour after a timeout = %v, want ErrFailedRecently", err)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls.Load())
	}

	// ... unless the source was replaced: the new file gets its chance.
	rewrite(t, src.Path)
	if err := ensure(); errors.Is(err, ErrFailedRecently) {
		t.Fatalf("replaced source is still refused: %v", err)
	}
	if runner.calls.Load() != 2 {
		t.Errorf("runner calls = %d, want 2", runner.calls.Load())
	}
}

func TestCache_TimeoutRetriedAfterADay(t *testing.T) {
	runner := blocking()
	c, src, clk := newTestCache(t, runner, Options{JobTimeout: 20 * time.Millisecond})
	_, _ = c.Ensure(context.Background(), src)
	waitIdle(t, c)
	clk.T = clk.T.Add(longBackoff + time.Minute)
	_, _ = c.Ensure(context.Background(), src)
	waitIdle(t, c)
	if runner.calls.Load() != 2 {
		t.Errorf("runner calls = %d, want a second attempt after %s", runner.calls.Load(), longBackoff)
	}
}

// The job timeout must cover working time only. The first job holds the only
// slot for three times the timeout (it ignores its context, so it really
// stays that long); the second job queues behind it for longer than the
// timeout. When it finally runs, its context must still be alive. If the
// timeout were started before the slot is acquired, the second job would be
// handed an already expired context and fail.
func TestCache_QueueTimeDoesNotCountAgainstTimeout(t *testing.T) {
	const timeout = 50 * time.Millisecond
	var first atomic.Bool
	var queuedCtxErr error
	runner := &fakeRunner{payload: "x", started: make(chan struct{}, 2)}
	runner.hook = func(ctx context.Context, _ Source) {
		if first.CompareAndSwap(false, true) {
			time.Sleep(3 * timeout)
			return
		}
		queuedCtxErr = ctx.Err()
	}
	c, src, _ := newTestCache(t, runner, Options{MaxConcurrent: 1, JobTimeout: timeout, WaitLimit: time.Millisecond})

	_, _ = c.Ensure(context.Background(), src)
	<-runner.started // the first job owns the slot
	queued := newSource(t, 8)
	_, _ = c.Ensure(context.Background(), queued) // queues behind it
	waitIdle(t, c)

	if queuedCtxErr != nil {
		t.Fatalf("queued job started with an expired context: %v", queuedCtxErr)
	}
	if _, err := c.Ensure(context.Background(), queued); err != nil {
		t.Errorf("queued job must succeed after waiting longer than the timeout: %v", err)
	}
}

func TestCache_LimitsInFlightJobs(t *testing.T) {
	runner := blocking()
	c, _, _ := newTestCache(t, runner, Options{WaitLimit: time.Millisecond, MaxConcurrent: 1, MaxInFlight: 3, MaxPerRequester: 2})
	ensure := func(id int64, requester string) error {
		src := newSource(t, id)
		src.Requester = requester
		_, err := c.Ensure(context.Background(), src)
		return err
	}

	// alice may hold two jobs (one running, one queued), not a third.
	for id := int64(1); id <= 2; id++ {
		if err := ensure(id, "user:alice"); !errors.Is(err, ErrPending) {
			t.Fatalf("alice job %d = %v, want ErrPending", id, err)
		}
	}
	<-runner.started // the first job holds the only slot
	if err := ensure(3, "user:alice"); !errors.Is(err, ErrBusy) {
		t.Fatalf("alice third job = %v, want ErrBusy", err)
	}
	// bob still gets in, which fills the queue for everyone.
	if err := ensure(4, "user:bob"); !errors.Is(err, ErrPending) {
		t.Fatalf("bob job = %v, want ErrPending", err)
	}
	if err := ensure(5, "user:carol"); !errors.Is(err, ErrBusy) {
		t.Fatalf("job beyond MaxInFlight = %v, want ErrBusy", err)
	}
	if got := runner.calls.Load(); got != 1 {
		t.Errorf("running ffmpeg jobs = %d, want 1 (MaxConcurrent)", got)
	}

	close(runner.release)
	waitIdle(t, c)
	if err := ensure(5, "user:carol"); err != nil {
		t.Errorf("after the queue drained: %v", err)
	}
}

func TestCache_SourceMissing(t *testing.T) {
	c, src, _ := newTestCache(t, &fakeRunner{payload: "x"}, Options{})
	for _, path := range []string{filepath.Join(t.TempDir(), "gone.avi"), t.TempDir()} {
		src.Path = path
		_, err := c.Ensure(context.Background(), src)
		if !errors.Is(err, ErrSourceMissing) {
			t.Fatalf("Ensure(%s) error = %v, want ErrSourceMissing", path, err)
		}
		// The error travels towards the client; the path stays in the log.
		if strings.Contains(err.Error(), path) {
			t.Errorf("error leaks the source path: %v", err)
		}
	}
}

func TestCache_UnwritableDir(t *testing.T) {
	// A regular file where the cache directory should be makes MkdirAll
	// fail, like a read-only root filesystem does.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c, src, _ := newTestCache(t, &fakeRunner{payload: "x"}, Options{Dir: filepath.Join(blocker, "cache")})
	if _, err := c.Ensure(context.Background(), src); err == nil {
		t.Fatal("expected error for unwritable cache dir")
	}
	if err := c.Preflight(); err == nil {
		t.Error("Preflight must report the unwritable cache dir")
	}
}

func TestCache_Preflight(t *testing.T) {
	c, _, _ := newTestCache(t, &fakeRunner{}, Options{})
	if err := c.Preflight(); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	wantNames(t, c.opts.Dir) // directory created, probe file removed
}

func TestCache_DiskFull(t *testing.T) {
	free := int64(100)
	runner := &fakeRunner{payload: "x"}
	c, src, _ := newTestCache(t, runner, Options{
		MinFreeBytes: 1000,
		FreeSpace:    func(string) (int64, error) { return free, nil },
	})

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("Ensure error = %v, want ErrNoSpace", err)
	}
	waitIdle(t, c)
	if runner.calls.Load() != 0 {
		t.Error("ffmpeg must not start when the volume is below its reserve")
	}
	wantNames(t, c.opts.Dir)

	// Not remembered as a failure: it works as soon as space is back.
	free = 1 << 30
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("Ensure with free space: %v", err)
	}
}

func TestCache_FreeSpaceCheckError(t *testing.T) {
	c, src, _ := newTestCache(t, &fakeRunner{payload: "x"}, Options{
		FreeSpace: func(string) (int64, error) { return 0, errors.New("statfs failed") },
	})
	if _, err := c.Ensure(context.Background(), src); err == nil {
		t.Fatal("expected error when free space cannot be determined")
	}
}

func TestFreeSpace_RealVolume(t *testing.T) {
	free, err := freeSpace(t.TempDir())
	if err != nil || free <= 0 {
		t.Errorf("freeSpace = %d, %v", free, err)
	}
	if _, err := freeSpace(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("expected error for a missing directory")
	}
}

// rewrite replaces a source file with different content and mtime.
func rewrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("a different source"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Old enough to count as settled, different from newSource's mtime.
	age(t, path, 30*time.Minute)
}

func TestCache_InvalidatesWhenSourceChanges(t *testing.T) {
	runner := &fakeRunner{payload: "one"}
	c, src, _ := newTestCache(t, runner, Options{})

	first, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitIdle(t, c)
	rewrite(t, src.Path)
	runner.payload = "two"

	second, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure after change: %v", err)
	}
	if second.Path == first.Path || second.ETag == first.ETag {
		t.Fatal("changed source must yield a new rendition and ETag")
	}
	if got, _ := os.ReadFile(second.Path); string(got) != "two" {
		t.Errorf("new rendition content = %q", got)
	}
	waitIdle(t, c)
	wantNames(t, c.opts.Dir, filepath.Base(second.Path)) // stale one removed
}

// A source that is still being written (upload, copy) while ffmpeg reads it
// must not end up as a rendition of the truncated file, and polling clients
// must not start one ffmpeg run per request while the copy goes on.
func TestCache_SourceChangedDuringTranscode(t *testing.T) {
	runner := &fakeRunner{payload: "x"}
	c, src, clk := newTestCache(t, runner, Options{})
	runner.hook = func(_ context.Context, s Source) { rewrite(t, s.Path) }
	ensure := func() error {
		_, err := c.Ensure(context.Background(), src)
		waitIdle(t, c)
		return err
	}

	if err := ensure(); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("Ensure error = %v, want ErrSourceChanged", err)
	}
	wantNames(t, c.opts.Dir)

	// The file has a new name now; the backoff is per media item and holds.
	runner.hook = nil
	for range 3 {
		if err := ensure(); !errors.Is(err, ErrSourceChanged) {
			t.Fatalf("poll during backoff = %v, want ErrSourceChanged", err)
		}
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1 during the backoff", runner.calls.Load())
	}
	clk.T = clk.T.Add(failureBackoff + time.Second)
	if err := ensure(); err != nil {
		t.Fatalf("retry after backoff: %v", err)
	}
}

// A file modified seconds ago is probably still being uploaded: no ffmpeg
// run is started for it until it has settled.
func TestCache_WaitsForSourceToSettle(t *testing.T) {
	runner := &fakeRunner{payload: "x"}
	c, src, clk := newTestCache(t, runner, Options{})
	age(t, src.Path, 2*time.Second) // written two seconds ago

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("fresh source = %v, want ErrSourceChanged", err)
	}
	if runner.calls.Load() != 0 {
		t.Fatal("ffmpeg started on a file that was just modified")
	}
	clk.T = clk.T.Add(DefaultSourceSettle + time.Second)
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("settled source: %v", err)
	}
	// Once a rendition exists it is served even right after a new stat.
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("cached rendition: %v", err)
	}
}

// ffmpeg's "-fs" stops the output at the allowed size and exits successfully;
// such a truncated file must never be published. When the cap was the cache
// budget the rendition can never fit: that is remembered, so repeated
// requests (or anonymous HEAD probes) do not write the budget's worth of
// data again each time — and it costs the other renditions nothing.
func TestCache_RenditionOverBudgetIsRememberedAndEvictsNothing(t *testing.T) {
	var gotCap int64
	runner := &capRunner{payload: "0123456789", gotCap: &gotCap}
	c, src, clk := newTestCache(t, runner, Options{Dir: t.TempDir(), MaxBytes: 10})
	// Unrelated renditions filling the budget; none of them may be evicted
	// for a job that cannot succeed.
	writeCached(t, c.opts.Dir, "m1-10-1-v1.mp4", 4, time.Now().Add(-3*time.Hour))
	writeCached(t, c.opts.Dir, "m2-10-1-v1.mp4", 4, time.Now().Add(-2*time.Hour))
	ensure := func() error {
		_, err := c.Ensure(context.Background(), src)
		waitIdle(t, c)
		return err
	}

	for range 3 {
		if err := ensure(); !errors.Is(err, ErrTooLarge) || errors.Is(err, ErrNoSpace) {
			t.Fatalf("Ensure error = %v, want ErrTooLarge", err)
		}
	}
	if runner.calls != 1 || gotCap != 10 {
		t.Errorf("runner ran %d times with cap %d, want once with the budget 10", runner.calls, gotCap)
	}
	wantNames(t, c.opts.Dir, "m1-10-1-v1.mp4", "m2-10-1-v1.mp4")

	// Still remembered hours later, but a replaced source is tried at once.
	clk.T = clk.T.Add(2 * maxFailureBackoff)
	if err := ensure(); !errors.Is(err, ErrTooLarge) || runner.calls != 1 {
		t.Fatalf("an hour later: %v after %d runs, want ErrTooLarge without a new run", err, runner.calls)
	}
	rewrite(t, src.Path)
	_ = ensure()
	if runner.calls != 2 {
		t.Errorf("runner ran %d times, want a new attempt for the replaced source", runner.calls)
	}
}

// A source many times larger than the whole budget is refused before ffmpeg
// starts: no rendition of it could plausibly fit.
func TestCache_ImplausiblyLargeSourceIsRefusedUpFront(t *testing.T) {
	runner := &capRunner{payload: "x", gotCap: new(int64)}
	c, src, _ := newTestCache(t, runner, Options{Dir: t.TempDir(), MaxBytes: 10})
	writeCached(t, c.opts.Dir, "m1-10-1-v1.mp4", 9, time.Now().Add(-time.Hour))
	if err := os.WriteFile(src.Path, make([]byte, 10*implausibleShrink+implausibleShrink), 0o644); err != nil {
		t.Fatal(err)
	}
	age(t, src.Path, time.Hour)

	for range 2 {
		if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("Ensure error = %v, want ErrTooLarge", err)
		}
		waitIdle(t, c)
	}
	if runner.calls != 0 {
		t.Errorf("ffmpeg ran %d times for a source that can never fit", runner.calls)
	}
	wantNames(t, c.opts.Dir, "m1-10-1-v1.mp4")

	// Just inside the limit it is given its one try.
	if err := os.WriteFile(src.Path, make([]byte, 10*implausibleShrink), 0o644); err != nil {
		t.Fatal(err)
	}
	age(t, src.Path, 2*time.Hour)
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("source within the plausible range: %v", err)
	}
}

// When the volume's free space, not the budget, set the cap, hitting it means
// "disk full right now". That heals when space is freed and is not remembered.
func TestCache_CapFromFreeSpaceIsNoSpaceAndNotRemembered(t *testing.T) {
	var gotCap int64
	runner := &capRunner{payload: "0123456789", gotCap: &gotCap}
	free := int64(1005)
	c, src, _ := newTestCache(t, runner, Options{
		MaxBytes: 1 << 20, MinFreeBytes: 1000,
		FreeSpace: func(string) (int64, error) { return free, nil },
	})
	for range 2 {
		if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrNoSpace) || errors.Is(err, ErrTooLarge) {
			t.Fatalf("Ensure error = %v, want ErrNoSpace", err)
		}
		waitIdle(t, c)
	}
	if runner.calls != 2 || gotCap != 5 {
		t.Errorf("runner ran %d times with cap %d, want twice (not remembered) with cap 5", runner.calls, gotCap)
	}
	free = 5000
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("after space was freed: %v", err)
	}
}

// Old renditions are evicted for a new job only when the volume really lacks
// the space, and then only as much as the job is assumed to need.
func TestCache_EvictsForAJobOnlyWhenTheVolumeIsShort(t *testing.T) {
	runner := &capRunner{payload: "x", gotCap: new(int64)}
	var dir string
	used := func() (n int64) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				n += info.Size()
			}
		}
		return n
	}
	// A 100 byte volume holding nothing but the cache.
	c, src, _ := newTestCache(t, runner, Options{
		Dir: t.TempDir(), MaxBytes: 1 << 20, MinFreeBytes: 1,
		FreeSpace: func(string) (int64, error) { return 100 - used(), nil },
	})
	dir = c.opts.Dir
	writeCached(t, dir, "m1-10-1-v1.mp4", 50, time.Now().Add(-3*time.Hour))
	writeCached(t, dir, "m2-10-1-v1.mp4", 45, time.Now().Add(-2*time.Hour))

	// 4 bytes are available, the 6 byte source is assumed to need 6:
	// freeing the oldest rendition is enough, the newer one stays.
	r, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitIdle(t, c)
	wantNames(t, dir, "m2-10-1-v1.mp4", filepath.Base(r.Path))
}

// A modification time in the future (camera clock, clock skew) must not be
// mistaken for "modified just now" forever.
func TestCache_FutureMtimeCountsAsSettled(t *testing.T) {
	runner := &fakeRunner{payload: "x"}
	c, src, _ := newTestCache(t, runner, Options{})
	age(t, src.Path, -72*time.Hour) // three days ahead

	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("source dated in the future: %v", err)
	}
}

// A timestamp only slightly ahead is ordinary clock skew between this server
// and the file server: such a file may well be mid-write and still waits.
func TestCache_SlightlyFutureMtimeStillWaits(t *testing.T) {
	runner := &fakeRunner{payload: "x"}
	c, src, clk := newTestCache(t, runner, Options{})
	age(t, src.Path, -3*time.Second)

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("source stamped 3 s ahead = %v, want ErrSourceChanged", err)
	}
	clk.T = clk.T.Add(DefaultSourceSettle + 5*time.Second)
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("after it settled: %v", err)
	}
}

// With the volume far below its reserve the shortfall is huge, but one job
// never evicts more than it is assumed to need: emptying the cache would not
// be its business (and here would not help either).
func TestCache_EvictionForOneJobIsBounded(t *testing.T) {
	runner := &capRunner{payload: "x", gotCap: new(int64)}
	c, src, _ := newTestCache(t, runner, Options{
		Dir: t.TempDir(), MaxBytes: 1 << 20, MinFreeBytes: 1000,
		FreeSpace: func(string) (int64, error) { return 500, nil }, // 500 below the reserve
	})
	writeCached(t, c.opts.Dir, "m1-10-1-v1.mp4", 50, time.Now().Add(-3*time.Hour))
	writeCached(t, c.opts.Dir, "m2-10-1-v1.mp4", 45, time.Now().Add(-2*time.Hour))
	writeCached(t, c.opts.Dir, "m3-10-1-v1.mp4", 40, time.Now().Add(-1*time.Hour))

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("Ensure error = %v, want ErrNoSpace", err)
	}
	waitIdle(t, c)
	// The 6 byte source justifies freeing 6 bytes: the oldest rendition
	// goes, the other two stay although the volume is still short.
	wantNames(t, c.opts.Dir, "m2-10-1-v1.mp4", "m3-10-1-v1.mp4")
	if runner.calls != 0 {
		t.Error("ffmpeg ran although the volume is below its reserve")
	}
}

// Replacing a broken file with a good one must not keep answering from the
// old file's failure backoff.
func TestCache_FailureBackoffEndsWhenSourceIsReplaced(t *testing.T) {
	runner := &fakeRunner{err: errors.New("ffmpeg exploded")}
	c, src, _ := newTestCache(t, runner, Options{})
	_, _ = c.Ensure(context.Background(), src)
	waitIdle(t, c)
	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrFailedRecently) {
		t.Fatalf("same file = %v, want ErrFailedRecently", err)
	}

	rewrite(t, src.Path)
	runner.err, runner.payload = nil, "ok"
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("replaced file: %v", err)
	}
}

// The cap handed to the runner is what the volume can spare, never more
// than the cache budget.
func TestCache_OutputCapFollowsFreeSpace(t *testing.T) {
	var gotCap int64
	runner := &capRunner{payload: "x", gotCap: &gotCap}
	c, src, _ := newTestCache(t, runner, Options{
		MaxBytes: 1 << 20, MinFreeBytes: 1000,
		FreeSpace: func(string) (int64, error) { return 1500, nil },
	})
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if gotCap != 500 {
		t.Errorf("cap = %d, want free space minus reserve = 500", gotCap)
	}
}

// A runner reporting ENOSPC is "no space", not a broken file to back off.
func TestCache_RunnerNoSpaceIsNotBackedOff(t *testing.T) {
	runner := &fakeRunner{err: fmt.Errorf("%w: ffmpeg: No space left on device", ErrNoSpace)}
	c, src, _ := newTestCache(t, runner, Options{})
	for range 2 {
		if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrNoSpace) {
			t.Fatalf("Ensure error = %v, want ErrNoSpace", err)
		}
		waitIdle(t, c)
	}
	if runner.calls.Load() != 2 {
		t.Errorf("runner calls = %d, want 2 (no backoff)", runner.calls.Load())
	}
	wantNames(t, c.opts.Dir)
}

// capRunner records the size cap it was given and writes a fixed payload.
// Its fields are read by tests only while no job runs.
type capRunner struct {
	payload string
	gotCap  *int64
	calls   int
}

func (r *capRunner) Transcode(_ context.Context, job Job) error {
	r.calls++
	*r.gotCap = job.MaxBytes
	return os.WriteFile(job.Output, []byte(r.payload), 0o644)
}

func TestCache_CloseReleasesWaitersAndRefusesNewJobs(t *testing.T) {
	runner := blocking()
	c, src, _ := newTestCache(t, runner, Options{})
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Ensure(context.Background(), src)
		errCh <- err
	}()
	<-runner.started

	c.Close()
	c.Close() // idempotent
	// The waiting request returns at once, not after the 20 s wait limit.
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("waiter got %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting request was not released by Close")
	}
	if !c.WaitTimeout(settleTimeout) {
		t.Fatal("job did not stop after Close")
	}
	// No job may be started once shutdown began.
	if _, err := c.Ensure(context.Background(), newSource(t, 9)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ensure after Close = %v, want ErrClosed", err)
	}
	if runner.calls.Load() != 1 {
		t.Errorf("runner calls = %d, want 1", runner.calls.Load())
	}
	wantNames(t, c.opts.Dir)
}

// Shutdown must not hang on a job that does not react (stuck in I/O).
func TestCache_WaitTimeoutGivesUp(t *testing.T) {
	stuck := make(chan struct{})
	runner := &fakeRunner{payload: "x", started: make(chan struct{}, 1)}
	runner.hook = func(context.Context, Source) { <-stuck } // ignores cancellation
	c, src, _ := newTestCache(t, runner, Options{WaitLimit: time.Millisecond})
	_, _ = c.Ensure(context.Background(), src)
	<-runner.started

	c.Close()
	if c.WaitTimeout(20 * time.Millisecond) {
		t.Fatal("WaitTimeout reported completion while a job is stuck")
	}
	close(stuck)
	if !c.WaitTimeout(settleTimeout) {
		t.Fatal("job did not finish after it was unstuck")
	}
}

// A job for an old version of the source that finishes after the job for the
// new version must neither publish its output nor delete the newer one.
func TestCache_LateJobKeepsNewerRendition(t *testing.T) {
	runner := blocking()
	c, src, _ := newTestCache(t, runner, Options{WaitLimit: time.Millisecond})

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrPending) {
		t.Fatalf("old job = %v, want ErrPending", err)
	}
	<-runner.started
	rewrite(t, src.Path)

	// The job for the new version: release both, wait for both.
	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrPending) {
		t.Fatalf("new job = %v, want ErrPending", err)
	}
	close(runner.release)
	waitIdle(t, c)

	newest, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure newest: %v", err)
	}
	if runner.calls.Load() != 2 {
		t.Errorf("runner calls = %d, want 2 (newest must come from cache)", runner.calls.Load())
	}
	wantNames(t, c.opts.Dir, filepath.Base(newest.Path))
}

// Two server instances sharing one cache directory (rolling update) must not
// write the same temporary file.
func TestCache_InstancesUseDistinctTempFiles(t *testing.T) {
	dir := t.TempDir()
	src := newSource(t, 7)
	var runners [2]*fakeRunner
	var wg sync.WaitGroup
	for i := range runners {
		runners[i] = blocking()
		c := NewCache(context.Background(), runners[i], clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: 1 << 20, FreeSpace: plentyOfSpace})
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Ensure(context.Background(), src); err != nil {
				t.Errorf("instance %d: %v", i, err)
			}
			c.WaitTimeout(settleTimeout)
		}()
	}
	for _, r := range runners {
		<-r.started
	}
	if a, b := runners[0].outputs[0], runners[1].outputs[0]; a == b || !tmpRe.MatchString(filepath.Base(a)) {
		t.Errorf("temp files %q and %q must differ and match the temp pattern", a, b)
	}
	for _, r := range runners {
		close(r.release)
	}
	wg.Wait()
	if names := dirNames(t, dir); len(names) != 1 || !renditionRe.MatchString(names[0]) {
		t.Errorf("cache dir = %v, want exactly one finished rendition", names)
	}
}

func TestCache_ConcurrentUse(t *testing.T) {
	// Exercised under -race: lookups, jobs, prune and remove in parallel.
	c, _, _ := newTestCache(t, &fakeRunner{payload: "x"}, Options{MaxBytes: 3, MaxInFlight: 64})
	var wg sync.WaitGroup
	for i := range 16 {
		src := newSource(t, int64(i%4+1))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Ensure(context.Background(), src)
			_ = c.Prune(context.Background())
			if i%5 == 0 {
				_ = c.Remove(src.MediaID)
			}
		}()
	}
	wg.Wait()
	waitIdle(t, c)
	for _, name := range dirNames(t, c.opts.Dir) {
		if !renditionRe.MatchString(name) {
			t.Errorf("unexpected file left in cache: %s", name)
		}
	}
}

func ExampleCache_Ensure() {
	dir, _ := os.MkdirTemp("", "transcode-example")
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "clip.avi")
	_ = os.WriteFile(src, []byte("source"), 0o644)
	settled := time.Now().Add(-time.Hour) // not "still being written"
	_ = os.Chtimes(src, settled, settled)

	c := NewCache(context.Background(), &fakeRunner{payload: "x"}, clock.RealClock{}, quietLogger(), Options{
		Dir: filepath.Join(dir, "cache"), MaxBytes: 1 << 20, FreeSpace: plentyOfSpace,
	})
	r, err := c.Ensure(context.Background(), Source{MediaID: 1, Path: src, Kind: KindVideo})
	stop(c)
	fmt.Println(r.ContentType, err)
	// Output: video/mp4 <nil>
}
