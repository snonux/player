package transcode

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

const (
	// DefaultWaitLimit is how long one request waits for a rendition before
	// it is told to retry. It must stay below the HTTP server's 30 s
	// WriteTimeout, otherwise the connection would be cut before the
	// "retry later" response can be written.
	DefaultWaitLimit = 20 * time.Second
	// DefaultJobTimeout bounds a single transcode (including the time it
	// spends queued) so a stuck ffmpeg cannot occupy a slot forever.
	DefaultJobTimeout = 2 * time.Hour
	// DefaultMaxConcurrent limits parallel ffmpeg processes. libx264 already
	// uses every core, so more parallelism only slows each job down; two
	// slots keep a short audio job from waiting behind a long film.
	DefaultMaxConcurrent = 2

	// tmpSuffix marks a rendition that is still being written. Renditions
	// are renamed into place only when complete, so a file without the
	// suffix is always safe to serve.
	tmpSuffix = ".tmp"
)

// Options configures a Cache. Zero durations and counts fall back to the
// Default* constants.
type Options struct {
	// Dir is the cache directory. It is created on first use and must be on
	// a writable volume (the container root filesystem is read-only).
	Dir string
	// MaxBytes is the size the cache is pruned back to, least recently used
	// renditions first. The newest rendition is never pruned, so the cache
	// can exceed MaxBytes by one file.
	MaxBytes      int64
	WaitLimit     time.Duration
	JobTimeout    time.Duration
	MaxConcurrent int
}

// job tracks one in-flight transcode. err is written before done is closed
// and must only be read afterwards.
type job struct {
	done chan struct{}
	err  error
}

// Cache produces renditions on demand, deduplicates concurrent requests for
// the same source, and keeps the on-disk cache bounded.
type Cache struct {
	// baseCtx is the application context: transcodes deliberately outlive
	// the HTTP request that started them (the client will retry and pick up
	// the finished file) but must stop on shutdown.
	baseCtx context.Context
	runner  Runner
	clk     clock.Clock
	logger  *slog.Logger
	opts    Options
	sem     chan struct{}

	mu   sync.Mutex
	jobs map[string]*job // keyed by rendition file name
}

// NewCache creates a Cache. baseCtx bounds the lifetime of background
// transcodes; cancel it on shutdown.
func NewCache(baseCtx context.Context, runner Runner, clk clock.Clock, logger *slog.Logger, opts Options) *Cache {
	if opts.WaitLimit <= 0 {
		opts.WaitLimit = DefaultWaitLimit
	}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = DefaultJobTimeout
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = DefaultMaxConcurrent
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{
		baseCtx: baseCtx,
		runner:  runner,
		clk:     clk,
		logger:  logger,
		opts:    opts,
		sem:     make(chan struct{}, opts.MaxConcurrent),
		jobs:    make(map[string]*job),
	}
}

// Ensure returns the rendition for src, producing it when it is not cached.
//
// It waits at most Options.WaitLimit. When the transcode is not finished by
// then it returns ErrPending and the transcode continues in the background;
// when ctx is cancelled it returns ctx.Err() and the transcode also
// continues, because other requests may be waiting for the same rendition
// and the client is expected to come back.
func (c *Cache) Ensure(ctx context.Context, src Source) (Rendition, error) {
	info, err := os.Stat(src.Path)
	if err != nil {
		return Rendition{}, fmt.Errorf("%w: %w", ErrSourceMissing, err)
	}
	if !info.Mode().IsRegular() {
		return Rendition{}, fmt.Errorf("%w: %s is not a regular file", ErrSourceMissing, src.Path)
	}

	name := renditionName(src, info)
	if j := c.jobFor(name, src); j != nil {
		if err := c.wait(ctx, j); err != nil {
			return Rendition{}, err
		}
	}

	path := filepath.Join(c.opts.Dir, name)
	c.touch(path)
	return Rendition{Path: path, ContentType: src.Kind.contentType(), ETag: name}, nil
}

