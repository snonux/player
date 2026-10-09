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
	// DefaultMaxConcurrent is the number of parallel ffmpeg processes. One
	// is the safe default: a 1080p job needs roughly 400 MB of memory and
	// as much CPU as it is given, and the container limits are sized for a
	// single job next to the server.
	DefaultMaxConcurrent = 1
	// DefaultMaxPerRequester limits the jobs (running or queued) one
	// requester may have at a time. All anonymous share requests together
	// count as one requester, so they can never fill the queue.
	DefaultMaxPerRequester = 2
	// DefaultMaxInFlight bounds running plus queued jobs, and with it the
	// number of goroutines parked on the slot semaphore.
	DefaultMaxInFlight = 8
	// DefaultMinFreeBytes is the free space that must remain on the cache
	// volume. The default cache directory shares its volume with the SQLite
	// database, which must never run out of space because of renditions.
	DefaultMinFreeBytes = 512 << 20
	// DefaultSourceSettle is how long a source must have been unmodified
	// before it is transcoded. A file that changed seconds ago is most
	// likely still being uploaded or copied.
	DefaultSourceSettle = 10 * time.Second

	// tmpMaxAge is how long an unmodified temporary file is left alone.
	// ffmpeg writes continuously, so an hour of silence means its process
	// (possibly in another instance sharing the directory) is gone.
	tmpMaxAge = time.Hour
	// pruneGrace protects renditions handed out very recently from eviction,
	// so a request that just finished waiting can still open its file.
	pruneGrace = time.Minute
	// implausibleShrink is the up-front size check: a source more than this
	// many times larger than the cache budget is refused without running
	// ffmpeg. Re-encoding legacy video to H.264 at these settings typically
	// shrinks it to between a half and a quarter (DVD-era MPEG-2 being the
	// best case); nothing realistic shrinks eightfold, so such a rendition
	// could never fit. Sources between one and eight times the budget are
	// tried once: "-fs" stops them at the budget and the result is
	// remembered (see ErrTooLarge). Stream copies, whose output is about the
	// size of the source, are refused by the runner before it starts.
	implausibleShrink = 8
)

// Rendition file names are "m<mediaID>-<srcSize>-<srcMtimeNs>-v<profile>"
// plus extension; temporary files append ".<random>.tmp". The cache only
// ever deletes names matching these patterns, so unrelated files in the
// directory are never touched.
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
	// renditions first. Files of running jobs count towards it, and no
	// single rendition may be larger.
	MaxBytes        int64
	WaitLimit       time.Duration
	JobTimeout      time.Duration
	MaxConcurrent   int
	MaxPerRequester int
	MaxInFlight     int
	MinFreeBytes    int64
	SourceSettle    time.Duration
	// FreeSpace reports the free bytes of the volume holding a directory.
	// Tests replace it; the default asks the operating system.
	FreeSpace func(dir string) (int64, error)
}

