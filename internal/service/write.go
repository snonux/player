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
	"codeberg.org/snonux/player/internal/mediatype"
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

	if !mediatype.IsSupportedExt(filename) {
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

	if err := ImportMediaFile(ctx, s.store, media, s.prober, s.thumbGen); err != nil {
		os.Remove(path)
		s.store.HardDeleteMedia(ctx, media.ID)
		return nil, err
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
		Type:          mediatype.TypeForExt(filepath.Base(path)),
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
