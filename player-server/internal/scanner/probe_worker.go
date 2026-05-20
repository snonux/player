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

// fileResult carries a successfully probed media record back to the scanWriter.
type fileResult struct {
	media *model.Media
	path  string // absolute path used for logging
}

// probeWorker probes individual media files via ffprobe and resolves thumbnail
// paths. It handles concurrency: multiple goroutines call run() in parallel,
// each reading from pathChan and writing probed fileResults to resultChan.
// All filesystem probing and thumbnail resolution happens here; no DB writes.
type probeWorker struct {
	prober   probe.Prober
	thumbMkr thumb.Maker
	fs       FS
	clock    clock.Clock
	logger   *slog.Logger
}

// newProbeWorker creates a probeWorker with the required dependencies.
func newProbeWorker(prober probe.Prober, maker thumb.Maker, fs FS, clk clock.Clock, logger *slog.Logger) *probeWorker {
	return &probeWorker{
		prober:   prober,
		thumbMkr: maker,
		fs:       fs,
		clock:    clk,
		logger:   logger,
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

// probeFile probes a single file and builds a media record ready for persistence.
// Returns nil when the file already exists in existing or cannot be probed
// (unrecognised format — a warning is logged and the file is skipped).
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

	_, alreadyExists := existing[relPath]
	pw.logger.Debug("scanner file checked", "set", setName, "path", relPath, "existing", alreadyExists)
	if alreadyExists {
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
	thumbnailPath, err := pw.buildThumbnailPath(ctx, path, setPath, mediaType, coverImages, meta)
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
		CreatedAt:       pw.clock.Now(),
	}

	return &fileResult{media: media, path: path}, nil
}

// buildThumbnailPath resolves the thumbnail path for a new media file.
// Video and image thumbnails are produced via thumb.Maker; audio uses a
// nearby cover image; SVG images are served as-is (no raster thumbnail needed).
func (pw *probeWorker) buildThumbnailPath(ctx context.Context, path, setPath string, mediaType model.MediaType, coverImages map[string]string, meta *model.Metadata) (string, error) {
	switch mediaType {
	case model.MediaTypeVideo:
		return pw.thumbMkr.MakeVideo(ctx, path, setPath, meta.Duration)
	case model.MediaTypeAudio:
		return findCoverImage(path, coverImages, setPath), nil
	case model.MediaTypeImage:
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".svg" {
			// SVG is a vector format; serve the original file directly.
			return path, nil
		}
		thumbPath, err := pw.thumbMkr.MakeImage(ctx, path, setPath)
		if err != nil {
			return "", err
		}
		if thumbPath != "" {
			if _, statErr := pw.fs.Stat(thumbPath); statErr == nil {
				return thumbPath, nil
			}
		}
		return path, nil
	}
	return "", nil
}