// Prune deletes abandoned temporary files and then the least recently used
// renditions until the cache fits Options.MaxBytes. It is called after every
// transcode and periodically by the GC worker.
func (c *Cache) Prune(ctx context.Context) error {
	return c.prune(ctx, "")
}

// Remove deletes every rendition of a media item, e.g. after the item was
// hard-deleted, so removed content does not linger in the cache.
func (c *Cache) Remove(mediaID int64) error {
	return c.removeMatching(mediaPrefix(mediaID), "")
}

// renditionName derives the cache file name. It encodes the source's size and
// mtime, so replacing or editing the source file yields a new name and the
// old rendition is no longer served (it is deleted once the new one exists).
func renditionName(src Source, info fs.FileInfo) string {
	return fmt.Sprintf("%s%d-%d-%s%s", mediaPrefix(src.MediaID), info.Size(), info.ModTime().UnixNano(), profileVersion, src.Kind.ext())
}

// mediaPrefix is the file name prefix shared by all renditions of a media
// item. The trailing dash keeps "m1-" from matching media 12.
func mediaPrefix(mediaID int64) string {
	return fmt.Sprintf("m%d-", mediaID)
}

// jobFor returns the in-flight job for name, starting one when the rendition
// is neither cached nor being produced. It returns nil when the rendition is
// already on disk. The existence check happens under the lock so a job that
// finishes concurrently is seen either as a job or as a file, never neither.
func (c *Cache) jobFor(name string, src Source) *job {
	c.mu.Lock()
	defer c.mu.Unlock()

	if j, ok := c.jobs[name]; ok {
		return j
	}
	if info, err := os.Stat(filepath.Join(c.opts.Dir, name)); err == nil && info.Mode().IsRegular() {
		return nil
	}
	j := &job{done: make(chan struct{})}
	c.jobs[name] = j
	go c.run(j, name, src)
	return j
}

// wait blocks until the job finishes, ctx is cancelled, or the wait limit
// elapses.
func (c *Cache) wait(ctx context.Context, j *job) error {
	timer := time.NewTimer(c.opts.WaitLimit)
	defer timer.Stop()
	select {
	case <-j.done:
		return j.err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ErrPending
	}
}

// run executes one transcode job in its own goroutine. Failures are reported
// to the waiting requests and not remembered: the next request starts a fresh
// attempt, so a transient problem (full disk, shutdown) heals by itself.
func (c *Cache) run(j *job, name string, src Source) {
	defer func() {
		if r := recover(); r != nil {
			j.err = fmt.Errorf("transcode panic: %v", r)
			c.logger.Error("transcode panic", "media_id", src.MediaID, "panic", r)
		}
		c.mu.Lock()
		delete(c.jobs, name)
		c.mu.Unlock()
		close(j.done)
	}()

	ctx, cancel := context.WithTimeout(c.baseCtx, c.opts.JobTimeout)
	defer cancel()

	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		j.err = ctx.Err()
		return
	}

	start := c.clk.Now()
	c.logger.Info("transcode started", "media_id", src.MediaID, "source", src.Path)
	if j.err = c.produce(ctx, name, src); j.err != nil {
		c.logger.Error("transcode failed", "media_id", src.MediaID, "source", src.Path, "err", j.err)
		return
	}
	c.logger.Info("transcode finished", "media_id", src.MediaID, "rendition", name, "took", c.clk.Now().Sub(start))
	c.cleanupAfter(ctx, name, src.MediaID)
}

