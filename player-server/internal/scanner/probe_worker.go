// Package scanner implements media library scanning logic.
package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/thumb"
)

// fileResult carries the outcome of handling one file back to the scanWriter:
// the media record of a newly probed file, or an already indexed row whose
// thumbnail was regenerated at a new path.
type fileResult struct {
	media *model.Media
	path  string // absolute path used for logging
	// refresh marks an already indexed row whose thumbnail was generated
	// anew; the row is updated instead of inserted.
	refresh bool
	// replaced is the generated thumbnail the row stored before, which is
	// deleted once unused. Empty when there was none.
	replaced string
}

// probeWorker probes individual media files via ffprobe and resolves thumbnail
// paths. It handles concurrency: multiple goroutines call run() in parallel,
// each reading from pathChan and writing probed fileResults to resultChan.
// All filesystem probing and thumbnail generation happens here; no DB writes.
type probeWorker struct {
	prober   probe.Prober
	thumbMkr thumb.Maker
	fs       FS
	clock    clock.Clock
	logger   *slog.Logger
	// stale holds the relPaths of indexed rows that get a thumbnail
	// generated, and why (see staleThumbnails). Read-only once the
	// workers run.
	stale map[string]staleKind
}

// newProbeWorker creates a probeWorker with the required dependencies.
// stale lists the indexed rows that get a thumbnail generated.
func newProbeWorker(prober probe.Prober, maker thumb.Maker, fs FS, clk clock.Clock, logger *slog.Logger, stale map[string]staleKind) *probeWorker {
	return &probeWorker{
		prober:   prober,
		thumbMkr: maker,
		fs:       fs,
		clock:    clk,
		logger:   logger,
		stale:    stale,
	}
}

// run consumes file paths from pathChan, probes each one, and sends the result
// to resultChan. It exits early when scanCtx is cancelled or sendErr is called.
// The caller is responsible for closing pathChan; run returns when pathChan is drained.
func (pw *probeWorker) run(
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
		// Use scanCtx (not ctx) so ffprobe/ffmpeg subprocesses cancel promptly
		// when another worker fails or TriggerRescan restarts the scan.
		result, err := pw.probeFile(scanCtx, path, setPath, setID, setName, existing, coverImages, progress)
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

// probeFile handles a single file found on disk. A new file is probed and
// turned into a media record ready for persistence. A file that is already
// indexed yields nothing, unless its thumbnail is stale, in which case the
// thumbnail is regenerated (see refreshThumbnail). Also returns nil when a
// new file cannot be probed (unrecognised format — a warning is logged and
// the file is skipped).
func (pw *probeWorker) probeFile(
	ctx context.Context,
	path, setPath string,
	setID int64,
	setName string,
	existing map[string]model.Media,
	coverImages map[string]string,
	progress *model.ScanProgress,
) (*fileResult, error) {
	relPath, err := filepath.Rel(setPath, path)
	if err != nil {
		return nil, fmt.Errorf("rel path for %q: %w", path, err)
	}
	relPath = filepath.ToSlash(relPath)

	if progress != nil {
		progress.IncrementFile()
	}

	row, alreadyExists := existing[relPath]
	pw.logger.Debug("scanner file checked", "set", setName, "path", relPath, "existing", alreadyExists)
	if alreadyExists {
		if kind, isStale := pw.stale[relPath]; isStale {
			return pw.refreshThumbnail(ctx, path, row, kind), nil
		}
		return nil, nil
	}

	info, err := pw.fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	meta, err := pw.prober.Probe(ctx, path)
	if err != nil {
		pw.logger.Warn("scanner skipping unprobeable file", "path", path, "err", err)
		return nil, nil
	}
	meta.FileSizeBytes = info.Size()

	mediaType := mediatype.TypeForExt(path)
	thumbnailPath := pw.buildThumbnailPath(ctx, path, setPath, mediaType, coverImages, meta)
	media := pw.newMedia(setID, relPath, path, mediaType, meta, thumbnailPath)
	return &fileResult{media: media, path: path}, nil
}

// newMedia assembles the media record of a newly found file from its probed
// metadata.
func (pw *probeWorker) newMedia(setID int64, relPath, path string, mediaType model.MediaType, meta *model.Metadata, thumbnailPath string) *model.Media {
	return &model.Media{
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
		CreatedAt:       pw.clock.Now(),
	}
}

// buildThumbnailPath resolves the thumbnail path for a new media file.
// Video and image thumbnails are produced via thumb.Maker; audio uses a
// nearby cover image; SVG images are served as-is (no raster thumbnail
// needed). When no thumbnail can be made (generator failure, unwritable
// folder) the file is indexed all the same: a video without thumbnail, an
// image with the image itself standing in.
func (pw *probeWorker) buildThumbnailPath(ctx context.Context, path, setPath string, mediaType model.MediaType, coverImages map[string]string, meta *model.Metadata) string {
	switch mediaType {
	case model.MediaTypeVideo:
		return pw.thumbMkr.MakeVideo(ctx, path, meta.Duration)
	case model.MediaTypeAudio:
		return findCoverImage(path, coverImages, setPath)
	case model.MediaTypeImage:
		if strings.ToLower(filepath.Ext(path)) == ".svg" {
			// SVG is a vector format; serve the original file directly.
			return path
		}
		if thumbPath := pw.thumbMkr.MakeImage(ctx, path); thumbPath != "" {
			return thumbPath
		}
		return path
	}
	return ""
}

// refreshThumbnail generates a thumbnail for an indexed row that is due for
// one (see staleThumbnails) and returns the result for the scanWriter to
// apply, or nil when no thumbnail could be made. In that case the row keeps
// what it has, and the next rescan tries again.
//
// The row's stored duration is used, so the file is not probed anew.
func (pw *probeWorker) refreshThumbnail(ctx context.Context, path string, row model.Media, kind staleKind) *fileResult {
	var thumbPath string
	if row.Type == model.MediaTypeVideo {
		thumbPath = pw.thumbMkr.MakeVideo(ctx, path, row.Duration)
	} else {
		thumbPath = pw.thumbMkr.MakeImage(ctx, path)
	}
	if thumbPath == "" {
		return nil
	}
	result := &fileResult{media: &row, path: path, refresh: true}
	if kind != staleNone {
		// Only a generated thumbnail is ever deleted. A row without one
		// stores nothing, or the image itself.
		result.replaced = row.ThumbnailPath
	}
	row.ThumbnailPath = thumbPath
	return result
}
