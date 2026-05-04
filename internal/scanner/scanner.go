// Package scanner implements media library scanning logic.
package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"

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

func (s *FSScanner) scanSet(ctx context.Context, root, setPath string, progress *model.ScanProgress) error {
	setName := filepath.Base(setPath)
	s.log().Info("scanner set started", "name", setName, "path", setPath)
	if progress != nil {
		progress.SetCurrentSet(setName)
	}
	relRoot, err := filepath.Rel(root, setPath)
	if err != nil {
		relRoot = setName
	}

	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return fmt.Errorf("list sets for %q: %w", setName, err)
	}

	var setID int64
	var set *model.Set
	for i := range sets {
		if sets[i].RootPath == relRoot {
			set = &sets[i]
			break
		}
	}
	if set == nil {
		newSet := &model.Set{
			Name:      setName,
			RootPath:  relRoot,
			CreatedAt: s.clock.Now(),
		}
		id, err := s.store.CreateSet(ctx, newSet)
		if err != nil {
			return fmt.Errorf("create set %q: %w", setName, err)
		}
		setID = id
	} else {
		setID = set.ID
	}

	// Build map of existing media for quick lookup by relPath.
	existing := make(map[string]model.Media)
	mediaList, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return fmt.Errorf("list media for set %q: %w", setName, err)
	}
	for _, m := range mediaList {
		existing[m.RelPath] = m
	}
	newFiles := 0

	// First pass: gather images per directory.
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

	// Second pass: walk for NEW media files.
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
		relPath, err := filepath.Rel(setPath, path)
		if err != nil {
			return fmt.Errorf("rel path for %q: %w", path, err)
		}
		relPath = filepath.ToSlash(relPath)
		_, alreadyExists := existing[relPath]
		s.log().Debug("scanner file checked", "set", setName, "path", relPath, "existing", alreadyExists)
		if progress != nil {
			progress.IncrementFile()
		}
		if alreadyExists {
			return nil
		}

		info, err := s.fs.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}

		meta, err := s.prober.Probe(ctx, path)
		if err != nil {
			s.log().Warn("scanner skipping unprobeable file", "path", path, "err", err)
			return nil
		}
		meta.FileSizeBytes = info.Size()

		mediaType := mediatype.TypeForExt(path)
		var thumbnailPath string
		if mediaType == model.MediaTypeVideo {
			thumbDir := filepath.Join(setPath, ".thumbnails")
			if err := s.fs.MkdirAll(thumbDir, 0o755); err != nil {
				return fmt.Errorf("mkdir thumbnails %q: %w", thumbDir, err)
			}
			thumbName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".jpg"
			thumbnailPath = filepath.Join(thumbDir, thumbName)
			if err := s.thumbGen.Generate(ctx, path, thumbnailPath, meta.Duration); err != nil {
				s.log().Warn("scanner skipping thumbnail", "path", path, "err", err)
				thumbnailPath = ""
			}
		} else if mediaType == model.MediaTypeAudio {
			thumbnailPath = findCoverImage(path, coverImages, setPath)
		} else if mediaType == model.MediaTypeImage {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".svg" {
				thumbnailPath = path
			} else {
				thumbDir := filepath.Join(setPath, ".thumbnails")
				if err := s.fs.MkdirAll(thumbDir, 0o755); err != nil {
					return fmt.Errorf("mkdir thumbnails %q: %w", thumbDir, err)
				}
				thumbName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".jpg"
				thumbnailPath = filepath.Join(thumbDir, thumbName)
				if err := s.thumbGen.Generate(ctx, path, thumbnailPath, 0); err != nil {
					s.log().Warn("scanner skipping thumbnail", "path", path, "err", err)
					thumbnailPath = ""
				}
			}
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

		if _, err := s.store.CreateMedia(ctx, media); err != nil {
			return fmt.Errorf("create media %q: %w", path, err)
		}
		newFiles++
		if newFiles == 1 || newFiles%25 == 0 {
			s.log().Info("scanner set progress", "name", setName, "new_media", newFiles, "latest", relPath)
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("scan set %q: %w", setName, walkErr)
	}

	// Third pass: update existing audio files that gained a cover image.
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
