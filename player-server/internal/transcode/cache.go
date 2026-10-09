package transcode

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

const (
	// DefaultWaitLimit is how long one request waits for a rendition before
	// it is told to retry. It stays below the HTTP server's 30 s
	// WriteTimeout so the "retry later" answer can always be written.
	DefaultWaitLimit = 20 * time.Second
	// DefaultJobTimeout bounds the working time of one transcode. Time spent
	// queued for a slot does not count, so a job that waited long still gets
	// its full budget.
	DefaultJobTimeout = 2 * time.Hour
	// DefaultMaxConcurrent limits parallel ffmpeg processes. It is one more
	// than DefaultMaxPerRequester so a single requester can never hold every
	// slot.
	DefaultMaxConcurrent = 3
	// DefaultMaxPerRequester limits the jobs (running or queued) one user or
	// share token may have at a time.
	DefaultMaxPerRequester = 2
	// DefaultMaxInFlight bounds running plus queued jobs, and with it the
	// number of goroutines parked on the slot semaphore.
	DefaultMaxInFlight = 8
	// DefaultMinFreeBytes is the free space that must remain on the cache
	// volume. The default cache directory shares its volume with the SQLite
	// database, which must never run out of space because of renditions.
	DefaultMinFreeBytes = 512 << 20

	// tmpMaxAge is how long an unmodified temporary file is left alone.
	// ffmpeg writes continuously, so an hour of silence means its process
	// (possibly in another instance sharing the directory) is gone.
	tmpMaxAge = time.Hour
	// pruneGrace protects renditions handed out very recently from eviction,
	// so a request that just finished waiting can still open its file.
	pruneGrace = time.Minute
	// failureBackoff is the pause after a first failed transcode; it doubles
	// per consecutive failure up to maxFailureBackoff.
	failureBackoff    = time.Minute
	maxFailureBackoff = 30 * time.Minute
)

// Rendition file names are "m<mediaID>-<srcSize>-<srcMtimeNs>-v<profile>"
// plus extension; temporary files append ".<random>.tmp". The cache only
// ever deletes names matching these patterns, so unrelated files in the
// directory (the SQLite database, or media if someone points the cache at
// it) are never touched.
var (
	renditionRe = regexp.MustCompile(`^m\d+-\d+-\d+-v\d+\.(mp4|m4a)$`)
	tmpRe       = regexp.MustCompile(`^m\d+-\d+-\d+-v\d+\.(mp4|m4a)\.\d+\.tmp$`)
)

// Options configures a Cache. Zero values fall back to the Default*
// constants.
type Options struct {
	// Dir is the cache directory. It is created on first use and must be on
	// a writable volume (the container root filesystem is read-only).
	Dir string
	// MaxBytes is the size the cache is pruned back to, least recently used
	// renditions first. Files of running jobs count towards it.
	MaxBytes        int64
	WaitLimit       time.Duration
	JobTimeout      time.Duration
	MaxConcurrent   int
	MaxPerRequester int
	MaxInFlight     int
	MinFreeBytes    int64
	// FreeSpace reports the free bytes of the volume holding a directory.
	// Tests replace it; the default asks the operating system.
	FreeSpace func(dir string) (int64, error)
}

// withDefaults fills unset options.
func (o Options) withDefaults() Options {
	if o.WaitLimit <= 0 {
		o.WaitLimit = DefaultWaitLimit
	}
	if o.JobTimeout <= 0 {
		o.JobTimeout = DefaultJobTimeout
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = DefaultMaxConcurrent
	}
	if o.MaxPerRequester <= 0 {
		o.MaxPerRequester = DefaultMaxPerRequester
	}
	if o.MaxInFlight <= 0 {
		o.MaxInFlight = DefaultMaxInFlight
	}
	if o.MinFreeBytes <= 0 {
		o.MinFreeBytes = DefaultMinFreeBytes
	}
	if o.FreeSpace == nil {
		o.FreeSpace = freeSpace
	}
	return o
}

// job tracks one in-flight transcode. err is written before done is closed
// and must only be read afterwards.
type job struct {
	done      chan struct{}
	err       error
	mediaID   int64
	requester string
	cancel    context.CancelFunc
}

