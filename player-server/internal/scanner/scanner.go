// Package scanner implements media library scanning logic.
package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// Scanner defines the filesystem scanning contract.
type Scanner interface {
	Scan(ctx context.Context, root string, progress *model.ScanProgress) error
}

// FSScanner recursively scans media root for sets and media files.
//
// The scanner does not own thumbnail policy: it delegates path derivation,
// directory creation, generator invocation, and failure-tolerant warning
// to a thumb.Maker. This keeps SRP intact — FSScanner orchestrates the
// scan, thumb.Maker decides how thumbnails get produced on disk.
//
// Scan itself is orchestrated by delegating to focused collaborators:
//   - fileDiscoverer: walks the filesystem to find media files and cover images
//   - probeWorker:    runs ffprobe in parallel workers to build media records,
//     and regenerates thumbnails stored under an older naming scheme
//   - scanWriter:     persists probed results to the database
//   - thumbSwitcher:  points migrated rows at their new thumbnail and deletes
//     the old file (driven by the scanWriter)
type FSScanner struct {
	store     repository.ScannerStore
	prober    probe.Prober
	thumbMkr  thumb.Maker
	clock     clock.Clock
	mediaRoot string
	fs        FS
	// remover deletes thumbnails the migration made obsolete. It is
	// separate from fs because nothing else in a scan may delete files.
	remover fileRemover
	logger  *slog.Logger
	workers int
	// scanMu serialises Scan calls, see Scan.
	scanMu sync.Mutex
}

// NewFSScanner creates a filesystem scanner with injected dependencies.
// A default thumb.FSMaker is constructed from thumbGen so existing callers
// keep working without having to know about the Maker interface.
func NewFSScanner(store repository.ScannerStore, prober probe.Prober, thumbGen thumb.Generator, clk clock.Clock, mediaRoot string) *FSScanner {
	return NewFSScannerWithLogger(store, prober, thumbGen, clk, mediaRoot, slog.Default())
}

// NewFSScannerWithLogger creates a filesystem scanner with an injected logger.
// Like NewFSScanner this wraps thumbGen in a default thumb.FSMaker; callers
// that want a custom Maker should use NewFSScannerWithMaker instead.
func NewFSScannerWithLogger(store repository.ScannerStore, prober probe.Prober, thumbGen thumb.Generator, clk clock.Clock, mediaRoot string, logger *slog.Logger) *FSScanner {
	if logger == nil {
		logger = slog.Default()
	}
	maker := thumb.NewFSMaker(thumbGen, nil, logger)
	return NewFSScannerWithMaker(store, prober, maker, clk, mediaRoot, logger)
}

// NewFSScannerWithMaker creates a filesystem scanner with an explicit
// thumb.Maker. Production wiring (cmd/player/main.go) prefers this form
// so the Maker can be constructed once and shared with any other
// component that needs to produce thumbnails consistently.
func NewFSScannerWithMaker(store repository.ScannerStore, prober probe.Prober, maker thumb.Maker, clk clock.Clock, mediaRoot string, logger *slog.Logger) *FSScanner {
	if logger == nil {
		logger = slog.Default()
	}
	return &FSScanner{
		store:     store,
		prober:    prober,
		thumbMkr:  maker,
		clock:     clk,
		mediaRoot: mediaRoot,
		fs:        osFS{},
		remover:   osFS{},
		logger:    logger,
		workers:   runtime.NumCPU(),
	}
}

