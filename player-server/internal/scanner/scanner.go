// Package scanner implements media library scanning logic.
package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/mediatype"
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
type FSScanner struct {
	store     repository.ScannerStore
	prober    probe.Prober
	thumbGen  thumb.Generator
	clock     clock.Clock
	mediaRoot string
	fs        FS
	logger    *slog.Logger
	workers   int
}

// NewFSScanner creates a filesystem scanner with injected dependencies.
func NewFSScanner(store repository.ScannerStore, prober probe.Prober, thumbGen thumb.Generator, clk clock.Clock, mediaRoot string) *FSScanner {
	return NewFSScannerWithLogger(store, prober, thumbGen, clk, mediaRoot, slog.Default())
}

// NewFSScannerWithLogger creates a filesystem scanner with an injected logger.
func NewFSScannerWithLogger(store repository.ScannerStore, prober probe.Prober, thumbGen thumb.Generator, clk clock.Clock, mediaRoot string, logger *slog.Logger) *FSScanner {
	if logger == nil {
		logger = slog.Default()
	}
	return &FSScanner{
		store:     store,
		prober:    prober,
		thumbGen:  thumbGen,
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
func (s *FSScanner) Scan(ctx context.Context, root string, progress *model.ScanProgress) error {
	entries, err := s.fs.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read media root %q: %w", root, err)
	}

	// Count total sets for progress.
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

// gatherCoverImages walks the set and records the first cover image per directory.
func (s *FSScanner) gatherCoverImages(setPath string) map[string]string {
	coverImages := make(map[string]string)
	_ = s.fs.WalkDir(setPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !mediatype.IsCoverImageExt(path) {
			if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != setPath {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, _ := filepath.Rel(setPath, path)
		dir := filepath.Dir(path)
		if _, ok := coverImages[dir]; !ok {
			coverImages[dir] = relPath
		}
		return nil
	})
	return coverImages
}

// thumbnailForVideo generates a thumbnail for a video file inside the set's .thumbnails directory.
func (s *FSScanner) thumbnailForVideo(ctx context.Context, path, setPath string, duration float64) (string, error) {
	thumbDir := filepath.Join(setPath, ".thumbnails")
	if err := s.fs.MkdirAll(thumbDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir thumbnails %q: %w", thumbDir, err)
	}
	thumbName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".jpg"
	thumbnailPath := filepath.Join(thumbDir, thumbName)
	if err := s.thumbGen.Generate(ctx, path, thumbnailPath, duration); err != nil {
		s.log().Warn("scanner skipping thumbnail", "path", path, "err", err)
		return "", nil
	}
	return thumbnailPath, nil
}

// thumbnailForImage generates a thumbnail for an image file inside the set's .thumbnails directory.
func (s *FSScanner) thumbnailForImage(ctx context.Context, path, setPath string) (string, error) {
	thumbDir := filepath.Join(setPath, ".thumbnails")
	if err := s.fs.MkdirAll(thumbDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir thumbnails %q: %w", thumbDir, err)
	}
	thumbName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".jpg"
	thumbnailPath := filepath.Join(thumbDir, thumbName)
	if err := s.thumbGen.Generate(ctx, path, thumbnailPath, 0); err != nil {
		s.log().Warn("scanner skipping thumbnail", "path", path, "err", err)
		return "", nil
	}
	return thumbnailPath, nil
}

// buildThumbnailPath resolves the thumbnail path for a new media file.
func (s *FSScanner) buildThumbnailPath(ctx context.Context, path, setPath string, mediaType model.MediaType, coverImages map[string]string, meta *model.Metadata) (string, error) {
	switch mediaType {
	case model.MediaTypeVideo:
		return s.thumbnailForVideo(ctx, path, setPath, meta.Duration)
	case model.MediaTypeAudio:
		return findCoverImage(path, coverImages, setPath), nil
	case model.MediaTypeImage:
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".svg" {
			return path, nil
		}
		thumbPath, err := s.thumbnailForImage(ctx, path, setPath)
		if err != nil {
			return "", err
		}
		if thumbPath != "" {
			if _, statErr := s.fs.Stat(thumbPath); statErr == nil {
				return thumbPath, nil
			}
		}
		return path, nil
	}
	return "", nil
}

// fileResult carries a successfully probed media record back to the writer.
type fileResult struct {
	media *model.Media
	path  string // absolute path for logging
}

// probeFile probes a single file and builds a media record.
// It returns nil when the file already exists or is unprobeable.
func (s *FSScanner) probeFile(ctx context.Context, path, setPath string, setID int64, setName string, existing map[string]model.Media, coverImages map[string]string, progress *model.ScanProgress) (*fileResult, error) {
	relPath, err := filepath.Rel(setPath, path)
	if err != nil {
		return nil, fmt.Errorf("rel path for %q: %w", path, err)
	}
	relPath = filepath.ToSlash(relPath)

	if progress != nil {
		progress.IncrementFile()
	}

	_, alreadyExists := existing[relPath]
	s.log().Debug("scanner file checked", "set", setName, "path", relPath, "existing", alreadyExists)
	if alreadyExists {
		return nil, nil
	}

	info, err := s.fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	meta, err := s.prober.Probe(ctx, path)
	if err != nil {
		s.log().Warn("scanner skipping unprobeable file", "path", path, "err", err)
		return nil, nil
	}
	meta.FileSizeBytes = info.Size()

	mediaType := mediatype.TypeForExt(path)
	thumbnailPath, err := s.buildThumbnailPath(ctx, path, setPath, mediaType, coverImages, meta)
	if err != nil {
		return nil, err
	}

	media := &model.Media{
		SetID:           setID,
		RelPath:         relPath,
		FileName:        filepath.Base(path),
		AbsPath:         path,
		Type:            mediaType,
		Duration:        meta.Duration,
		Codec:           meta.Codec,
		Resolution:      meta.Resolution,
		Bitrate:         meta.Bitrate,
		FileSizeBytes:   meta.FileSizeBytes,
		Width:           meta.Width,
		Height:          meta.Height,
		EXIFCamera:      meta.EXIFCamera,
		EXIFLens:        meta.EXIFLens,
		EXIFDate:        meta.EXIFDate,
		EXIFISO:         meta.EXIFISO,
		EXIFFNumber:     meta.EXIFFNumber,
		EXIFExposure:    meta.EXIFExposure,
		EXIFFocalLength: meta.EXIFFocalLength,
		ThumbnailPath:   thumbnailPath,
		CreatedAt:       s.clock.Now(),
	}

	return &fileResult{media: media, path: path}, nil
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

// scanSet scans a single set using a pool of workers for probing and a single
// writer goroutine for SQLite inserts.
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

	coverImages := s.gatherCoverImages(setPath)

	files, err := s.collectFiles(setPath)
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
	resultChan := make(chan fileResult, s.workers)

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errChan := make(chan error, 1)
	var errOnce sync.Once
	sendErr := func(err error) {
		errOnce.Do(func() { errChan <- err; cancel() })
	}

	var workerWg sync.WaitGroup
	for i := 0; i < workers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			s.probeWorkerLoop(ctx, scanCtx, pathChan, resultChan, setPath, setID, setName, existing, coverImages, progress, sendErr)
		}()
	}

	var newFiles int32
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		s.writerLoop(ctx, scanCtx, resultChan, setName, setPath, &newFiles, sendErr)
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
func (s *FSScanner) collectFiles(setPath string) ([]string, error) {
	var files []string
	walkErr := s.fs.WalkDir(setPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %q: %w", path, err)
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != setPath {
				return filepath.SkipDir
			}
			return nil
		}
		if !mediatype.IsSupportedExt(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

// probeWorkerLoop consumes file paths, probes each one, and sends the result to resultChan.
// It stops early if scanCtx is cancelled or if sendErr reports a fatal error.
func (s *FSScanner) probeWorkerLoop(
	ctx context.Context,
	scanCtx context.Context,
	pathChan <-chan string,
	resultChan chan<- fileResult,
	setPath string,
	setID int64,
	setName string,
	existing map[string]model.Media,
	coverImages map[string]string,
	progress *model.ScanProgress,
	sendErr func(error),
) {
	for path := range pathChan {
		if scanCtx.Err() != nil {
			continue
		}
		// Use scanCtx (not ctx) so ffprobe/ffmpeg subprocesses spawned by
		// probeFile cancel promptly when scanCtx is cancelled — e.g. another
		// worker failed or TriggerRescan restarted the scan. Passing the
		// parent ctx here would leave ffprobe running after cancel.
		result, err := s.probeFile(scanCtx, path, setPath, setID, setName, existing, coverImages, progress)
		if err != nil {
			sendErr(err)
			return
		}
		if result == nil {
			continue
		}
		select {
		case resultChan <- *result:
		case <-scanCtx.Done():
			return
		}
	}
}

// writerLoop reads probed results and inserts them into the store.
// It logs progress every 25 files and tracks the total newFiles count.
func (s *FSScanner) writerLoop(
	ctx context.Context,
	scanCtx context.Context,
	resultChan <-chan fileResult,
	setName string,
	setPath string,
	newFiles *int32,
	sendErr func(error),
) {
	for result := range resultChan {
		if scanCtx.Err() != nil {
			continue
		}
		if _, err := s.store.CreateMedia(ctx, result.media); err != nil {
			sendErr(fmt.Errorf("create media %q: %w", result.path, err))
			continue
		}
		nf := atomic.AddInt32(newFiles, 1)
		if nf == 1 || nf%25 == 0 {
			relPath, _ := filepath.Rel(setPath, result.path)
			s.log().Info("scanner set progress", "name", setName, "new_media", nf, "latest", filepath.ToSlash(relPath))
		}
	}
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