// failure is a negative-cache entry for a rendition that failed to build.
type failure struct {
	count   int
	retryAt time.Time
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
	wg      sync.WaitGroup // one count per job goroutine, see Wait

	// mu guards the maps below. It is never held across file I/O, so a hung
	// (network) filesystem cannot block unrelated requests.
	mu       sync.Mutex
	jobs     map[string]*job      // in-flight jobs by rendition name
	lastUse  map[string]time.Time // LRU clock by rendition name
	failures map[string]failure   // negative cache by rendition name
}

// NewCache creates a Cache. baseCtx bounds the lifetime of background
// transcodes; cancel it on shutdown and then call Wait.
func NewCache(baseCtx context.Context, runner Runner, clk clock.Clock, logger *slog.Logger, opts Options) *Cache {
	opts = opts.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{
		baseCtx:  baseCtx,
		runner:   runner,
		clk:      clk,
		logger:   logger,
		opts:     opts,
		sem:      make(chan struct{}, opts.MaxConcurrent),
		jobs:     make(map[string]*job),
		lastUse:  make(map[string]time.Time),
		failures: make(map[string]failure),
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
	name, err := currentName(src)
	if err != nil {
		c.logger.Warn("transcode source unreadable", "media_id", src.MediaID, "source", src.Path, "err", err)
		return Rendition{}, ErrSourceMissing
	}
	if r, ok := c.lookup(name, src.Kind); ok {
		return r, nil
	}

	j, err := c.jobFor(name, src)
	if err != nil {
		return Rendition{}, err
	}
	if err := c.wait(ctx, j); err != nil {
		return Rendition{}, err
	}
	if r, ok := c.lookup(name, src.Kind); ok {
		return r, nil
	}
	// Finished but gone again (evicted by another instance, removed with its
	// media item): the caller retries and a new job starts.
	return Rendition{}, ErrPending
}

// Prune deletes abandoned temporary files and then the least recently used
// renditions until the cache fits Options.MaxBytes. It runs before every
// transcode and periodically from the GC worker.
func (c *Cache) Prune(ctx context.Context) error {
	return c.prune(ctx, 0)
}

// Remove deletes every rendition of a media item and stops its running
// transcodes, e.g. after the item was hard-deleted, so removed content
// neither lingers in the cache nor is published afterwards.
func (c *Cache) Remove(mediaID int64) error {
	c.mu.Lock()
	for _, j := range c.jobs {
		if j.mediaID == mediaID {
			j.cancel()
		}
	}
	c.mu.Unlock()
	return c.removeFiles(func(name string) bool {
		return hasMediaPrefix(name, mediaID) && (renditionRe.MatchString(name) || tmpRe.MatchString(name))
	})
}

// Wait blocks until every transcode goroutine has returned. Call it after
// cancelling the base context so shutdown does not leave ffmpeg processes or
// temporary files behind.
func (c *Cache) Wait() {
	c.wg.Wait()
}

// Preflight verifies that the cache directory can be created and written,
// so a read-only or mis-mounted volume is reported at startup rather than as
// a failed playback.
func (c *Cache) Preflight() error {
	if err := os.MkdirAll(c.opts.Dir, 0o755); err != nil {
		return fmt.Errorf("create transcode cache dir: %w", err)
	}
	f, err := os.CreateTemp(c.opts.Dir, ".preflight-*")
	if err != nil {
		return fmt.Errorf("transcode cache dir not writable: %w", err)
	}
	_ = f.Close()
	return os.Remove(f.Name())
}

// currentName stats the source and derives its rendition file name. The name
// encodes the source's size and mtime, so replacing or editing the source
// yields a new name and the old rendition is no longer served.
func currentName(src Source) (string, error) {
	info, err := os.Stat(src.Path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", src.Path)
	}
	// The mtime is formatted unsigned so pre-1970 timestamps cannot put a
	// minus sign into the name and break the name patterns.
	return fmt.Sprintf("%s%d-%d-%s%s", mediaPrefix(src.MediaID), info.Size(), uint64(info.ModTime().UnixNano()), profileVersion, src.Kind.ext()), nil
}

// mediaPrefix is the file name prefix shared by all renditions of a media
// item. The trailing dash keeps "m1-" from matching media 12.
func mediaPrefix(mediaID int64) string {
	return fmt.Sprintf("m%d-", mediaID)
}

// hasMediaPrefix reports whether a cache file name belongs to mediaID.
func hasMediaPrefix(name string, mediaID int64) bool {
	p := mediaPrefix(mediaID)
	return len(name) >= len(p) && name[:len(p)] == p
}

// lookup returns the finished rendition when it is on disk and records the
// access for LRU eviction.
func (c *Cache) lookup(name string, kind Kind) (Rendition, bool) {
	path := filepath.Join(c.opts.Dir, name)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Rendition{}, false
	}
	c.mu.Lock()
	c.lastUse[name] = c.clk.Now()
	c.mu.Unlock()
	return Rendition{
		Path:        path,
		ContentType: kind.contentType(),
		// The mtime is the moment this encode was published and is never
		// touched afterwards, see Rendition.ETag.
		ETag: fmt.Sprintf("%s-%x", name, uint64(info.ModTime().UnixNano())),
	}, true
}

