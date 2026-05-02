package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

var (
	_ MediaBrowseService   = (*mediaService)(nil)
	_ MediaWriteService    = (*mediaService)(nil)
	_ MediaShareService    = (*mediaService)(nil)
	_ MediaTagService      = (*mediaService)(nil)
	_ MediaFavoriteService = (*mediaService)(nil)
	_ MediaNoteService     = (*mediaService)(nil)
	_ MediaService         = (*mediaService)(nil)
)

// mediaService is the concrete implementation of MediaService.
type mediaService struct {
	store     repository.MediaServiceStore
	clock     clock.Clock
	mediaRoot string
	thumbGen  thumb.Generator
	prober    probe.Prober
}

// NewMediaService creates a concrete MediaService.
func NewMediaService(store repository.MediaServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober) MediaService {
	return &mediaService{
		store:     store,
		clock:     clk,
		mediaRoot: mediaRoot,
		thumbGen:  thumbGen,
		prober:    prober,
	}
}

// Sentinel errors returned by the media service layer.
var (
	ErrNotFound             = errors.New("not found")
	ErrForbidden            = errors.New("access denied")
	ErrShareNotFound        = errors.New("share not found")
	ErrShareExpired         = errors.New("share expired")
	ErrMediaNotFound        = errors.New("media not found")
	ErrUnsupportedExtension = errors.New("unsupported file extension")
	ErrAlreadyBootstrapped  = errors.New("already bootstrapped")
	ErrInvalidCredentials   = errors.New("invalid credentials")
)

// supportedExtensions lists all file extensions accepted by UploadMedia.
var supportedExtensions = map[string]struct{}{
	".mp4":  {},
	".mkv":  {},
	".avi":  {},
	".mov":  {},
	".wmv":  {},
	".flv":  {},
	".webm": {},
	".mp3":  {},
	".wav":  {},
	".flac": {},
	".aac":  {},
	".ogg":  {},
	".m4a":  {},
	".wma":  {},
	".m4b":  {},
	".opus": {},
}

func isSupportedExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := supportedExtensions[ext]
	return ok
}

func guessMediaType(name string) model.MediaType {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm":
		return model.MediaTypeVideo
	case ".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma", ".m4b", ".opus":
		return model.MediaTypeAudio
	default:
		return model.MediaTypeVideo
	}
}

// generateThumbnail creates a thumbnail for a video file.
func (s *mediaService) generateThumbnail(ctx context.Context, media *model.Media, duration float64) error {
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
