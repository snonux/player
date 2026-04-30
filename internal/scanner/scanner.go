// Package scanner implements media library scanning logic.
package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/play/internal/clock"
	"codeberg.org/snonux/play/internal/model"
	"codeberg.org/snonux/play/internal/probe"
	"codeberg.org/snonux/play/internal/repository"
	"codeberg.org/snonux/play/internal/thumb"
)

// Scanner defines the filesystem scanning contract.
type Scanner interface {
	Scan(ctx context.Context, root string) error
}

// FSScanner recursively scans media root for sets and media files.
type FSScanner struct {
	store     repository.ScannerStore
	prober    probe.Prober
	thumbGen  thumb.Generator
	clock     clock.Clock
	mediaRoot string
	fs        FS
}

// NewFSScanner creates a filesystem scanner with injected dependencies.
func NewFSScanner(store repository.ScannerStore, prober probe.Prober, thumbGen thumb.Generator, clk clock.Clock, mediaRoot string) Scanner {
	return &FSScanner{
		store:     store,
		prober:    prober,
		thumbGen:  thumbGen,
		clock:     clk,
		mediaRoot: mediaRoot,
		fs:        osFS{},
	}
}

// Scan walks immediate subdirectories of root, treating each as a set.
func (s *FSScanner) Scan(ctx context.Context, root string) error {
	entries, err := s.fs.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read media root %q: %w", root, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		setPath := filepath.Join(root, entry.Name())
		if err := s.scanSet(ctx, root, setPath); err != nil {
			return err
		}
	}
	return nil
}

func (s *FSScanner) scanSet(ctx context.Context, root, setPath string) error {
	setName := filepath.Base(setPath)
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

	// Build map of existing media for quick lookup.
	existing := make(map[string]bool)
	mediaList, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return fmt.Errorf("list media for set %q: %w", setName, err)
	}
	for _, m := range mediaList {
		existing[m.RelPath] = true
	}

	// Walk set directory recursively.
	walkErr := s.fs.WalkDir(setPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %q: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		if !isMediaFile(path) {
			return nil
		}
		relPath, err := filepath.Rel(setPath, path)
		if err != nil {
			return fmt.Errorf("rel path for %q: %w", path, err)
		}
		if existing[relPath] {
			return nil
		}

		info, err := s.fs.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}

		meta, err := s.prober.Probe(ctx, path)
		if err != nil {
			return fmt.Errorf("probe %q: %w", path, err)
		}
		meta.FileSizeBytes = info.Size()

		mediaType := mediaTypeFromExt(path)
		var thumbnailPath string
		if mediaType == model.MediaTypeVideo {
			thumbDir := filepath.Join(setPath, ".thumbnails")
			if err := s.fs.MkdirAll(thumbDir, 0o755); err != nil {
				return fmt.Errorf("mkdir thumbnails %q: %w", thumbDir, err)
			}
			thumbName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".jpg"
			thumbnailPath = filepath.Join(thumbDir, thumbName)
			if err := s.thumbGen.Generate(ctx, path, thumbnailPath, meta.Duration); err != nil {
				return fmt.Errorf("thumbnail %q: %w", path, err)
			}
		}

		media := &model.Media{
			SetID:         setID,
			RelPath:       relPath,
			FileName:      filepath.Base(path),
			AbsPath:       path,
			Type:          mediaType,
			Duration:      meta.Duration,
			Codec:         meta.Codec,
			Resolution:    meta.Resolution,
			Bitrate:       meta.Bitrate,
			FileSizeBytes: meta.FileSizeBytes,
			ThumbnailPath: thumbnailPath,
			CreatedAt:     s.clock.Now(),
		}

		if _, err := s.store.CreateMedia(ctx, media); err != nil {
			return fmt.Errorf("create media %q: %w", path, err)
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("scan set %q: %w", setName, walkErr)
	}
	return nil
}

var mediaExtensions = map[string]struct{}{
	".mp4": {}, ".mkv": {}, ".avi": {}, ".mov": {}, ".webm": {},
	".mp3": {}, ".flac": {}, ".wav": {}, ".aac": {}, ".ogg": {}, ".m4a": {}, ".opus": {},
}

func isMediaFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := mediaExtensions[ext]
	return ok
}

func mediaTypeFromExt(path string) model.MediaType {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".webm":
		return model.MediaTypeVideo
	default:
		return model.MediaTypeAudio
	}
}
