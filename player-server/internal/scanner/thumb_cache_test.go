package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/thumb"
)

// These tests cover scans with a thumbnail cache directory configured
// (THUMBNAIL_CACHE_DIR): every generated thumbnail goes there instead of
// into the .thumbnails directory beside its media file.

// cached returns where the thumbnail of the set-relative file rel lives in
// the fixture's cache.
func (f *migrationFixture) cached(rel string) string {
	f.t.Helper()
	path, ok := thumb.NewLayout(f.root, f.cache).Cached(f.abs(rel))
	if !ok {
		f.t.Fatalf("no cache path for %q", rel)
	}
	return path
}

// assertCached checks that rel points at its own thumbnail in the cache and
// that the file was generated from rel's source.
func (f *migrationFixture) assertCached(rel string) {
	f.t.Helper()
	got, want := f.row(rel).ThumbnailPath, f.cached(rel)
	if got != want {
		f.t.Errorf("thumbnail of %q = %q, want %q", rel, got, want)
		return
	}
	content, err := os.ReadFile(got)
	if err != nil || string(content) != "thumb of "+f.abs(rel) {
		f.t.Errorf("thumbnail of %q holds %q (err %v), want one generated from its source", rel, content, err)
	}
}

// TestScanWithCache_ReadOnlySet is the reason the cache exists: a set that
// is a read-only mount of another application's library gets thumbnails,
// and nothing at all is written into it.
func TestScanWithCache_ReadOnlySet(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("album/clip.mp4"), "source")
	f.write(f.abs("album/pic.png"), "source")
	f.write(f.abs("other/clip.mp4"), "source")
	for _, dir := range []string{"album", "other", ""} {
		if err := os.Chmod(f.abs(dir), 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(f.abs(dir), 0o755) })
	}

	f.scan()

	f.assertCached("album/clip.mp4")
	f.assertCached("album/pic.png")
	// Same file name in another folder: the cache mirrors the media tree.
	f.assertCached("other/clip.mp4")
	if f.cached("album/clip.mp4") == f.cached("other/clip.mp4") {
		t.Error("files of the same name in two folders share a cache path")
	}
	for _, dir := range []string{"album", "other", ""} {
		if _, err := os.Stat(thumb.ThumbnailDir(f.abs(dir))); err == nil {
			t.Errorf("a .thumbnails directory was created in the set (%q)", dir)
		}
	}

	// A second scan finds everything in place and generates nothing.
	f.generated = nil
	f.scan()
	f.assertNothingGenerated()
}

// TestScanWithCache_MovesThumbnailsOutOfTheSet: switching the cache on moves
// the thumbnails of a writable set into it on the next rescan; the files
// beside the media are deleted.
func TestScanWithCache_MovesThumbnailsOutOfTheSet(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "a/clip.mp4", thumb: "a/.thumbnails/clip.mp4.jpg"},
		{rel: "pic.png", thumb: ".thumbnails/pic.png.jpg"},
	})
	f.cache = t.TempDir()

	f.scan()

	f.assertCached("a/clip.mp4")
	f.assertCached("pic.png")
	f.assertGone("a/.thumbnails/clip.mp4.jpg")
	f.assertGone(".thumbnails/pic.png.jpg")
}

// TestScanWithCache_MissingCachedThumbnailIsRegenerated: a wiped cache
// volume heals on the next rescan.
func TestScanWithCache_MissingCachedThumbnailIsRegenerated(t *testing.T) {
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("clip.mp4"), "source")
	f.scan()
	if err := os.RemoveAll(filepath.Join(f.cache, "set")); err != nil {
		t.Fatal(err)
	}

	f.scan()

	f.assertCached("clip.mp4")
}

// TestScanWithCache_UnwritableCache: a cache that cannot be written is no
// reason to fall back into the set or to fail the scan. Media is indexed
// without a generated thumbnail and retried by the next rescan.
func TestScanWithCache_UnwritableCache(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("clip.mp4"), "source")
	f.write(f.abs("pic.png"), "source")
	if err := os.Chmod(f.cache, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.cache, 0o755) })

	f.scan()

	if got := f.row("clip.mp4").ThumbnailPath; got != "" {
		t.Errorf("thumbnail of the video = %q, want none", got)
	}
	f.assertThumb("pic.png", "pic.png", "source")
	f.assertNothingGenerated()
	if _, err := os.Stat(thumb.ThumbnailDir(f.setPath)); err == nil {
		t.Error("the scan fell back to a .thumbnails directory in the set")
	}

	// Once the cache is writable the next rescan makes the thumbnails.
	if err := os.Chmod(f.cache, 0o755); err != nil {
		t.Fatal(err)
	}
	f.scan()
	f.assertCached("clip.mp4")
	f.assertCached("pic.png")
}

// TestScanWithCache_SweepsStaleTemporaries: a temporary file left in the
// cache by a killed generation is removed, a recent one is kept.
func TestScanWithCache_SweepsStaleTemporaries(t *testing.T) {
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("clip.mp4"), "source")
	dir := filepath.Dir(f.cached("clip.mp4"))
	stale, recent := filepath.Join(dir, ".tmp-1.jpg"), filepath.Join(dir, ".tmp-2.jpg")
	f.write(stale, "half")
	f.write(recent, "half")
	long := time.Now().Add(-2 * staleTemporaryAge)
	if err := os.Chtimes(stale, long, long); err != nil {
		t.Fatal(err)
	}

	f.scan()

	if _, err := os.Stat(stale); err == nil {
		t.Error("stale temporary file in the cache was not swept")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("recent temporary file was removed: %v", err)
	}
}

// TestStaleKindOf_Cache: which stored thumbnail paths a scan with a cache
// replaces, and which it must leave alone.
func TestStaleKindOf_Cache(t *testing.T) {
	const root, cache, set = "/media", "/thumbs", "/media/set"
	src := set + "/a/clip.mp4"
	layout := thumb.NewLayout(root, cache)
	cached, _ := layout.Cached(src)
	present := map[string]bool{cached: true, "/elsewhere/.thumbnails/clip.mp4.jpg": true}
	exists := func(path string) bool { return present[path] }
	tests := []struct {
		name   string
		layout thumb.Layout
		stored string
		want   staleKind
	}{
		{"cached and present", layout, cached, 0},
		{"beside the source, cache now configured", layout, thumb.ThumbnailPathFor(src), staleOldName},
		{"never made", layout, "", staleNone},
		{"another file's cache path that is gone", layout, "/thumbs/set/a/.thumbnails/other.mp4.jpg", staleNone},
		{"abandoned cache, file still there", layout, "/elsewhere/.thumbnails/clip.mp4.jpg", 0},
		{"abandoned cache, file gone", layout, "/old-cache/set/a/.thumbnails/clip.mp4.jpg", staleNone},
		{"ordinary file outside the set that is gone", layout, "/elsewhere/cover.jpg", 0},
		// Without a cache a path in the former cache is just a path
		// outside the set: kept while it exists.
		{"cache switched off, file still there", thumb.Layout{}, cached, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staleKindOf(tt.stored, src, set, map[string]model.Media{}, exists, tt.layout); got != tt.want {
				t.Errorf("staleKindOf(%q) = %d, want %d", tt.stored, got, tt.want)
			}
		})
	}

	t.Run("cached but missing", func(t *testing.T) {
		none := func(string) bool { return false }
		if got := staleKindOf(cached, src, set, map[string]model.Media{}, none, layout); got != staleMissing {
			t.Errorf("staleKindOf = %d, want staleMissing", got)
		}
	})
}
