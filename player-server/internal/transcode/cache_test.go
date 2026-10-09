package transcode

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

// fakeRunner is a Runner that writes a fixed payload instead of running
// ffmpeg. started receives one value per invocation; when release is non-nil
// the runner blocks until it is closed (or ctx is cancelled).
type fakeRunner struct {
	calls   atomic.Int32
	payload string
	err     error
	started chan struct{}
	release chan struct{}
}

func (f *fakeRunner) Transcode(ctx context.Context, _ Kind, _, outputPath string) error {
	f.calls.Add(1)
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
	if f.err != nil {
		// Leave a partial file behind, like a crashed ffmpeg would.
		_ = os.WriteFile(outputPath, []byte("partial"), 0o644)
		return f.err
	}
	return os.WriteFile(outputPath, []byte(f.payload), 0o644)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestCache builds a cache in a temp dir and a source file to transcode.
func newTestCache(t *testing.T, runner Runner, opts Options) (*Cache, Source) {
	t.Helper()
	if opts.Dir == "" {
		// A not-yet-existing subdirectory proves the cache creates it.
		opts.Dir = filepath.Join(t.TempDir(), "cache")
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 1 << 20
	}
	src := filepath.Join(t.TempDir(), "clip.avi")
	if err := os.WriteFile(src, []byte("source"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	c := NewCache(context.Background(), runner, clock.RealClock{}, quietLogger(), opts)
	return c, Source{MediaID: 7, Path: src, Kind: KindVideo}
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

func TestCache_EnsureProducesAndReuses(t *testing.T) {
	runner := &fakeRunner{payload: "rendition"}
	c, src := newTestCache(t, runner, Options{})

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
	if names := dirNames(t, c.opts.Dir); len(names) != 1 {
		t.Errorf("cache dir = %v, want exactly the rendition", names)
	}
}

func TestCache_AudioKind(t *testing.T) {
	c, src := newTestCache(t, &fakeRunner{payload: "a"}, Options{})
	src.Kind = KindAudio
	r, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if r.ContentType != "audio/mp4" || filepath.Ext(r.Path) != ".m4a" {
		t.Errorf("unexpected audio rendition %+v", r)
	}
}

func TestCache_DeduplicatesConcurrentRequests(t *testing.T) {
	runner := &fakeRunner{payload: "x", started: make(chan struct{}, 16), release: make(chan struct{})}
	c, src := newTestCache(t, runner, Options{})

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

func TestCache_RunnerFailure(t *testing.T) {
	boom := errors.New("ffmpeg exploded")
	runner := &fakeRunner{err: boom}
	c, src := newTestCache(t, runner, Options{})

	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, boom) {
		t.Fatalf("Ensure error = %v, want %v", err, boom)
	}
	if names := dirNames(t, c.opts.Dir); len(names) != 0 {
		t.Errorf("failed transcode left files behind: %v", names)
	}

	// Failures are not cached: the next request tries again.
	runner.err = nil
	runner.payload = "ok"
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if runner.calls.Load() != 2 {
		t.Errorf("runner calls = %d, want 2", runner.calls.Load())
	}
}

func TestCache_EmptyOutputIsFailure(t *testing.T) {
	c, src := newTestCache(t, &fakeRunner{payload: ""}, Options{})
	if _, err := c.Ensure(context.Background(), src); err == nil {
		t.Fatal("expected error for empty rendition")
	}
	if names := dirNames(t, c.opts.Dir); len(names) != 0 {
		t.Errorf("empty rendition left files behind: %v", names)
	}
}

func TestCache_CancelledRequestKeepsTranscoding(t *testing.T) {
	runner := &fakeRunner{payload: "x", started: make(chan struct{}, 1), release: make(chan struct{})}
	c, src := newTestCache(t, runner, Options{})

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
	runner := &fakeRunner{payload: "x", started: make(chan struct{}, 1), release: make(chan struct{})}
	c, src := newTestCache(t, runner, Options{WaitLimit: 10 * time.Millisecond})

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

func TestCache_ShutdownStopsJob(t *testing.T) {
	runner := &fakeRunner{payload: "x", started: make(chan struct{}, 1), release: make(chan struct{})}
	base, shutdown := context.WithCancel(context.Background())
	src := filepath.Join(t.TempDir(), "clip.avi")
	if err := os.WriteFile(src, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(base, runner, clock.RealClock{}, quietLogger(), Options{Dir: t.TempDir(), MaxBytes: 1 << 20})

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Ensure(context.Background(), Source{MediaID: 1, Path: src})
		errCh <- err
	}()
	<-runner.started
	shutdown()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Ensure error = %v, want context.Canceled from shutdown", err)
	}
}

func TestCache_SourceMissing(t *testing.T) {
	c, src := newTestCache(t, &fakeRunner{payload: "x"}, Options{})
	src.Path = filepath.Join(t.TempDir(), "gone.avi")
	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrSourceMissing) {
		t.Fatalf("Ensure error = %v, want ErrSourceMissing", err)
	}
	src.Path = t.TempDir() // a directory is not a media file
	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrSourceMissing) {
		t.Fatalf("Ensure(dir) error = %v, want ErrSourceMissing", err)
	}
}

func TestCache_UnwritableDir(t *testing.T) {
	// A regular file where the cache directory should be makes MkdirAll
	// fail, like a read-only root filesystem does.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c, src := newTestCache(t, &fakeRunner{payload: "x"}, Options{Dir: filepath.Join(blocker, "cache")})
	if _, err := c.Ensure(context.Background(), src); err == nil {
		t.Fatal("expected error for unwritable cache dir")
	}
}

func TestCache_InvalidatesWhenSourceChanges(t *testing.T) {
	runner := &fakeRunner{payload: "one"}
	c, src := newTestCache(t, runner, Options{})

	first, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// Rewrite the source with a different size and mtime.
	if err := os.WriteFile(src.Path, []byte("a different source"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(src.Path, later, later); err != nil {
		t.Fatal(err)
	}
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
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) {
		t.Errorf("stale rendition still present: %v", err)
	}
}

// writeCached creates a cache file of the given size and last-use time.
func writeCached(t *testing.T, dir, name string, size int, lastUse time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, lastUse, lastUse); err != nil {
		t.Fatal(err)
	}
}

func TestCache_PruneEvictsLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeCached(t, dir, "m1-10-1-v1.mp4", 100, now.Add(-3*time.Hour))
	writeCached(t, dir, "m2-10-1-v1.m4a", 100, now.Add(-2*time.Hour))
	writeCached(t, dir, "m3-10-1-v1.mp4", 100, now.Add(-1*time.Hour))
	writeCached(t, dir, "m9-10-1-v1.mp4.tmp", 100, now) // orphan of a crash
	writeCached(t, dir, "media.db", 500, now.Add(-9*time.Hour))

	c := NewCache(context.Background(), &fakeRunner{}, clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: 250})
	if err := c.Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	want := map[string]bool{"m2-10-1-v1.m4a": true, "m3-10-1-v1.mp4": true, "media.db": true}
	got := dirNames(t, dir)
	if len(got) != len(want) {
		t.Fatalf("cache dir after prune = %v, want %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected survivor %q", name)
		}
	}
}

