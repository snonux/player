package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
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

// TestThumbnailCache_DirectoryChanged: when the cache directory is changed,
// thumbnails stored in the old one are refused (they are outside the media
// root and not in the current cache) until a rescan has made new ones.
func TestThumbnailCache_DirectoryChanged(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	oldCache, newCache := filepath.Join(t.TempDir(), "old"), filepath.Join(t.TempDir(), "new")
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) { cfg.ThumbnailCacheDir = newCache })
	root := e.deps.Cfg.MediaRoot
	src := filepath.Join(root, "library", "photo.png")
	writePNG(t, src)
	e.must(e.deps.Scanner.Scan(context.Background(), root, nil))
	photo := e.mediaByPath(src)

	// The row as an instance with the old cache directory left it.
	stale := filepath.Join(oldCache, "library", thumb.DirName, "photo.png.jpg")
	e.must(os.MkdirAll(filepath.Dir(stale), 0o755))
	e.must(os.WriteFile(stale, []byte("old thumbnail"), 0o644))
	e.must(e.store.UpdateMediaThumbnail(context.Background(), photo.ID, stale))
	path := "/api/v1/media/" + strconv.FormatInt(photo.ID, 10) + "/thumbnail"
	if rr := e.get(path, "admin"); rr.Code == http.StatusOK {
		t.Fatalf("a thumbnail outside media root and cache was served: %d", rr.Code)
	}

	e.must(e.deps.Scanner.Scan(context.Background(), root, nil))

	want := filepath.Join(newCache, "library", thumb.DirName, "photo.png.jpg")
	if got := e.mediaByPath(src).ThumbnailPath; got != want {
		t.Fatalf("stored thumbnail after the rescan = %q, want %q", got, want)
	}
	if rr := e.get(path, "admin"); rr.Code != http.StatusOK {
		t.Errorf("GET thumbnail after the rescan = %d, want 200", rr.Code)
	}
	if content, err := os.ReadFile(stale); err != nil || string(content) != "old thumbnail" {
		t.Errorf("the abandoned cache was touched: %q, %v", content, err)
	}
}

// TestCheckThumbnailCache: startup says where thumbnails go, and warns when
// the configured directory cannot be used or was ignored.
func TestCheckThumbnailCache(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "media")
	e := os.Mkdir(filepath.Join(parent, "locked"), 0o555)
	if e != nil {
		t.Fatal(e)
	}
	tests := []struct{ name, dir, want string }{
		{"unset", "", ""},
		{"usable", filepath.Join(parent, "ok"), "level=INFO msg=\"thumbnails are stored in the thumbnail cache\""},
		{"unusable", filepath.Join(parent, "locked", "cache"), "level=WARN msg=\"thumbnail cache unusable"},
		// Cannot come out of LoadConfig, which rejects it; the layout
		// ignores such a cache and startup must not claim otherwise.
		{"overlapping the media root", parent, "level=WARN msg=\"thumbnail cache ignored"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			deps := &Deps{
				Cfg:        &internal.Config{MediaRoot: root, ThumbnailCacheDir: tt.dir},
				Logger:     slog.New(slog.NewTextHandler(&out, nil)),
				Thumbnails: thumb.NewLayout(root, tt.dir),
			}
			checkThumbnailCache(deps)
			if tt.want == "" && out.Len() != 0 {
				t.Errorf("logged %q, want nothing", out.String())
			}
			if !strings.Contains(out.String(), tt.want) {
				t.Errorf("log = %q, want it to contain %q", out.String(), tt.want)
			}
		})
	}
}

// TestThumbnailCache_PodcastDownloadAndGC covers the two remaining users of
// the thumbnail layout in the production wiring: a downloaded video episode
// gets its thumbnail in the cache, not in the podcast folder, and the
// garbage collector finds and removes it there when the episode is purged.
func TestThumbnailCache_PodcastDownloadAndGC(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	video := filepath.Join(t.TempDir(), "episode.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=64x48:d=1", "-pix_fmt", "yuv420p", video).CombinedOutput(); err != nil {
		t.Skipf("cannot generate a test video: %v: %s", err, out)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, video) }))
	defer server.Close()

	cache := filepath.Join(t.TempDir(), "thumbnails")
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) { cfg.ThumbnailCacheDir = cache })
	ctx, now := context.Background(), time.Now()
	setID, err := e.store.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: now})
	e.must(err)
	feedID, err := e.store.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: server.URL + "/feed", Title: "Show", CheckIntervalMinutes: 60, CreatedAt: now})
	e.must(err)
	episodeID, err := e.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: "1", Title: "one", EpisodeURL: server.URL + "/episode.mp4", CreatedAt: now})
	e.must(err)

	media, err := e.deps.PodcastSvc.DownloadEpisode(ctx, episodeID, e.users["admin"])
	if err != nil {
		t.Fatalf("DownloadEpisode: %v", err)
	}
	want, ok := e.deps.Thumbnails.Cached(media.AbsPath)
	if !ok || media.ThumbnailPath != want {
		t.Fatalf("episode thumbnail = %q, want it in the cache at %q", media.ThumbnailPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("cached episode thumbnail missing: %v", err)
	}
	if _, err := os.Stat(thumb.ThumbnailDir(filepath.Dir(media.AbsPath))); err == nil {
		t.Error("a .thumbnails directory was created in the podcast folder")
	}

	e.must(e.store.SoftDeleteMedia(ctx, media.ID))
	e.must(e.deps.GCWorker.WithAge(-time.Hour).RunOnce())
	if _, err := os.Stat(media.AbsPath); err == nil {
		t.Fatal("the purged episode file remains; the GC run did not purge it")
	}
	if _, err := os.Stat(want); err == nil {
		t.Error("the GC worker left the purged episode's cached thumbnail behind")
	}
}
