package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// writeService handles mutations such as upload, soft-delete and restore.
type writeService struct {
	store     repository.WriteServiceStore
	clock     clock.Clock
	mediaRoot string
	thumbGen  thumb.Generator
	prober    probe.Prober
	helper    *accessHelper
}

// NewWriteService creates a WriteService.
func NewWriteService(store repository.WriteServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober, helper *accessHelper) MediaWriteService {
	return &writeService{
		store:     store,
		clock:     clk,
		mediaRoot: mediaRoot,
		thumbGen:  thumbGen,
		prober:    prober,
		helper:    helper,
	}
}

func (s *writeService) SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.helper.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.SoftDeleteMedia(ctx, mediaID)
}

func (s *writeService) RestoreMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.helper.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.RestoreMedia(ctx, mediaID)
}

func (s *writeService) UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error) {
	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	if err := s.helper.verifySetModifyAccess(ctx, setID, userID); err != nil {
		return nil, err
	}

	if !isSupportedExtension(filename) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedExtension, filepath.Ext(filename))
	}

	dir := filepath.Clean(filepath.Join(s.mediaRoot, set.RootPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	path := s.uniqueFilename(dir, filename)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator)) {
		return nil, errors.New("invalid filename")
	}

	media, err := s.saveUploadedMedia(ctx, setID, path, data, size)
	if err != nil {
		os.Remove(path)
		return nil, err
	}

	meta, err := s.probeMedia(ctx, path)
	if err != nil {
		os.Remove(path)
		s.store.HardDeleteMedia(ctx, media.ID)
		return nil, err
	}
	media.Duration = meta.Duration
	media.Codec = meta.Codec
	media.Resolution = meta.Resolution
	media.Bitrate = meta.Bitrate
	media.Width = meta.Width
	media.Height = meta.Height
	media.EXIFCamera = meta.EXIFCamera
	media.EXIFLens = meta.EXIFLens
	media.EXIFDate = meta.EXIFDate
	media.EXIFISO = meta.EXIFISO
	media.EXIFFNumber = meta.EXIFFNumber
	media.EXIFExposure = meta.EXIFExposure
	media.EXIFFocalLength = meta.EXIFFocalLength

	if media.Type == model.MediaTypeVideo || media.Type == model.MediaTypeImage {
		if err := s.generateThumbnail(ctx, media, meta.Duration); err != nil {
			os.Remove(path)
			_ = s.store.HardDeleteMedia(ctx, media.ID)
			return nil, err
		}
	}

	if err := s.store.UpdateMedia(ctx, media); err != nil {
		os.Remove(path)
		_ = s.store.HardDeleteMedia(ctx, media.ID)
		return nil, fmt.Errorf("update media metadata: %w", err)
	}

	return media, nil
}

func (s *writeService) uniqueFilename(dir, filename string) string {
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == "" {
		return ""
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}

	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (s *writeService) saveUploadedMedia(ctx context.Context, setID int64, path string, data io.Reader, size int64) (*model.Media, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, data)
	if err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	now := s.clock.Now()
	media := &model.Media{
		SetID:         setID,
		RelPath:       filepath.Base(path),
		FileName:      filepath.Base(path),
		AbsPath:       path,
		Type:          guessMediaType(filepath.Base(path)),
		FileSizeBytes: n,
		CreatedAt:     now,
	}
	_ = size

	id, err := s.store.CreateMedia(ctx, media)
	if err != nil {
		return nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = id
	return media, nil
}

func (s *writeService) probeMedia(ctx context.Context, path string) (*model.Metadata, error) {
	if s.prober == nil {
		return &model.Metadata{}, nil
	}
	meta, err := s.prober.Probe(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe media: %w", err)
	}
	return meta, nil
}

func (s *writeService) generateThumbnail(ctx context.Context, media *model.Media, duration float64) error {
	ext := strings.ToLower(filepath.Ext(media.AbsPath))
	if ext == ".svg" {
		media.ThumbnailPath = media.AbsPath
		return nil
	}
	thumbDir := filepath.Join(filepath.Dir(media.AbsPath), ".thumbnails")
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return fmt.Errorf("mkdir thumbnails: %w", err)
	}
	thumbName := strings.TrimSuffix(filepath.Base(media.AbsPath), filepath.Ext(media.AbsPath)) + ".jpg"
	thumbnailPath := filepath.Join(thumbDir, thumbName)

	if s.thumbGen == nil {
		media.ThumbnailPath = thumbnailPath
		return nil
	}
	if err := s.thumbGen.Generate(ctx, media.AbsPath, thumbnailPath, duration); err != nil {
		return fmt.Errorf("generate thumbnail: %w", err)
	}
	media.ThumbnailPath = thumbnailPath
	return nil
}
