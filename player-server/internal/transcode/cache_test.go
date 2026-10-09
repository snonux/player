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
	hook    func(src Source)

	mu      sync.Mutex
	outputs []string
}

func (f *fakeRunner) Transcode(ctx context.Context, src Source, outputPath string) error {
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
		f.hook(src)
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

// newSource creates a small source file for media id.
func newSource(t *testing.T, id int64) Source {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clip.avi")
	if err := os.WriteFile(path, []byte("source"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return Source{MediaID: id, Path: path, Kind: KindVideo}
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
	t.Cleanup(c.Wait)
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
		last = c.recordFailure("m1-1-1-v1.mp4")
	}
	if last != maxFailureBackoff {
		t.Errorf("backoff after many failures = %s, want cap %s", last, maxFailureBackoff)
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
	runner := &fakeRunner{payload: "x", hook: func(Source) { panic("runner bug") }}
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
	c.Wait() // returns only when the job goroutine is gone

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
	c, src, _ := newTestCache(t, runner, Options{JobTimeout: 20 * time.Millisecond})

	_, err := c.Ensure(context.Background(), src)
	if err == nil || !strings.Contains(err.Error(), "timed out") || isContextError(err) {
		t.Fatalf("Ensure error = %v, want a timeout that is not a context error", err)
	}
	waitIdle(t, c)
	// A timeout is a real failure and is backed off like one.
	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrFailedRecently) {
		t.Fatalf("after timeout = %v, want ErrFailedRecently", err)
	}
	wantNames(t, c.opts.Dir)
}

// The job timeout must cover working time only. Four jobs of ~60 ms share one
// slot, so the last one queues for ~180 ms — longer than the 150 ms timeout —
// and must still succeed.
func TestCache_QueueTimeDoesNotCountAgainstTimeout(t *testing.T) {
	runner := &fakeRunner{payload: "x", hook: func(Source) { time.Sleep(60 * time.Millisecond) }}
	c, _, _ := newTestCache(t, runner, Options{MaxConcurrent: 1, JobTimeout: 150 * time.Millisecond})

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		src := newSource(t, int64(i+1))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = c.Ensure(context.Background(), src)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("job %d: %v", i, err)
		}
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
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
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
// must not end up as a rendition of the truncated file.
func TestCache_SourceChangedDuringTranscode(t *testing.T) {
	runner := &fakeRunner{payload: "x"}
	c, src, _ := newTestCache(t, runner, Options{})
	runner.hook = func(s Source) { rewrite(t, s.Path) }

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("Ensure error = %v, want ErrSourceChanged", err)
	}
	waitIdle(t, c)
	wantNames(t, c.opts.Dir)

	// Not a failure to back off from: the retry transcodes the new version.
	runner.hook = nil
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("retry: %v", err)
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
			c.Wait()
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

	c := NewCache(context.Background(), &fakeRunner{payload: "x"}, clock.RealClock{}, quietLogger(), Options{
		Dir: filepath.Join(dir, "cache"), MaxBytes: 1 << 20, FreeSpace: plentyOfSpace,
	})
	r, err := c.Ensure(context.Background(), Source{MediaID: 1, Path: src, Kind: KindVideo})
	c.Wait()
	fmt.Println(r.ContentType, err)
	// Output: video/mp4 <nil>
}
