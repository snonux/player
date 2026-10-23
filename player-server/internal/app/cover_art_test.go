package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/model"
)

// Exercise the real Android cover endpoint with the production service wiring,
// actual source/cache files and SQLite rows. The live Audiobooks set contains
// book covers alongside unrelated Headspace text cards and videos.
func TestAudiobookSetCoverPrefersFrontArtwork(t *testing.T) {
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) {
		cfg.ThumbnailCacheDir = t.TempDir()
	})
	ctx := context.Background()
	setID, err := e.store.CreateSet(ctx, &model.Set{Name: "Audiobooks", RootPath: "Audiobooks", CreatedAt: time.Now()})
	e.must(err)
	root := filepath.Join(e.deps.Cfg.MediaRoot, "Audiobooks")
	write := func(path string, data []byte) {
		t.Helper()
		e.must(os.MkdirAll(filepath.Dir(path), 0o755))
		e.must(os.WriteFile(path, data, 0o644))
	}
	add := func(rel string, typ model.MediaType, data []byte, thumbnail string) (int64, string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		write(path, data)
		id, err := e.store.CreateMedia(ctx, &model.Media{
			SetID: setID, RelPath: rel, AbsPath: path, FileName: filepath.Base(path),
			Type: typ, ThumbnailPath: thumbnail, CreatedAt: time.Now(),
		})
		e.must(err)
		return id, path
	}
	session, err := e.deps.SM.CreateSession(ctx, e.users["admin"])
	e.must(err)
	get := func(prefix string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/sets/%d/cover", prefix, setID), nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: session})
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		rr := httptest.NewRecorder()
		e.server.ServeHTTP(rr, req)
		return rr
	}
	red, blue, unwanted := coverJPEG(t, color.RGBA{R: 255, A: 255}), coverJPEG(t, color.RGBA{B: 255, A: 255}), coverJPEG(t, color.RGBA{G: 255, A: 255})
	// Equal size/mtime still must not make different books share an ETag.
	size := max(len(red), len(blue))
	red = append(red, make([]byte, size-len(red))...)
	blue = append(blue, make([]byte, size-len(blue))...)
	cardThumb := filepath.Join(e.deps.Thumbnails.CacheDir(), "headspace.jpg")
	write(cardThumb, unwanted)
	_, _ = add("Headspace/Images/text.jpg", model.MediaTypeImage, unwanted, cardThumb)
	_, _ = add("Headspace/Videos/intro.mp4", model.MediaTypeVideo, []byte("video"), cardThumb)
	cached := get("/api/v1", nil)
	if cached.Code != http.StatusOK || !bytes.Equal(cached.Body.Bytes(), unwanted) {
		t.Fatal("text card fixture is not the initial cover")
	}
	cachedETag := cached.Header().Get("ETag")
	cardInfo, err := os.Stat(cardThumb)
	e.must(err)
	// The missing cached thumbnail must fall back to the source cover.
	_, first := add("Book A/cover.jpg", model.MediaTypeImage, red, filepath.Join(e.deps.Thumbnails.CacheDir(), "missing.jpg"))
	_, second := add("Book B/Front.PNG", model.MediaTypeImage, blue, "")
	_, _ = add("Book A/front.jpg", model.MediaTypeImage, unwanted, "")
	_, _ = add("Book A/folder.jpg", model.MediaTypeImage, unwanted, "")
	_, _ = add("Book A/back.jpg", model.MediaTypeImage, unwanted, "")
	_, _ = add("Book A/book.m4b", model.MediaTypeAudio, []byte("audio"), first)
	deleted, _ := add("Deleted/cover.jpg", model.MediaTypeImage, unwanted, "")
	e.must(e.store.SoftDeleteMedia(ctx, deleted))
	_, missing := add("Missing/cover.jpg", model.MediaTypeImage, unwanted, "")
	e.must(os.Remove(missing))
	_, _ = add("Empty/cover.jpg", model.MediaTypeImage, nil, "")
	_, directory := add("Directory/cover.jpg", model.MediaTypeImage, nil, "")
	e.must(os.Remove(directory))
	e.must(os.Mkdir(directory, 0o755))

	// A cached unrelated card may be newer than either correct book cover.
	older := time.Now().Add(-48 * time.Hour)
	e.must(os.Chtimes(first, older, older))
	e.must(os.Chtimes(second, older, older))
	for _, prefix := range []string{"/api", "/api/v1"} {
		seen := make(map[string]bool)
		for i := 0; i < 64; i++ {
			headers := map[string]string{"If-Modified-Since": cardInfo.ModTime().Format(http.TimeFormat)}
			if i%2 == 0 {
				headers["If-None-Match"] = cachedETag
			}
			rr := get(prefix, headers)
			if rr.Code != http.StatusOK || (!bytes.Equal(rr.Body.Bytes(), red) && !bytes.Equal(rr.Body.Bytes(), blue)) {
				t.Fatalf("%s root cover returned status %d, unexpected artwork %x", prefix, rr.Code, rr.Body.Bytes())
			}
			if rr.Header().Get("Last-Modified") != "" || rr.Header().Get("ETag") == "" || rr.Header().Get("Cache-Control") != "no-cache" {
				t.Fatalf("cover revalidation headers: %v", rr.Header())
			}
			seen[string(rr.Body.Bytes())] = true
		}
		if len(seen) != 2 {
			t.Fatal("root cover excluded one book's preferred artwork")
		}
	}

	// After one remaining candidate, matching ETags can return 304; stale
	// ETags and legacy mtime-only requests must receive the correct cover.
	e.must(os.Remove(second))
	rr := get("/api/v1", nil)
	etag := rr.Header().Get("ETag")
	if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), red) {
		t.Fatalf("remaining cover: %d", rr.Code)
	}
	if got := get("/api/v1", map[string]string{"If-None-Match": etag}); got.Code != http.StatusNotModified {
		t.Fatalf("matching ETag: %d", got.Code)
	}
	if got := get("/api/v1", map[string]string{"If-None-Match": cachedETag}); got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), red) {
		t.Fatalf("stale ETag: %d", got.Code)
	}
	// The ETag must include file identity even for identical size/mtime:
	// selecting another book cannot validate the previous book's bytes.
	_, third := add("Book C/cover.jpg", model.MediaTypeImage, blue, "")
	e.must(os.Chtimes(third, older, older))
	e.must(os.Remove(first))
	e.must(os.Remove(filepath.Join(root, "Book A", "front.jpg")))
	e.must(os.Remove(filepath.Join(root, "Book A", "folder.jpg")))
	if got := get("/api/v1", map[string]string{"If-None-Match": etag}); got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), blue) {
		t.Fatalf("different selected book: %d", got.Code)
	}
}

func coverJPEG(t *testing.T, pixel color.RGBA) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			im.SetRGBA(x, y, pixel)
		}
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, im, nil); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