// withDefaults fills unset options.
func (o Options) withDefaults() Options {
	setDuration := func(d *time.Duration, def time.Duration) {
		if *d <= 0 {
			*d = def
		}
	}
	setInt := func(n *int, def int) {
		if *n <= 0 {
			*n = def
		}
	}
	setDuration(&o.WaitLimit, DefaultWaitLimit)
	setDuration(&o.JobTimeout, DefaultJobTimeout)
	setDuration(&o.SourceSettle, DefaultSourceSettle)
	setInt(&o.MaxConcurrent, DefaultMaxConcurrent)
	setInt(&o.MaxPerRequester, DefaultMaxPerRequester)
	setInt(&o.MaxInFlight, DefaultMaxInFlight)
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

// Cache produces renditions on demand, deduplicates concurrent requests for
// the same source, and keeps the on-disk cache bounded.
//
// The LRU clock and the failure backoff live in memory, per process. That
// fits the single-replica deployment; when two instances share the directory
// for a moment (rolling update) each evicts by its own view, falling back to
// file modification times for renditions it has not served itself.
type Cache struct {
	// ctx is derived from the application context: transcodes deliberately
	// outlive the HTTP request that started them (the client will retry and
	// pick up the finished file) but stop on Close or application shutdown.
	ctx    context.Context
	cancel context.CancelFunc
	runner Runner
	clk    clock.Clock
	logger *slog.Logger
	opts   Options
	sem    chan struct{}
	// closing is closed by Close; it releases waiting requests.
	closing chan struct{}
	wg      sync.WaitGroup // one count per job goroutine, see WaitTimeout

	// mu guards the fields below. It is never held across file I/O, so a
	// hung (network) filesystem cannot block unrelated requests.
	mu       sync.Mutex
	closed   bool
	jobs     map[string]*job      // in-flight jobs by rendition name
	lastUse  map[string]time.Time // LRU clock by rendition name
	failures map[int64]failure    // negative cache by media id
}

// NewCache creates a Cache. baseCtx bounds the lifetime of background
// transcodes; on shutdown call Close and then WaitTimeout.
func NewCache(baseCtx context.Context, runner Runner, clk clock.Clock, logger *slog.Logger, opts Options) *Cache {
	opts = opts.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(baseCtx)
	return &Cache{
		ctx:      ctx,
		cancel:   cancel,
		runner:   runner,
		clk:      clk,
		logger:   logger,
		opts:     opts,
		sem:      make(chan struct{}, opts.MaxConcurrent),
		closing:  make(chan struct{}),
		jobs:     make(map[string]*job),
		lastUse:  make(map[string]time.Time),
		failures: make(map[int64]failure),
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
	st, err := statSource(src)
	if err != nil {
		c.logger.Warn("transcode source unreadable", "media_id", src.MediaID, "source", src.Path, "err", err)
		return Rendition{}, ErrSourceMissing
	}
	if r, ok := c.lookup(st.name, src.Kind); ok {
		return r, nil
	}
	// Starting ffmpeg on a file that is still being written would transcode
	// a truncated source (and be thrown away); wait until it settled. A
	// modification time in the future (wrong camera clock, clock skew on a
	// network mount) says nothing about recent writes and counts as
	// settled — otherwise such a file would never play.
	if age := c.clk.Now().Sub(st.modTime); age >= 0 && age < c.opts.SourceSettle {
		return Rendition{}, ErrSourceChanged
	}

	j, err := c.jobFor(st.name, src)
	if err != nil {
		return Rendition{}, err
	}
	if err := c.wait(ctx, j); err != nil {
		return Rendition{}, err
	}
	if r, ok := c.lookup(st.name, src.Kind); ok {
		return r, nil
	}
	// Finished but gone again (evicted by another instance, removed with its
	// media item): the caller retries and a new job starts.
	return Rendition{}, ErrPending
}

// Prune deletes abandoned temporary files and then the least recently used
// renditions until the cache fits Options.MaxBytes. It runs after every
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
	delete(c.failures, mediaID)
	c.mu.Unlock()
	return c.removeFiles(func(name string) bool {
		return hasMediaPrefix(name, mediaID) && (renditionRe.MatchString(name) || tmpRe.MatchString(name))
	})
}

// Close stops the cache: no new jobs are accepted (Ensure returns
// ErrClosed), requests waiting for a job return at once, and running jobs
// are cancelled, which kills their ffmpeg processes. Follow it with
// WaitTimeout. Close is idempotent.
func (c *Cache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	// Set under the lock jobFor takes before wg.Add, so no job can be
	// added once WaitTimeout has started waiting.
	c.closed = true
	close(c.closing)
	c.cancel()
}

// WaitTimeout waits until every transcode goroutine has returned, at most
// for d. It reports whether all of them finished. Shutdown must not hang on
// a job stuck in filesystem I/O, so the caller proceeds either way.
func (c *Cache) WaitTimeout(d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
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

// sourceState is what a stat of the source tells the cache.
type sourceState struct {
	// name is the rendition file name. It encodes the source's size and
	// mtime, so replacing or editing the source yields a new name and the
	// old rendition is no longer served.
	name    string
	size    int64
	modTime time.Time
}

// statSource stats the source and derives its rendition name.
func statSource(src Source) (sourceState, error) {
	info, err := os.Stat(src.Path)
	if err != nil {
		return sourceState{}, err
	}
	if !info.Mode().IsRegular() {
		return sourceState{}, fmt.Errorf("%s is not a regular file", src.Path)
	}
	// The mtime is formatted unsigned so pre-1970 timestamps cannot put a
	// minus sign into the name and break the name patterns.
	name := fmt.Sprintf("%s%d-%d-%s%s", mediaPrefix(src.MediaID), info.Size(), uint64(info.ModTime().UnixNano()), profileVersion, src.Kind.ext())
	return sourceState{name: name, size: info.Size(), modTime: info.ModTime()}, nil
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

// jobFor returns the in-flight job for name, starting one unless the cache
// is closed or the limits or the negative cache forbid it. It does no file
// I/O under the lock; the job itself re-checks whether the rendition
// appeared in the meantime.
func (c *Cache) jobFor(name string, src Source) (*job, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, ErrClosed
	}
	if j, ok := c.jobs[name]; ok {
		return j, nil
	}
	if err := c.backoffError(src.MediaID, name); err != nil {
		return nil, err
	}
	if len(c.jobs) >= c.opts.MaxInFlight || c.requesterJobs(src.Requester) >= c.opts.MaxPerRequester {
		return nil, ErrBusy
	}

	ctx, cancel := context.WithCancel(c.ctx)
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

// wait blocks until the job finishes, ctx is cancelled, the cache closes, or
// the wait limit elapses.
func (c *Cache) wait(ctx context.Context, j *job) error {
	timer := time.NewTimer(c.opts.WaitLimit)
	defer timer.Stop()
	select {
	case <-j.done:
		return j.err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closing:
		return ErrClosed
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

// produce writes the rendition to a uniquely named temporary file and
// renames it into place, so readers never observe a partial rendition and
// two instances sharing the directory never write the same file.
func (c *Cache) produce(ctx context.Context, name string, src Source) error {
	if err := os.MkdirAll(c.opts.Dir, 0o755); err != nil {
		return fmt.Errorf("create transcode cache dir: %w", err)
	}
	maxBytes, err := c.makeRoom(ctx, src)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(c.opts.Dir, name+".*.tmp")
	if err != nil {
		return fmt.Errorf("create rendition temp file: %w", err)
	}
	tmp := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(tmp) }() // no-op once renamed

	if err := c.runner.Transcode(ctx, Job{Source: src, Output: tmp, MaxBytes: maxBytes}); err != nil {
		return err
	}
	info, err := os.Stat(tmp)
	if err != nil || info.Size() == 0 {
		return errors.New("transcode produced no output")
	}
	// ffmpeg stops quietly (exit status 0) when "-fs" is reached, leaving a
	// truncated file that is at least as large as the cap. Never publish it.
	if info.Size() >= maxBytes {
		return c.capExceeded(maxBytes)
	}
	// A source that changed during the run (upload or copy still in
	// progress) would leave a rendition of a truncated file under a name
	// nobody asks for any more; discard it.
	if cur, err := statSource(src); err != nil || cur.name != name {
		return ErrSourceChanged
	}
	if err := os.Rename(tmp, filepath.Join(c.opts.Dir, name)); err != nil {
		return fmt.Errorf("publish rendition: %w", err)
	}
	return nil
}

// capExceeded classifies an output that reached its size cap. When the cap
// was the cache budget, the rendition can never fit (ErrTooLarge, which is
// remembered); when the volume's free space set a lower cap, the disk is
// simply full right now (ErrNoSpace, which is not).
func (c *Cache) capExceeded(maxBytes int64) error {
	if maxBytes >= c.opts.MaxBytes {
		return fmt.Errorf("%w of %d bytes", ErrTooLarge, c.opts.MaxBytes)
	}
	return fmt.Errorf("%w: rendition needs more than the %d bytes available", ErrNoSpace, maxBytes)
}

// makeRoom returns the size the new rendition may have at most: what the
// volume can give while keeping its free-space reserve, and never more than
// the whole cache budget. The cap is enforced while ffmpeg writes
// (Job.MaxBytes), because the size of a rendition cannot be predicted: video
// usually shrinks, but low-bitrate audio can grow several times.
//
// Nothing is evicted for a job up front unless the volume really lacks the
// space. A job that then fails has cost other renditions nothing; a job that
// succeeds triggers the regular prune afterwards (cleanupAfter), which
// brings the cache back under its budget.
func (c *Cache) makeRoom(ctx context.Context, src Source) (int64, error) {
	st, err := statSource(src)
	if err != nil {
		return 0, ErrSourceChanged
	}
	if st.size/implausibleShrink > c.opts.MaxBytes {
		return 0, fmt.Errorf("%w of %d bytes: source has %d bytes", ErrTooLarge, c.opts.MaxBytes, st.size)
	}
	// The source size, limited to the budget, is the most a job is assumed
	// to need — never evict more than it could ever use.
	want := min(st.size, c.opts.MaxBytes)
	avail, err := c.available()
	if err != nil {
		return 0, err
	}
	if avail < want {
		if err := c.prune(ctx, want-avail); err != nil {
			c.logger.Warn("transcode cache prune", "err", err)
		}
		if avail, err = c.available(); err != nil {
			return 0, err
		}
	}
	if avail <= 0 {
		return 0, fmt.Errorf("%w: free space is below the reserve of %d bytes", ErrNoSpace, c.opts.MinFreeBytes)
	}
	return min(avail, c.opts.MaxBytes), nil
}

// available returns how many bytes the cache volume can still take while
// keeping the free-space reserve (negative when it is already below it).
func (c *Cache) available() (int64, error) {
	free, err := c.opts.FreeSpace(c.opts.Dir)
	if err != nil {
		return 0, fmt.Errorf("check transcode cache free space: %w", err)
	}
	return free - c.opts.MinFreeBytes, nil
}

// cleanupAfter drops renditions of older versions of the same source.
// "Older" is decided by looking at the source again: only the rendition
// matching its current size and mtime is kept, so a job that finishes late
// can never delete the output of a newer job.
func (c *Cache) cleanupAfter(ctx context.Context, src Source) {
	cur, err := statSource(src)
	if err != nil {
		return
	}
	err = c.removeFiles(func(name string) bool {
		return hasMediaPrefix(name, src.MediaID) && renditionRe.MatchString(name) && name != cur.name
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