func (s *FSScanner) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// Scan walks immediate subdirectories of root, treating each as a set.
// It orchestrates the fileDiscoverer, probeWorker, scanWriter and
// thumbSwitcher collaborators: new files are indexed, rows whose file is
// gone are soft-deleted, and thumbnails stored under an older naming scheme
// are migrated (see thumb_migrate.go).
//
// Scans on one FSScanner run one at a time. A rescan triggered while another
// is running cancels that one but does not wait for it, so without the lock
// the two would briefly overlap, each working from its own snapshot of the
// rows: both could insert the same new file or migrate the same thumbnail.
// The cancelled scan stops quickly, then the new one starts from a fresh
// snapshot.
func (s *FSScanner) Scan(ctx context.Context, root string, progress *model.ScanProgress) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if err := ctx.Err(); err != nil {
		// Superseded or shut down while waiting for the previous scan.
		return err
	}

	entries, err := s.fs.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read media root %q: %w", root, err)
	}

	// Count total sets for progress reporting.
	var setCount int
	for _, entry := range entries {
		if isSetDir(entry) {
			setCount++
		}
	}
	if progress != nil {
		progress.Start(setCount)
	}
	s.log().Info("scanner scan started", "root", root, "sets", setCount)

	for _, entry := range entries {
		if !isSetDir(entry) {
			continue
		}
		setPath := filepath.Join(root, entry.Name())
		if err := s.scanSet(ctx, root, setPath, progress); err != nil {
			return err
		}
		if progress != nil {
			progress.IncrementSet()
		}
	}
	s.log().Info("scanner scan finished", "root", root, "sets", setCount)
	return nil
}

// isSetDir reports whether a media root entry is scanned as a set: every
// directory except one named .thumbnails. That name is reserved for
// generated thumbnails (the media root gets one when a file is uploaded
// straight into it), and indexing its content as media would make the
// thumbnails look like source files.
func isSetDir(entry os.DirEntry) bool {
	return entry.IsDir() && entry.Name() != thumb.DirName
}

// ensureSet returns the set ID for the given root/relative paths, creating the set if necessary.
func (s *FSScanner) ensureSet(ctx context.Context, root, setPath string) (int64, string, error) {
	setName := filepath.Base(setPath)
	relRoot, err := filepath.Rel(root, setPath)
	if err != nil {
		relRoot = setName
	}

	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("list sets for %q: %w", setName, err)
	}

	for i := range sets {
		if sets[i].RootPath == relRoot {
			if isPodcastRoot(relRoot) && !sets[i].IsPodcast {
				sets[i].IsPodcast = true
				if err := s.store.UpdateSet(ctx, &sets[i]); err != nil {
					return 0, "", fmt.Errorf("update podcast set %q: %w", setName, err)
				}
			}
			return sets[i].ID, setName, nil
		}
	}

	newSet := &model.Set{
		Name:      setName,
		RootPath:  relRoot,
		IsPodcast: isPodcastRoot(relRoot),
		CreatedAt: s.clock.Now(),
	}
	id, err := s.store.CreateSet(ctx, newSet)
	if err != nil {
		return 0, "", fmt.Errorf("create set %q: %w", setName, err)
	}
	return id, setName, nil
}

func isPodcastRoot(rootPath string) bool {
	return strings.EqualFold(filepath.ToSlash(rootPath), "podcast")
}

