package service

import (
	"context"
	"fmt"
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
//
// If the row cannot be updated, the thumbnail generated here is removed
// again: the callers then delete the file and the row, and nothing would
// ever find that thumbnail afterwards.
func ImportMediaFile(
	ctx context.Context,
	store interface {
		UpdateMedia(ctx context.Context, media *model.Media) error
	},
	media *model.Media,
	prober probe.Prober,
	thumbs ThumbnailMaker,
) error {
	meta, err := probeMedia(ctx, prober, media.AbsPath)
	if err != nil {
		return fmt.Errorf("probe media: %w", err)
	}
	applyMetadata(media, meta)

	made := false
	if media.Type == model.MediaTypeVideo || media.Type == model.MediaTypeImage {
		made = generateThumbnail(ctx, thumbs, media, meta.Duration)
	}

	if err := store.UpdateMedia(ctx, media); err != nil {
		if made {
			thumbs.Remove(media.ThumbnailPath)
		}
		return fmt.Errorf("update media metadata: %w", err)
	}
	return nil
}

// applyMetadata copies the probed metadata into the media row.
func applyMetadata(media *model.Media, meta *model.Metadata) {
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

// generateThumbnail sets media.ThumbnailPath for video and image media and
// reports whether it generated a thumbnail file for that (made). An SVG is
// its own thumbnail. Everything else goes through the ThumbnailMaker, the
// one implementation shared with the scanner and RegenerateThumbnail: the
// thumbnail lands in .thumbnails beside the source, named after its full
// basename ("clip.mp4.jpg"), so a same-stem sibling ("clip.png") gets its
// own file instead of overwriting this one; and it is generated into a
// temporary file and verified before it is put in place.
//
// A thumbnail that cannot be made is no reason to reject the upload or the
// download: the maker logs why, and the media is stored like the scanner
// would index it, a video without thumbnail, an image standing in as its
// own. A later rescan tries again.
//
// Without a maker (a service built without a generator) the path is
// recorded but no file is written.
//
// Accepted limitation, between upgrading from a release up to v0.2.2 and
// the first rescan: the path written here can still be the thumbnail of
// another item stored under the old stem-based naming ("holiday.mp4.jpg"
// for an indexed "holiday.mp4.png"). That item then shows this file's
// picture until the rescan gives it a thumbnail of its own. It is not
// guarded against because the collision needs such a pair of names and
// heals itself; see docs/admin.md.
func generateThumbnail(ctx context.Context, thumbs ThumbnailMaker, media *model.Media, duration float64) (made bool) {
	isImage := media.Type == model.MediaTypeImage
	if strings.ToLower(filepath.Ext(media.AbsPath)) == ".svg" {
		media.ThumbnailPath = media.AbsPath
		return false
	}
	if thumbs == nil {
		media.ThumbnailPath = thumb.ThumbnailPathFor(media.AbsPath)
		return false
	}
	var thumbnailPath string
	if isImage {
		thumbnailPath = thumbs.MakeImage(ctx, media.AbsPath)
	} else {
		thumbnailPath = thumbs.MakeVideo(ctx, media.AbsPath, duration)
	}
	switch {
	case thumbnailPath != "":
		media.ThumbnailPath = thumbnailPath
	case isImage:
		media.ThumbnailPath = media.AbsPath
	default:
		media.ThumbnailPath = ""
	}
	return thumbnailPath != ""
}