// produce writes the rendition to a temporary file and renames it into place,
// so readers never observe a partially written rendition.
func (c *Cache) produce(ctx context.Context, name string, src Source) error {
	if err := os.MkdirAll(c.opts.Dir, 0o755); err != nil {
		return fmt.Errorf("create transcode cache dir: %w", err)
	}
	final := filepath.Join(c.opts.Dir, name)
	tmp := final + tmpSuffix

	if err := c.runner.Transcode(ctx, src.Kind, src.Path, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	info, err := os.Stat(tmp)
	if err != nil || info.Size() == 0 {
		_ = os.Remove(tmp)
		return errors.New("transcode produced no output")
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish rendition: %w", err)
	}
	return nil
}

// cleanupAfter drops renditions of older versions of the same source and
// enforces the size bound. Problems are logged only: the new rendition is
// valid regardless.
func (c *Cache) cleanupAfter(ctx context.Context, name string, mediaID int64) {
	if err := c.removeMatching(mediaPrefix(mediaID), name); err != nil {
		c.logger.Warn("transcode cache remove stale", "media_id", mediaID, "err", err)
	}
	if err := c.prune(ctx, name); err != nil {
		c.logger.Warn("transcode cache prune", "err", err)
	}
}

// touch marks a rendition as recently used. The mtime is the LRU timestamp
// (atime is unreliable on noatime/relatime mounts); HTTP validators use the
// file name based ETag instead, so bumping the mtime does not invalidate
// client caches.
func (c *Cache) touch(path string) {
	now := c.clk.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		c.logger.Warn("transcode cache touch", "path", path, "err", err)
	}
}

// removeMatching deletes cache files whose name starts with prefix, except
// keep and keep's temporary file.
func (c *Cache) removeMatching(prefix, keep string) error {
	entries, err := c.readDir()
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || (keep != "" && strings.TrimSuffix(name, tmpSuffix) == keep) {
			continue
		}
		if err := os.Remove(filepath.Join(c.opts.Dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// cached is one finished rendition considered for eviction.
type cached struct {
	name    string
	size    int64
	lastUse time.Time
}

// prune implements Prune; keep names a rendition that must survive even when
// it alone exceeds the limit (the one that was just produced and is about to
// be served).
func (c *Cache) prune(ctx context.Context, keep string) error {
	files, total, err := c.scan()
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, k int) bool { return files[i].lastUse.Before(files[k].lastUse) })

	var errs []error
	for _, f := range files {
		if total <= c.opts.MaxBytes || ctx.Err() != nil {
			break
		}
		if f.name == keep {
			continue
		}
		if err := os.Remove(filepath.Join(c.opts.Dir, f.name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		total -= f.size
		c.logger.Info("transcode cache evicted", "rendition", f.name, "size", f.size)
	}
	return errors.Join(errs...)
}

// scan lists finished renditions with their total size. Temporary files that
// no running job owns are leftovers of a crash or restart and are deleted.
func (c *Cache) scan() ([]cached, int64, error) {
	entries, err := c.readDir()
	if err != nil {
		return nil, 0, err
	}
	var files []cached
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		base, isTmp := strings.CutSuffix(e.Name(), tmpSuffix)
		if !isRenditionName(base) {
			continue
		}
		if isTmp {
			if !c.inFlight(base) {
				_ = os.Remove(filepath.Join(c.opts.Dir, e.Name()))
			}
			continue
		}
		files = append(files, cached{name: e.Name(), size: info.Size(), lastUse: info.ModTime()})
		total += info.Size()
	}
	return files, total, nil
}

// isRenditionName reports whether a file name looks like one this cache
// wrote. Anything else is left alone, so pointing the cache at a directory
// that also holds other data (e.g. the SQLite database) cannot delete it.
func isRenditionName(name string) bool {
	ext := filepath.Ext(name)
	return strings.HasPrefix(name, "m") && (ext == KindVideo.ext() || ext == KindAudio.ext())
}

// inFlight reports whether a job is currently producing the named rendition.
func (c *Cache) inFlight(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.jobs[name]
	return ok
}

// readDir lists the cache directory; a directory that does not exist yet is
// an empty cache, not an error.
func (c *Cache) readDir() ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(c.opts.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read transcode cache dir: %w", err)
	}
	return entries, nil
}
