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
)

func TestMusicDownloadArchive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	if err := os.WriteFile(path, []byte("original bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	browse := &MockMediaService{
		GetMediaDetailFunc: func(_ context.Context, id, user int64) (*MediaDetail, error) {
			if user != 7 {
				t.Fatal("wrong account")
			}
			if id == 99 {
				return nil, ErrForbidden
			}
			return &MediaDetail{Media: &model.Media{ID: id, Type: "audio"}}, nil
		},
		DownloadMediaFunc: func(_ context.Context, id, user int64) (*FileResult, error) {
			calls++
			return &FileResult{Path: path, FileName: "../song.flac"}, nil
		},
	}
	t.Run("original bytes safe names and deduplication", func(t *testing.T) {
		result, err := BuildMusicDownloadArchive(context.Background(), browse, NewMediaStreamer(nil, root), []int64{1, 1, 2}, 7)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = result.File.Close(); _ = os.Remove(result.File.Name()) }()
		if calls != 2 || result.Count != 2 {
			t.Fatalf("calls=%d count=%d", calls, result.Count)
		}
		archive, err := zip.NewReader(result.File, result.Size)
		if err != nil {
			t.Fatal(err)
		}
		if len(archive.File) != 2 || archive.File[0].Name != "1-song.flac" {
			t.Fatalf("bad entries: %v", archive.File)
		}
		file, err := archive.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		bytes, err := io.ReadAll(file)
		if err != nil || string(bytes) != "original bytes" {
			t.Fatalf("bytes=%q err=%v", bytes, err)
		}
	})
	t.Run("permission failure returns no archive", func(t *testing.T) {
		archive, err := BuildMusicDownloadArchive(context.Background(), browse, NewMediaStreamer(nil, root), []int64{1, 99}, 7)
		if archive != nil || !errors.Is(err, ErrForbidden) {
			t.Fatalf("archive=%v err=%v", archive, err)
		}
	})
	t.Run("root confinement", func(t *testing.T) {
		archive, err := BuildMusicDownloadArchive(context.Background(), browse, NewMediaStreamer(nil, t.TempDir()), []int64{1}, 7)
		if archive != nil || err == nil {
			t.Fatalf("archive=%v err=%v", archive, err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		archive, err := BuildMusicDownloadArchive(ctx, browse, NewMediaStreamer(nil, root), []int64{1}, 7)
		if archive != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("archive=%v err=%v", archive, err)
		}
	})
	t.Run("count limit", func(t *testing.T) {
		archive, err := BuildMusicDownloadArchive(context.Background(), browse, NewMediaStreamer(nil, root), make([]int64, 501), 7)
		if archive != nil || err == nil {
			t.Fatalf("archive=%v err=%v", archive, err)
		}
	})
}
