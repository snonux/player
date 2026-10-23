package service

import (
	"archive/zip"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snonux/player/internal/transcode"
)

// MusicDownloadQuality snapshots an independent download profile for an archive.
// Empty and original preserve the original-file download contract.
type MusicDownloadQuality struct {
	Profile string
	Service QualityStreamService
}

func (q MusicDownloadQuality) validate() error {
	if q.Profile == "" || q.Profile == "original" {
		return nil
	}
	if _, ok := transcode.AudioProfileKind(q.Profile); !ok {
		return &apiError{msg: "unsupported download quality", status: http.StatusBadRequest}
	}
	if q.Service == nil {
		return ErrTranscodeBusy
	}
	return nil
}

func appendMusicDownload(ctx context.Context, archive *zip.Writer, streamer MediaStreamer, source *FileResult, id, userID, remaining int64, quality MusicDownloadQuality) (int64, error) {
	if quality.Profile == "" || quality.Profile == "original" {
		return appendMusicOriginal(ctx, archive, streamer, source, id, remaining)
	}
	rendition, err := readyMusicDownload(ctx, quality, id, userID)
	if err != nil {
		return 0, err
	}
	if rendition == nil {
		return 0, ErrNotFound
	}
	file, err := os.Open(rendition.Path)
	if err != nil {
		return 0, ErrTranscodePending
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return 0, err
	}
	name := strings.TrimSuffix(source.FileName, filepath.Ext(source.FileName)) + filepath.Ext(rendition.Path)
	return appendMusicFile(ctx, archive, &StreamResult{File: file, Size: stat.Size()}, name, id, remaining)
}

// Copy each finished rendition immediately so a collection can exceed the
// disposable cache budget without preflight evicting its earlier songs.
func readyMusicDownload(ctx context.Context, quality MusicDownloadQuality, id, userID int64) (*transcode.Rendition, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		rendition, err := quality.Service.QualityStream(ctx, id, userID, quality.Profile)
		if !errors.Is(err, ErrTranscodePending) {
			return rendition, err
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, ErrTranscodePending
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