// loadExistingMedia builds a lookup map of existing media keyed by relPath.
// IncludeDeleted = true so soft-deleted rows show up in the dedup map; if we
// omitted them, probeFile would treat the file as new and the writer would
// hit the UNIQUE(set_id, rel_path) constraint, failing the whole scan.
func (s *FSScanner) loadExistingMedia(ctx context.Context, setID int64, setName string) (map[string]model.Media, error) {
	existing := make(map[string]model.Media)
	mediaList, err := s.store.ListMedia(ctx, repository.MediaFilter{
		SetID:          &setID,
		IncludeDeleted: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list media for set %q: %w", setName, err)
	}
	for _, m := range mediaList {
		existing[m.RelPath] = m
	}
	return existing, nil
}

// reconcileOrphans soft-deletes media rows whose underlying file is no
// longer present on disk. seenRel is the set of relPaths produced by the
// current scan; any active media row in existing whose key is NOT in
// seenRel had its file deleted between scans. Soft-deleted rows are left
// alone so the soft-delete state survives the rescan.
func (s *FSScanner) reconcileOrphans(ctx context.Context, existing map[string]model.Media, seenRel map[string]struct{}, setName string) {
	for relPath, media := range existing {
		if _, ok := seenRel[relPath]; ok {
			continue
		}
		if media.DeletedAt != nil {
			// Already soft-deleted; nothing to reconcile.
			continue
		}
		if err := s.store.SoftDeleteMedia(ctx, media.ID); err != nil {
			s.log().Warn("scanner orphan soft-delete failed", "set", setName, "rel_path", relPath, "id", media.ID, "err", err)
			continue
		}
		s.log().Info("scanner soft-deleted orphan", "set", setName, "rel_path", relPath, "id", media.ID)
	}
}

// updateAudioThumbnails patches existing audio tracks when a new cover image appears.
func (s *FSScanner) updateAudioThumbnails(ctx context.Context, mediaList []model.Media, coverImages map[string]string, setPath string) {
	for _, m := range mediaList {
		if m.Type != model.MediaTypeAudio || m.ThumbnailPath != "" {
			continue
		}
		candidate := findCoverImage(m.AbsPath, coverImages, setPath)
		if candidate != "" && candidate != m.ThumbnailPath {
			if err := s.store.UpdateMediaThumbnail(ctx, m.ID, candidate); err != nil {
				s.log().Warn("scanner failed to update thumbnail", "file", m.FileName, "err", err)
			}
		}
	}
}

// setScan carries the state of one set's scan between its phases.
type setScan struct {
	id   int64
	name string
	path string
	// existing holds the set's rows as of the start of the scan, keyed by
	// relPath; soft-deleted rows are included.
	existing    map[string]model.Media
	coverImages map[string]string
	files       []string
	// stale holds the relPaths of the rows whose thumbnail must be
	// regenerated because it is stored under an older naming scheme.
	stale map[string]struct{}
}

// scanSet scans a single set in three phases: discoverSet finds the files
// and brings the already indexed rows up to date, probeAndStore probes and
// persists the new files, and finally audio tracks pick up cover images
// that appeared since they were indexed.
func (s *FSScanner) scanSet(ctx context.Context, root, setPath string, progress *model.ScanProgress) error {
	setID, setName, err := s.ensureSet(ctx, root, setPath)
	if err != nil {
		return err
	}
	s.log().Info("scanner set started", "name", setName, "path", setPath)
	if progress != nil {
		progress.SetCurrentSet(setName)
	}

	existing, err := s.loadExistingMedia(ctx, setID, setName)
	if err != nil {
		return err
	}
	sc := &setScan{id: setID, name: setName, path: setPath, existing: existing}
	if err := s.discoverSet(ctx, sc, progress); err != nil {
		return fmt.Errorf("scan set %q: %w", setName, err)
	}
	newFiles, err := s.probeAndStore(ctx, sc, progress)
	if err != nil {
		return fmt.Errorf("scan set %q: %w", setName, err)
	}

	mediaList, _ := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	s.updateAudioThumbnails(ctx, mediaList, sc.coverImages, setPath)

	s.log().Info("scanner set completed", "name", setName, "existing_media", len(existing), "new_media", newFiles)
	return nil
}

// discoverSet uses fileDiscoverer to collect the set's media files and
// cover images into sc, then reconciles the already indexed rows with what
// is on disk: rows whose file disappeared are soft-deleted, and the rows
// whose thumbnail is stored under an older naming scheme are noted in
// sc.stale for probeAndStore to migrate.
func (s *FSScanner) discoverSet(ctx context.Context, sc *setScan, progress *model.ScanProgress) error {
	disc := newFileDiscoverer(s.fs)
	sc.coverImages = disc.gatherCoverImages(sc.path)
	files, err := disc.Discover(sc.path)
	if err != nil {
		return err
	}
	sc.files = files

	// Build the set of relPaths we just saw on disk so reconcileOrphans
	// can soft-delete media rows whose files disappeared between scans.
	seenRel := make(map[string]struct{}, len(files))
	for _, p := range files {
		if rel, relErr := filepath.Rel(sc.path, p); relErr == nil {
			seenRel[filepath.ToSlash(rel)] = struct{}{}
		}
	}
	s.reconcileOrphans(ctx, sc.existing, seenRel, sc.name)

	sc.stale = staleThumbnails(sc.existing, seenRel, sc.path)
	if len(sc.stale) > 0 {
		s.log().Info("scanner migrating thumbnails", "set", sc.name, "rows", len(sc.stale))
	}

	if progress != nil {
		progress.AddFilesTotal(len(files))
	}
	return nil
}

// probeAndStore runs sc.files through the probe/persist pipeline and
// returns the number of media rows created. probeWorkers probe new files
// and regenerate stale thumbnails in parallel; a single scanWriter
// goroutine persists the results, because SQLite does not take concurrent
// writes. The first error cancels the rest.
func (s *FSScanner) probeAndStore(ctx context.Context, sc *setScan, progress *model.ScanProgress) (int32, error) {
	workers := max(s.workers, 1)
	pathChan := make(chan string, len(sc.files))
	resultChan := make(chan fileResult, workers)

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, 1)
	var errOnce sync.Once
	sendErr := func(err error) {
		errOnce.Do(func() { errChan <- err; cancel() })
	}

	workerWg := s.startProbeWorkers(ctx, scanCtx, sc, workers, pathChan, resultChan, progress, sendErr)

	// scanWriter persists results sequentially to avoid SQLite write conflicts.
	thumbs := newThumbSwitcher(s.store, s.remover, s.log(), sc.existing, sc.files)
	sw := newScanWriter(s.store, thumbs, s.log())
	var newFiles int32
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		sw.run(ctx, scanCtx, resultChan, sc.name, sc.path, &newFiles, sendErr)
	}()

	feedPaths(scanCtx, sc.files, pathChan)

	// Wait for workers to finish, then close the result channel so the
	// writer exits, and wait for it to drain all results.
	workerWg.Wait()
	close(resultChan)
	writerWg.Wait()

	select {
	case err := <-errChan:
		return newFiles, err
	default:
		return newFiles, nil
	}
}

