package thumb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

func TestFSResolver_Resolve(t *testing.T) {
	tmpDir := t.TempDir()
	thumbPath := filepath.Join(tmpDir, "thumb.jpg")
	if err := os.WriteFile(thumbPath, []byte("thumb-bytes"), 0o644); err != nil {
		t.Fatalf("write thumb: %v", err)
	}
	imgPath := filepath.Join(tmpDir, "cover.jpg")
	if err := os.WriteFile(imgPath, []byte("imgcontents"), 0o644); err != nil {
		t.Fatalf("write img: %v", err)
	}

	r := NewFSResolver()

	t.Run("nil media is not found", func(t *testing.T) {
		if _, err := r.Resolve(nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("empty thumbnail path is not found", func(t *testing.T) {
		_, err := r.Resolve(&model.Media{ID: 1})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("existing thumbnail returns resolved file", func(t *testing.T) {
		res, err := r.Resolve(&model.Media{ID: 1, ThumbnailPath: thumbPath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Path != thumbPath {
			t.Fatalf("path = %q, want %q", res.Path, thumbPath)
		}
		if res.FileName != "thumb.jpg" {
			t.Fatalf("file name = %q", res.FileName)
		}
		if res.FileSize != int64(len("thumb-bytes")) {
			t.Fatalf("file size = %d", res.FileSize)
		}
	})

	t.Run("image falls back to AbsPath when thumb missing", func(t *testing.T) {
		missing := filepath.Join(tmpDir, "missing.jpg")
		res, err := r.Resolve(&model.Media{
			ID:            1,
			ThumbnailPath: missing,
			AbsPath:       imgPath,
			Type:          model.MediaTypeImage,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Path != imgPath {
			t.Fatalf("path = %q, want fallback %q", res.Path, imgPath)
		}
	})

	t.Run("non-image with missing thumb returns wrapped stat error", func(t *testing.T) {
		missing := filepath.Join(tmpDir, "nope.jpg")
		_, err := r.Resolve(&model.Media{
			ID:            1,
			ThumbnailPath: missing,
			AbsPath:       imgPath,
			Type:          model.MediaTypeVideo,
		})
		if err == nil {
			t.Fatal("expected error for missing video thumb")
		}
		if errors.Is(err, ErrNotFound) {
			t.Fatalf("did not expect ErrNotFound for video, got %v", err)
		}
	})

	t.Run("image with missing thumb and missing AbsPath surfaces stat error", func(t *testing.T) {
		_, err := r.Resolve(&model.Media{
			ID:            1,
			ThumbnailPath: filepath.Join(tmpDir, "gone.jpg"),
			AbsPath:       filepath.Join(tmpDir, "also-gone.jpg"),
			Type:          model.MediaTypeImage,
		})
		if err == nil {
			t.Fatal("expected error when both files are missing")
		}
	})
}