func TestCache_PruneMissingDir(t *testing.T) {
	c := NewCache(context.Background(), &fakeRunner{}, clock.RealClock{}, quietLogger(), Options{Dir: filepath.Join(t.TempDir(), "absent")})
	if err := c.Prune(context.Background()); err != nil {
		t.Fatalf("Prune on missing dir: %v", err)
	}
	if err := c.Remove(1); err != nil {
		t.Fatalf("Remove on missing dir: %v", err)
	}
}

func TestCache_NewRenditionSurvivesOwnPrune(t *testing.T) {
	// MaxBytes smaller than the rendition: older files go, the fresh one
	// stays because it is about to be served.
	runner := &fakeRunner{payload: "0123456789"}
	c, src := newTestCache(t, runner, Options{Dir: t.TempDir(), MaxBytes: 5})
	writeCached(t, c.opts.Dir, "m99-1-1-v1.mp4", 50, time.Now().Add(-time.Hour))

	r, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitIdle(t, c)
	if names := dirNames(t, c.opts.Dir); len(names) != 1 || names[0] != filepath.Base(r.Path) {
		t.Errorf("cache dir = %v, want only %s", names, filepath.Base(r.Path))
	}
}

func TestCache_Remove(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeCached(t, dir, "m1-10-1-v1.mp4", 10, now)
	writeCached(t, dir, "m1-20-2-v1.mp4", 10, now)
	writeCached(t, dir, "m12-10-1-v1.mp4", 10, now)

	c := NewCache(context.Background(), &fakeRunner{}, clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: 1 << 20})
	if err := c.Remove(1); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if names := dirNames(t, dir); len(names) != 1 || names[0] != "m12-10-1-v1.mp4" {
		t.Errorf("cache dir after Remove(1) = %v, want only media 12", names)
	}
}

func TestCache_TouchMakesRenditionRecentlyUsed(t *testing.T) {
	runner := &fakeRunner{payload: "x"}
	c, src := newTestCache(t, runner, Options{})
	r, err := c.Ensure(context.Background(), src)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(r.Path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	info, err := os.Stat(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) > time.Hour {
		t.Errorf("cache hit did not refresh last-use time: %v", info.ModTime())
	}
}
