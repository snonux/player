// Package scanner implements media library scanning logic.
package scanner

import (
	"context"
	"fmt"
	"log/slog"
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
// Scan itself is orchestrated by delegating to three focused collaborators:
//   - fileDiscoverer: walks the filesystem to find media files and cover images
//   - probeWorker:    runs ffprobe in parallel workers to build media records
//   - scanWriter:     persists probed results to the database
type FSScanner struct {
	store     repository.ScannerStore
	prober    probe.Prober
	thumbMkr  thumb.Maker
	clock     clock.Clock
	mediaRoot string
	fs        FS
	logger    *slog.Logger
	workers   int
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
// It orchestrates fileDiscoverer, probeWorker, and scanWriter collaborators.
func (s *FSScanner) Scan(ctx context.Context, root string, progress *model.ScanProgress) error {
	entries, err := s.fs.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read media root %q: %w", root, err)
	}

	// Count total sets for progress reporting.
	var setCount int
	for _, entry := range entries {
		if entry.IsDir() {
			setCount++
		}
	}
	if progress != nil {
		progress.Start(setCount)
	}
	s.log().Info("scanner scan started", "root", root, "sets", setCount)

	for _, entry := range entries {
		if !entry.IsDir() {
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

// scanSet scans a single set using fileDiscoverer, probeWorker, and scanWriter.
// fileDiscoverer collects the file list; probeWorker probes files in parallel;
// scanWriter persists results to SQLite via a single writer goroutine.
func (s *FSScanner) scanSet(ctx context.Context, root, setPath string, progress *model.ScanProgress) error {
	workers := s.workers
	if workers <= 0 {
		workers = 1
	}
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

	// Use fileDiscoverer to collect files and cover images.
	disc := newFileDiscoverer(s.fs)
	coverImages := disc.gatherCoverImages(setPath)
	files, err := disc.Discover(setPath)
	if err != nil {
		return fmt.Errorf("scan set %q: %w", setName, err)
	}

	// Build the set of relPaths we just saw on disk so reconcileOrphans
	// can soft-delete media rows whose files disappeared between scans.
	seenRel := make(map[string]struct{}, len(files))
	for _, p := range files {
		if rel, relErr := filepath.Rel(setPath, p); relErr == nil {
			seenRel[filepath.ToSlash(rel)] = struct{}{}
		}
	}
	s.reconcileOrphans(ctx, existing, seenRel, setName)

	if progress != nil {
		progress.AddFilesTotal(len(files))
	}

	pathChan := make(chan string, len(files))
	resultChan := make(chan fileResult, workers)

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, 1)
	var errOnce sync.Once
	sendErr := func(err error) {
		errOnce.Do(func() { errChan <- err; cancel() })
	}

	// probeWorker probes files concurrently and sends results to resultChan.
	pw := newProbeWorker(s.prober, s.thumbMkr, s.fs, s.clock, s.log())
	var workerWg sync.WaitGroup
	for i := 0; i < workers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			pw.run(ctx, scanCtx, pathChan, resultChan, setPath, setID, setName, existing, coverImages, progress, sendErr)
		}()
	}

	// scanWriter persists results sequentially to avoid SQLite write conflicts.
	sw := newScanWriter(s.store, s.log())
	var newFiles int32
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		sw.run(ctx, scanCtx, resultChan, setName, setPath, &newFiles, sendErr)
	}()

	// Feed the worker pool.
	for _, path := range files {
		if scanCtx.Err() != nil {
			break
		}
		select {
		case pathChan <- path:
		case <-scanCtx.Done():
			break
		}
	}
	close(pathChan)

	// Wait for workers to finish, then close the result channel so the writer exits.
	workerWg.Wait()
	close(resultChan)

	// Wait for the writer to drain all results.
	writerWg.Wait()

	var firstErr error
	select {
	case firstErr = <-errChan:
	default:
	}

	if firstErr != nil {
		return fmt.Errorf("scan set %q: %w", setName, firstErr)
	}

	mediaList, _ := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	s.updateAudioThumbnails(ctx, mediaList, coverImages, setPath)

	s.log().Info("scanner set completed", "name", setName, "existing_media", len(existing), "new_media", newFiles)
	return nil
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
