package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/thumb"
)

// ImportMediaFile probes an existing media file on disk, generates a thumbnail if
// needed, and updates the media row with extracted metadata. It is used by both
// UploadMedia (after writing an uploaded file) and the podcast downloader (after
// fetching an episode enclosure).
func ImportMediaFile(
	ctx context.Context,
	store interface {
		UpdateMedia(ctx context.Context, media *model.Media) error
	},
	media *model.Media,
	prober probe.Prober,
	thumbGen thumb.Generator,
) error {
	meta, err := probeMedia(ctx, prober, media.AbsPath)
	if err != nil {
		return fmt.Errorf("probe media: %w", err)
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
		if err := generateThumbnail(ctx, thumbGen, media, meta.Duration); err != nil {
			return fmt.Errorf("generate thumbnail: %w", err)
		}
	}

	if err := store.UpdateMedia(ctx, media); err != nil {
		return fmt.Errorf("update media metadata: %w", err)
	}
	return nil
}

// probeMedia probes a file at the given path and returns its metadata.
func probeMedia(ctx context.Context, prober probe.Prober, path string) (*model.Metadata, error) {
	if prober == nil {
		return &model.Metadata{}, nil
	}
	meta, err := prober.Probe(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe media: %w", err)
	}
	return meta, nil
}

// generateThumbnail creates a thumbnail for video and image media.
// Thumbnail directory + filename are derived via internal/thumb to share the
// on-disk convention with the scanner and RegenerateThumbnail.
func generateThumbnail(ctx context.Context, thumbGen thumb.Generator, media *model.Media, duration float64) error {
	ext := strings.ToLower(filepath.Ext(media.AbsPath))
	if ext == ".svg" {
		media.ThumbnailPath = media.AbsPath
		return nil
	}
	parent := filepath.Dir(media.AbsPath)
	thumbDir := thumb.ThumbnailDir(parent)
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return fmt.Errorf("mkdir thumbnails: %w", err)
	}
	thumbnailPath := thumb.ThumbnailPathFor(media.AbsPath, parent)

	if thumbGen == nil {
		media.ThumbnailPath = thumbnailPath
		return nil
	}
	if err := thumbGen.Generate(ctx, media.AbsPath, thumbnailPath, duration); err != nil {
		return fmt.Errorf("generate thumbnail: %w", err)
	}
	media.ThumbnailPath = thumbnailPath
	return nil
}