// startProbeWorkers launches n probeWorker goroutines that probe the paths
// arriving on pathChan concurrently and send their results to resultChan.
// The returned WaitGroup is done once pathChan is closed and drained.
func (s *FSScanner) startProbeWorkers(
	ctx, scanCtx context.Context,
	sc *setScan,
	n int,
	pathChan <-chan string,
	resultChan chan<- fileResult,
	progress *model.ScanProgress,
	sendErr func(error),
) *sync.WaitGroup {
	pw := newProbeWorker(s.prober, s.thumbMkr, s.fs, s.clock, s.log(), sc.stale)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pw.run(ctx, scanCtx, pathChan, resultChan, sc.path, sc.id, sc.name, sc.existing, sc.coverImages, progress, sendErr)
		}()
	}
	return &wg
}

// feedPaths sends files to the worker pool and closes pathChan, which lets
// the workers return. It stops early once scanCtx is cancelled.
func feedPaths(scanCtx context.Context, files []string, pathChan chan<- string) {
	defer close(pathChan)
	for _, path := range files {
		select {
		case pathChan <- path:
		case <-scanCtx.Done():
			return
		}
	}
}

// collectFiles walks the set and returns the absolute paths of all supported media files.
// Kept for backward compatibility with existing tests that call it directly.
func (s *FSScanner) collectFiles(setPath string) ([]string, error) {
	return newFileDiscoverer(s.fs).Discover(setPath)
}

func findCoverImage(filePath string, coverImages map[string]string, setPath string) string {
	for dir := filepath.Dir(filePath); len(dir) >= len(setPath); dir = filepath.Dir(dir) {
		if coverRel, ok := coverImages[dir]; ok {
			return filepath.Join(setPath, coverRel)
		}
		if dir == setPath {
			break
		}
	}
	return ""
}
