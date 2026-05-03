package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

func (s *mediaService) SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.SoftDeleteMedia(ctx, mediaID)
}

func (s *mediaService) RestoreMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.RestoreMedia(ctx, mediaID)
}

func (s *mediaService) uniqueFilename(dir, filename string) string {
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

func (s *mediaService) UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error) {
	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	if err := s.verifySetModifyAccess(ctx, setID, userID); err != nil {
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

// saveUploadedMedia writes data to disk and creates a minimal media row.
func (s *mediaService) saveUploadedMedia(ctx context.Context, setID int64, path string, data io.Reader, size int64) (*model.Media, error) {
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

// probeMedia extracts metadata from the uploaded file.
func (s *mediaService) probeMedia(ctx context.Context, path string) (*model.Metadata, error) {
	if s.prober == nil {
		return &model.Metadata{}, nil
	}
	meta, err := s.prober.Probe(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe media: %w", err)
	}
	return meta, nil
}
