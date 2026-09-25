package thumb

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	t.Run("non-image with missing thumb file is not found", func(t *testing.T) {
		// The database can still reference artwork deleted from disk; clients
		// must get a 404, not a 500.
		missing := filepath.Join(tmpDir, "nope.jpg")
		_, err := r.Resolve(&model.Media{
			ID:            1,
			ThumbnailPath: missing,
			AbsPath:       imgPath,
			Type:          model.MediaTypeAudio,
		})
		if !errors.Is(err, ErrNotFound) || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected ErrNotFound wrapping os.ErrNotExist, got %v", err)
		}
	})

	t.Run("image with missing thumb and missing AbsPath is not found", func(t *testing.T) {
		_, err := r.Resolve(&model.Media{
			ID:            1,
			ThumbnailPath: filepath.Join(tmpDir, "gone.jpg"),
			AbsPath:       filepath.Join(tmpDir, "also-gone.jpg"),
			Type:          model.MediaTypeImage,
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound when both files are missing, got %v", err)
		}
	})

	t.Run("unreadable original names the original in the error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}
		locked := filepath.Join(tmpDir, "locked-original")
		if err := os.Mkdir(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		_, err := r.Resolve(&model.Media{
			ID:            1,
			ThumbnailPath: filepath.Join(tmpDir, "no-thumb.jpg"),
			AbsPath:       filepath.Join(locked, "photo.jpg"),
			Type:          model.MediaTypeImage,
		})
		if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "stat original image") {
			t.Fatalf("expected a stat original image I/O error, got %v", err)
		}
	})

	t.Run("permission failure is an I/O error, not not-found", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}
		locked := filepath.Join(tmpDir, "locked")
		if err := os.Mkdir(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		thumb := filepath.Join(locked, "thumb.jpg")
		if err := os.WriteFile(thumb, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		for _, typ := range []model.MediaType{model.MediaTypeAudio, model.MediaTypeImage} {
			_, err := r.Resolve(&model.Media{ID: 1, ThumbnailPath: thumb, AbsPath: imgPath, Type: typ})
			if err == nil || errors.Is(err, ErrNotFound) {
				t.Fatalf("%s: expected a non-not-found I/O error, got %v", typ, err)
			}
		}
	})
}
