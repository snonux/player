package service

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/transcode"
)

type downloadQualityStub struct {
	path  string
	err   error
	calls int
}

func (q *downloadQualityStub) QualityStream(_ context.Context, id, user int64, profile string) (*transcode.Rendition, error) {
	q.calls++
	if id != 1 || user != 7 || profile != "opus96" {
		return nil, ErrForbidden
	}
	return &transcode.Rendition{Path: q.path, ContentType: "audio/ogg"}, q.err
}

func TestMusicDownloadSelectedQuality(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "encoded.ogg")
	if err := os.WriteFile(path, []byte("encoded bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	quality := &downloadQualityStub{path: path}
	browse := &MockMediaService{
		GetMediaDetailFunc: func(context.Context, int64, int64) (*MediaDetail, error) {
			return &MediaDetail{Media: &model.Media{Type: "audio"}}, nil
		},
		DownloadMediaFunc: func(context.Context, int64, int64) (*FileResult, error) {
			return &FileResult{FileName: "song.flac"}, nil
		},
	}
	archive, err := BuildMusicDownloadArchive(context.Background(), browse, nil, []int64{1, 1}, 7, MusicDownloadQuality{Profile: "opus96", Service: quality})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.File.Close(); _ = os.Remove(archive.File.Name()) }()
	reader, err := zip.NewReader(archive.File, archive.Size)
	if err != nil {
		t.Fatal(err)
	}
	if quality.calls != 1 || len(reader.File) != 1 || reader.File[0].Name != "1-song.ogg" {
		t.Fatalf("calls=%d entries=%v", quality.calls, reader.File)
	}
	file, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	bytes, err := io.ReadAll(file)
	if err != nil || string(bytes) != "encoded bytes" {
		t.Fatalf("bytes=%q err=%v", bytes, err)
	}
	quality.err = ErrTranscodeNoSpace
	failed, err := BuildMusicDownloadArchive(context.Background(), browse, nil, []int64{1}, 7, MusicDownloadQuality{Profile: "opus96", Service: quality})
	if failed != nil || !errors.Is(err, ErrTranscodeNoSpace) {
		t.Fatalf("partial archive=%v err=%v", failed, err)
	}
	failed, err = BuildMusicDownloadArchive(context.Background(), browse, nil, []int64{1}, 7, MusicDownloadQuality{Profile: "unknown", Service: quality})
	if failed != nil || err == nil {
		t.Fatalf("unknown profile archive=%v err=%v", failed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	quality.err = ErrTranscodePending
	_, err = readyMusicDownload(ctx, MusicDownloadQuality{Profile: "opus96", Service: quality}, 1, 7)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preparation: %v", err)
	}
}