// jobFor returns the in-flight job for name, starting one unless the limits
// or the negative cache forbid it. It does no file I/O under the lock; the
// job itself re-checks whether the rendition appeared in the meantime.
func (c *Cache) jobFor(name string, src Source) (*job, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if j, ok := c.jobs[name]; ok {
		return j, nil
	}
	if f, ok := c.failures[name]; ok && c.clk.Now().Before(f.retryAt) {
		return nil, ErrFailedRecently
	}
	if len(c.jobs) >= c.opts.MaxInFlight || c.requesterJobs(src.Requester) >= c.opts.MaxPerRequester {
		return nil, ErrBusy
	}

	ctx, cancel := context.WithCancel(c.baseCtx)
	j := &job{done: make(chan struct{}), mediaID: src.MediaID, requester: src.Requester, cancel: cancel}
	c.jobs[name] = j
	c.wg.Add(1)
	go c.run(ctx, j, name, src)
	return j, nil
}

// requesterJobs counts the in-flight jobs started by requester. An empty
// requester is not limited. The caller holds c.mu.
func (c *Cache) requesterJobs(requester string) int {
	if requester == "" {
		return 0
	}
	n := 0
	for _, j := range c.jobs {
		if j.requester == requester {
			n++
		}
	}
	return n
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

// run executes one transcode job in its own goroutine: wait for a slot, then
// produce the rendition within the job timeout.
func (c *Cache) run(ctx context.Context, j *job, name string, src Source) {
	defer c.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			j.err = fmt.Errorf("transcode panic: %v", r)
			c.logger.Error("transcode panic", "media_id", src.MediaID, "panic", r)
		}
		c.mu.Lock()
		delete(c.jobs, name)
		c.mu.Unlock()
		j.cancel()
		close(j.done)
	}()

	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		j.err = ErrAborted
		return
	}
	// The rendition may have been published while this job was queued or
	// between the caller's lookup and jobFor (also by another instance).
	if _, err := os.Stat(filepath.Join(c.opts.Dir, name)); err == nil {
		return
	}

	// The timeout starts only now, so queue time does not eat the budget.
	workCtx, cancelWork := context.WithTimeout(ctx, c.opts.JobTimeout)
	defer cancelWork()
	start := c.clk.Now()
	c.logger.Info("transcode started", "media_id", src.MediaID, "source", src.Path)
	j.err = c.settle(ctx, workCtx, name, src, c.produce(workCtx, name, src))
	if j.err == nil {
		c.logger.Info("transcode finished", "media_id", src.MediaID, "rendition", name, "took", c.clk.Now().Sub(start))
		c.cleanupAfter(ctx, src)
	}
}

// settle turns the outcome of produce into the error reported to waiting
// requests and maintains the negative cache.
//
// Context errors are replaced, never wrapped: waiters must not mistake a
// stopped job for their own request being cancelled. Only genuine transcode
// failures (including a timeout) are remembered; conditions that heal by
// themselves (shutdown, full disk, source still changing) are not.
func (c *Cache) settle(jobCtx, workCtx context.Context, name string, src Source, err error) error {
	switch {
	case err == nil:
		c.mu.Lock()
		delete(c.failures, name)
		c.lastUse[name] = c.clk.Now()
		c.mu.Unlock()
		return nil
	case jobCtx.Err() != nil:
		c.logger.Info("transcode aborted", "media_id", src.MediaID, "source", src.Path)
		return ErrAborted
	case errors.Is(err, ErrNoSpace), errors.Is(err, ErrSourceChanged):
		c.logger.Warn("transcode not completed", "media_id", src.MediaID, "source", src.Path, "err", err)
		return err
	case workCtx.Err() != nil:
		err = fmt.Errorf("transcode timed out after %s", c.opts.JobTimeout)
	}
	retryIn := c.recordFailure(name)
	c.logger.Error("transcode failed", "media_id", src.MediaID, "source", src.Path, "retry_in", retryIn, "err", err)
	return err
}

