package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

// writeCached creates a cache file of the given size and modification time.
func writeCached(t *testing.T, dir, name string, size int, modTime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

// pruneCache builds a cache over an existing directory.
func pruneCache(dir string, maxBytes int64) *Cache {
	return NewCache(context.Background(), &fakeRunner{}, clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: maxBytes, FreeSpace: plentyOfSpace})
}

func TestCache_PruneEvictsLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeCached(t, dir, "m1-10-1-v1.mp4", 100, now.Add(-3*time.Hour))
	writeCached(t, dir, "m2-10-1-v1.m4a", 100, now.Add(-2*time.Hour))
	writeCached(t, dir, "m3-10-1-v1.mp4", 100, now.Add(-1*time.Hour))

	if err := pruneCache(dir, 250).Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	wantNames(t, dir, "m2-10-1-v1.m4a", "m3-10-1-v1.mp4")
}

// The cache may be pointed at a directory that holds other things (by default
// it sits next to the SQLite database). Only files with the exact rendition
// name pattern may ever be deleted — in particular not media files that
// merely start with "m" and end in .mp4.
func TestCache_PruneAndRemoveLeaveForeignFilesAlone(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	foreign := []string{"media.db", "movie.mp4", "m1-holiday.mp4", "m1-10-1-v1.mp4.bak", "my-song.m4a", "m1-10-1-v1.mkv", "notes.tmp"}
	for _, name := range foreign {
		writeCached(t, dir, name, 500, old)
	}
	writeCached(t, dir, "m1-10-1-v1.mp4", 100, old)
	writeCached(t, dir, "m2-10-1-v1.mp4", 100, time.Now())

	c := pruneCache(dir, 1)
	if err := c.Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	wantNames(t, dir, append(foreign, "m2-10-1-v1.mp4")...)

	if err := c.Remove(1); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := c.Remove(2); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	wantNames(t, dir, foreign...)
}

func TestCache_PruneKeepsMostRecentlyUsedEvenWhenOversized(t *testing.T) {
	dir := t.TempDir()
	writeCached(t, dir, "m1-10-1-v1.mp4", 100, time.Now().Add(-time.Hour))
	writeCached(t, dir, "m2-10-1-v1.mp4", 5000, time.Now())

	if err := pruneCache(dir, 10).Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	// The big one is likely being played; deleting it would restart its
	// transcode on every following Range request.
	wantNames(t, dir, "m2-10-1-v1.mp4")
}

func TestCache_PruneCountsRunningJobsAndDropsStaleTemp(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeCached(t, dir, "m1-10-1-v1.mp4", 100, now.Add(-3*time.Hour))
	writeCached(t, dir, "m2-10-1-v1.mp4", 100, now.Add(-2*time.Hour))
	writeCached(t, dir, "m3-10-1-v1.mp4.111.tmp", 100, now)                   // job of this or another instance
	writeCached(t, dir, "m4-10-1-v1.mp4.222.tmp", 100, now.Add(-2*time.Hour)) // crashed long ago

	// 250 bytes would fit both renditions; the running job's 100 bytes
	// count too, so the older rendition has to go.
	if err := pruneCache(dir, 250).Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	wantNames(t, dir, "m2-10-1-v1.mp4", "m3-10-1-v1.mp4.111.tmp")
}

func TestCache_PruneMissingDir(t *testing.T) {
	c := pruneCache(filepath.Join(t.TempDir(), "absent"), 1)
	if err := c.Prune(context.Background()); err != nil {
		t.Fatalf("Prune on missing dir: %v", err)
	}
	if err := c.Remove(1); err != nil {
		t.Fatalf("Remove on missing dir: %v", err)
	}
}

func TestCache_PruneUnreadableDir(t *testing.T) {
	// A regular file in place of the directory: listing fails with
	// something other than "does not exist".
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pruneCache(file, 1).Prune(context.Background()); err == nil {
		t.Fatal("expected error when the cache dir cannot be listed")
	}
}

// A rendition that was just handed to a request must survive a prune (e.g.
// the GC tick) until the request had time to open it.
func TestCache_PruneSparesRecentlyServedRenditions(t *testing.T) {
	c, a, clk := newTestCache(t, &fakeRunner{payload: "0123456789"}, Options{MaxBytes: 1})
	b := newSource(t, 8)
	ra, err := c.Ensure(context.Background(), a)
	if err != nil {
		t.Fatalf("Ensure a: %v", err)
	}
	waitIdle(t, c)
	clk.T = clk.T.Add(time.Second)
	rb, err := c.Ensure(context.Background(), b)
	if err != nil {
		t.Fatalf("Ensure b: %v", err)
	}
	waitIdle(t, c)

	if err := c.Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	wantNames(t, c.opts.Dir, filepath.Base(ra.Path), filepath.Base(rb.Path))

	// Once the grace period is over the older one is evicted.
	clk.T = clk.T.Add(2 * pruneGrace)
	if err := c.Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	wantNames(t, c.opts.Dir, filepath.Base(rb.Path))
}

func TestCache_PruneDuringJobKeepsItsTempFile(t *testing.T) {
	runner := blocking()
	c, src, _ := newTestCache(t, runner, Options{MaxBytes: 1, WaitLimit: time.Millisecond})
	if _, err := c.Ensure(context.Background(), src); !errors.Is(err, ErrPending) {
		t.Fatalf("Ensure = %v, want ErrPending", err)
	}
	<-runner.started

	if err := c.Prune(context.Background()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if names := dirNames(t, c.opts.Dir); len(names) != 1 || !tmpRe.MatchString(names[0]) {
		t.Fatalf("cache dir during job = %v, want its temp file", names)
	}
	close(runner.release)
	waitIdle(t, c)
	if _, err := c.Ensure(context.Background(), src); err != nil {
		t.Fatalf("job must still succeed after a prune: %v", err)
	}
}

// Removing a media item while its transcode runs stops the job; the output
// must not be published afterwards.
func TestCache_RemoveCancelsRunningJob(t *testing.T) {
	runner := blocking()
	c, src, _ := newTestCache(t, runner, Options{Dir: t.TempDir()})
	// Media 70 shares the digit prefix of media 7 and must not be touched.
	writeCached(t, c.opts.Dir, "m70-10-1-v1.mp4", 10, time.Now())

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Ensure(context.Background(), src)
		errCh <- err
	}()
	<-runner.started
	if err := c.Remove(src.MediaID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := <-errCh; !errors.Is(err, ErrAborted) {
		t.Fatalf("Ensure error = %v, want ErrAborted", err)
	}
	close(runner.release)
	waitIdle(t, c)
	wantNames(t, c.opts.Dir, "m70-10-1-v1.mp4")
}

func TestCache_Remove(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeCached(t, dir, "m1-10-1-v1.mp4", 10, now)
	writeCached(t, dir, "m1-20-2-v1.mp4", 10, now)
	writeCached(t, dir, "m1-20-2-v1.mp4.5.tmp", 10, now)
	writeCached(t, dir, "m12-10-1-v1.mp4", 10, now)

	if err := pruneCache(dir, 1<<20).Remove(1); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	wantNames(t, dir, "m12-10-1-v1.mp4")
}
