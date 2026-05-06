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
func NewFSScanner(store repository.ScannerStore, prober probe.Prober, thumbGen thumb.Generator, clk clock.Clock, mediaRoot string) Scanner {
	return NewFSScannerWithLogger(store, prober, thumbGen, clk, mediaRoot, slog.Default())
}

// NewFSScannerWithLogger creates a filesystem scanner with an injected logger.
func NewFSScannerWithLogger(store repository.ScannerStore, prober probe.Prober, thumbGen thumb.Generator, clk clock.Clock, mediaRoot string, logger *slog.Logger) Scanner {
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
			return sets[i].ID, setName, nil
		}
	}

	newSet := &model.Set{
		Name:      setName,
		RootPath:  relRoot,
		CreatedAt: s.clock.Now(),
	}
	id, err := s.store.CreateSet(ctx, newSet)
	if err != nil {
		return 0, "", fmt.Errorf("create set %q: %w", setName, err)
	}
	return id, setName, nil
}

// loadExistingMedia builds a lookup map of existing media keyed by relPath.
func (s *FSScanner) loadExistingMedia(ctx context.Context, setID int64, setName string) (map[string]model.Media, error) {
	existing := make(map[string]model.Media)
	mediaList, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return nil, fmt.Errorf("list media for set %q: %w", setName, err)
	}
	for _, m := range mediaList {
		existing[m.RelPath] = m
	}
	return existing, nil
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

	// First pass: collect supported media files so we know the total and can
	// safely close the path channel without blocking the walk.
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
		return fmt.Errorf("scan set %q: %w", setName, walkErr)
	}

	if progress != nil {
		progress.SetFilesTotal(len(files))
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

	// Spawn worker goroutines that consume paths, probe files, and send results.
	for i := 0; i < workers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for path := range pathChan {
				if scanCtx.Err() != nil {
					continue
				}
				result, err := s.probeFile(ctx, path, setPath, setID, setName, existing, coverImages, progress)
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
		}()
	}

	var newFiles int32

	// Single writer goroutine serialises all SQLite inserts.
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		for result := range resultChan {
			if scanCtx.Err() != nil {
				continue
			}
			if _, err := s.store.CreateMedia(ctx, result.media); err != nil {
				sendErr(fmt.Errorf("create media %q: %w", result.path, err))
				continue
			}
			nf := atomic.AddInt32(&newFiles, 1)
			if nf == 1 || nf%25 == 0 {
				relPath, _ := filepath.Rel(setPath, result.path)
				s.log().Info("scanner set progress", "name", setName, "new_media", nf, "latest", filepath.ToSlash(relPath))
			}
		}
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
