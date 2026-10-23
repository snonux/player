package service

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// MusicDownloadLimit bounds original-file archives for browser memory use.
	MusicDownloadLimit = 512 << 20
	// MusicDownloadCountLimit bounds one collection download request.
	MusicDownloadCountLimit = 500
)

// MusicDownloadArchive is a temporary, complete original-file ZIP. The caller
// must close and remove File after serving it. No partially built ZIP is served.
type MusicDownloadArchive struct {
	File  *os.File
	Count int
	Size  int64
}

// BuildMusicDownloadArchive rechecks access using the normal download service
// and opens originals through the normal confined streamer. A failure discards
// the entire archive; retrying never saves an incomplete song as successful.
func BuildMusicDownloadArchive(ctx context.Context, browse MediaBrowseService, streamer MediaStreamer, ids []int64, userID int64) (*MusicDownloadArchive, error) {
	if len(ids) == 0 || len(ids) > MusicDownloadCountLimit {
		return nil, &apiError{msg: fmt.Sprintf("select 1–%d songs per download", MusicDownloadCountLimit), status: http.StatusBadRequest}
	}
	file, err := os.CreateTemp("", "player-music-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create music archive: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()
	archive := zip.NewWriter(file)
	seen := make(map[int64]bool)
	var total int64
	count := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if id <= 0 {
			return nil, &apiError{msg: "invalid song ID", status: http.StatusBadRequest}
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		detail, err := browse.GetMediaDetail(ctx, id, userID)
		if err != nil {
			return nil, err
		}
		if detail == nil || detail.Media == nil || detail.Media.Type != "audio" {
			return nil, &apiError{msg: "only available audio songs can be downloaded", status: http.StatusBadRequest}
		}
		res, err := browse.DownloadMedia(ctx, id, userID)
		if err != nil {
			return nil, err
		}
		if res == nil {
			return nil, ErrNotFound
		}
		size, err := appendMusicOriginal(ctx, archive, streamer, res, id, MusicDownloadLimit-total)
		if err != nil {
			return nil, err
		}
		total += size
		count++
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finish music archive: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() > MusicDownloadLimit {
		return nil, &apiError{msg: "archive exceeds 512 MiB; select fewer songs", status: http.StatusRequestEntityTooLarge}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	success = true
	return &MusicDownloadArchive{File: file, Count: count, Size: stat.Size()}, nil
}

func appendMusicOriginal(ctx context.Context, archive *zip.Writer, streamer MediaStreamer, res *FileResult, id, remaining int64) (int64, error) {
	stream, err := streamer.Open(ctx, res, true)
	if err != nil {
		return 0, fmt.Errorf("song %d is unavailable; refresh and retry", id)
	}
	defer func() { _ = stream.File.Close() }()
	if stream.Size < 0 || stream.Size > remaining {
		return 0, &apiError{msg: "archive exceeds 512 MiB; select fewer songs", status: http.StatusRequestEntityTooLarge}
	}
	name := filepath.Base(strings.ReplaceAll(res.FileName, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "song"
	}
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("%d-%s", id, name), Method: zip.Store})
	if err != nil {
		return 0, err
	}
	// Read one byte beyond the advertised size to reject files changed mid-copy.
	size, err := io.Copy(entry, &contextMusicReader{ctx: ctx, reader: io.LimitReader(stream.File, stream.Size+1)})
	if err != nil {
		return 0, fmt.Errorf("read song %d: download interrupted", id)
	}
	if size != stream.Size {
		return 0, fmt.Errorf("song %d changed; refresh and retry", id)
	}
	return size, nil
}

type contextMusicReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextMusicReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
