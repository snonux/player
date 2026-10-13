package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// writePNG writes a small real image, which ffprobe and ffmpeg accept.
func writePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.Set(3, 3, color.RGBA{R: 0xff, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestThumbnailCache_ReadOnlySet drives the production wiring with
// THUMBNAIL_CACHE_DIR set and a set the server cannot write to, the way a
// read-only mount of another application's library looks: a rescan and
// "regenerate thumbnail" put the thumbnail into the cache, the thumbnail
// endpoint serves it from there, and the set stays untouched.
func TestThumbnailCache_ReadOnlySet(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	cache := filepath.Join(t.TempDir(), "thumbnails")
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) { cfg.ThumbnailCacheDir = cache })
	root := e.deps.Cfg.MediaRoot
	setDir := filepath.Join(root, "library", "album")
	src := filepath.Join(setDir, "photo.png")
	writePNG(t, src)
	for _, dir := range []string{setDir, filepath.Dir(setDir)} {
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}

	e.must(e.deps.Scanner.Scan(context.Background(), root, nil))

	want := filepath.Join(cache, "library", "album", thumb.DirName, "photo.png.jpg")
	photo := e.mediaByPath(src)
	if photo.ThumbnailPath != want {
		t.Fatalf("stored thumbnail = %q, want %q", photo.ThumbnailPath, want)
	}
	id := strconv.FormatInt(photo.ID, 10)
	rr := e.get("/api/v1/media/"+id+"/thumbnail", "admin")
	cached, err := os.ReadFile(want)
	e.must(err)
	if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), cached) {
		t.Fatalf("GET thumbnail = %d with %d bytes, want 200 and the %d cached bytes", rr.Code, rr.Body.Len(), len(cached))
	}

	// Regenerating works on the read-only set too and stays in the cache.
	e.must(os.Remove(want))
	if rr := e.request(context.Background(), http.MethodPost, "/api/v1/media/"+id+"/thumbnail", "admin", ""); rr.Code != http.StatusOK {
		t.Fatalf("regenerate thumbnail = %d %s", rr.Code, rr.Body)
	}
	if info, err := os.Stat(want); err != nil || info.Size() == 0 {
		t.Fatalf("regenerated thumbnail missing in the cache: %v", err)
	}

	entries, err := os.ReadDir(setDir)
	e.must(err)
	if len(entries) != 1 || entries[0].Name() != "photo.png" {
		t.Errorf("the read-only set was written to: %v", entries)
	}
}

// TestThumbnailCache_OffByDefault: without the setting the same scan keeps
// the thumbnail beside the media file, as before.
func TestThumbnailCache_OffByDefault(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	e := newE2E(t, &fakeRunner{})
	root := e.deps.Cfg.MediaRoot
	src := filepath.Join(root, "library", "photo.png")
	writePNG(t, src)

	e.must(e.deps.Scanner.Scan(context.Background(), root, nil))

	if got, want := e.mediaByPath(src).ThumbnailPath, thumb.ThumbnailPathFor(src); got != want {
		t.Errorf("stored thumbnail = %q, want %q", got, want)
	}
}

// mediaByPath returns the indexed row of the media file at abs.
func (e *e2e) mediaByPath(abs string) repositoryMedia {
	e.t.Helper()
	all, err := e.store.ListMedia(context.Background(), repository.MediaFilter{IncludeDeleted: true})
	e.must(err)
	for _, m := range all {
		if m.AbsPath == abs {
			return repositoryMedia{ID: m.ID, ThumbnailPath: m.ThumbnailPath}
		}
	}
	e.t.Fatalf("%q was not indexed", abs)
	return repositoryMedia{}
}

// repositoryMedia is the part of a media row these tests look at.
type repositoryMedia struct {
	ID            int64
	ThumbnailPath string
}