// recordFailure notes a failed transcode and returns how long new attempts
// are refused. Without it every request for a file ffmpeg cannot convert
// would start another ffmpeg run.
func (c *Cache) recordFailure(name string) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clk.Now()
	// Forget entries that expired long ago so the map cannot grow forever.
	for n, f := range c.failures {
		if now.Sub(f.retryAt) > maxFailureBackoff {
			delete(c.failures, n)
		}
	}
	f := c.failures[name]
	f.count++
	backoff := maxFailureBackoff
	if f.count < 6 { // 1m, 2m, 4m, 8m, 16m, then the 30m cap
		backoff = failureBackoff << (f.count - 1)
	}
	f.retryAt = now.Add(backoff)
	c.failures[name] = f
	return backoff
}

// produce writes the rendition to a uniquely named temporary file and
// renames it into place, so readers never observe a partial rendition and
// two instances sharing the directory never write the same file.
func (c *Cache) produce(ctx context.Context, name string, src Source) error {
	if err := os.MkdirAll(c.opts.Dir, 0o755); err != nil {
		return fmt.Errorf("create transcode cache dir: %w", err)
	}
	if err := c.makeRoom(ctx, src); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.opts.Dir, name+".*.tmp")
	if err != nil {
		return fmt.Errorf("create rendition temp file: %w", err)
	}
	tmp := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(tmp) }() // no-op once renamed

	if err := c.runner.Transcode(ctx, src, tmp); err != nil {
		return err
	}
	if info, err := os.Stat(tmp); err != nil || info.Size() == 0 {
		return errors.New("transcode produced no output")
	}
	// A source that changed during the run (upload or copy still in
	// progress) would leave a rendition of a truncated file under a name
	// nobody asks for any more; discard it.
	if cur, err := currentName(src); err != nil || cur != name {
		return ErrSourceChanged
	}
	if err := os.Rename(tmp, filepath.Join(c.opts.Dir, name)); err != nil {
		return fmt.Errorf("publish rendition: %w", err)
	}
	return nil
}

// makeRoom evicts old renditions to fit the new one and refuses to start
// when the volume would drop below the free-space reserve. The rendition
// size is unknown beforehand; the source size is the estimate (H.264/AAC at
// these settings is rarely larger than a legacy-codec original).
func (c *Cache) makeRoom(ctx context.Context, src Source) error {
	info, err := os.Stat(src.Path)
	if err != nil {
		return ErrSourceChanged
	}
	if err := c.prune(ctx, info.Size()); err != nil {
		c.logger.Warn("transcode cache prune", "err", err)
	}
	free, err := c.opts.FreeSpace(c.opts.Dir)
	if err != nil {
		return fmt.Errorf("check transcode cache free space: %w", err)
	}
	if free < c.opts.MinFreeBytes+info.Size() {
		return fmt.Errorf("%w: %d bytes free, need %d", ErrNoSpace, free, c.opts.MinFreeBytes+info.Size())
	}
	return nil
}

// cleanupAfter drops renditions of older versions of the same source.
// "Older" is decided by looking at the source again: only the rendition
// matching its current size and mtime is kept, so a job that finishes late
// can never delete the output of a newer job.
func (c *Cache) cleanupAfter(ctx context.Context, src Source) {
	cur, err := currentName(src)
	if err != nil {
		return
	}
	err = c.removeFiles(func(name string) bool {
		return hasMediaPrefix(name, src.MediaID) && renditionRe.MatchString(name) && name != cur
	})
	if err != nil {
		c.logger.Warn("transcode cache remove stale", "media_id", src.MediaID, "err", err)
	}
	if err := c.prune(ctx, 0); err != nil {
		c.logger.Warn("transcode cache prune", "err", err)
	}
}

// removeFiles deletes the cache files selected by match.
func (c *Cache) removeFiles(match func(name string) bool) error {
	entries, err := c.readDir()
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if !match(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(c.opts.Dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
