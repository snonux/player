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

// TestScanWithCache_CacheDirectoryChanged: after THUMBNAIL_CACHE_DIR was
// pointed elsewhere the old thumbnails are not served any more, so a rescan
// makes new ones in the new cache. The abandoned directory is left alone.
func TestScanWithCache_CacheDirectoryChanged(t *testing.T) {
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("clip.mp4"), "source")
	f.scan()
	abandoned := f.cached("clip.mp4")

	f.cache = t.TempDir()
	f.scan()

	f.assertCached("clip.mp4")
	if f.cached("clip.mp4") == abandoned {
		t.Fatal("fixture error: the cache path did not change")
	}
	if _, err := os.Stat(abandoned); err != nil {
		t.Errorf("the abandoned cache was touched: %v", err)
	}
}

// TestScanWithCache_SwitchedOff: with the setting removed again, a rescan
// puts the thumbnails back beside the media and leaves the old cache alone.
func TestScanWithCache_SwitchedOff(t *testing.T) {
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("clip.mp4"), "source")
	f.scan()
	abandoned := f.cached("clip.mp4")

	f.cache = ""
	f.scan()

	f.assertFresh("clip.mp4")
	if _, err := os.Stat(abandoned); err != nil {
		t.Errorf("the abandoned cache was touched: %v", err)
	}
}

// TestScanWithCache_SweepsTemporariesOfVanishedMedia: a temporary file in a
// cache folder whose media is gone from the set would never be reached
// through the files on disk, and would keep that folder alive for good.
func TestScanWithCache_SweepsTemporariesOfVanishedMedia(t *testing.T) {
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("kept/clip.mp4"), "source")
	gone := filepath.Join(f.cache, "set", "gone", "deeper", thumb.DirName)
	stale, recent := filepath.Join(gone, ".tmp-1.jpg"), filepath.Join(gone, ".tmp-2.jpg")
	bystander := filepath.Join(gone, "clip.mp4.jpg")
	otherSet := filepath.Join(f.cache, "other-set", thumb.DirName, ".tmp-3.jpg")
	for _, path := range []string{stale, recent, bystander, otherSet} {
		f.write(path, "x")
	}
	long := time.Now().Add(-2 * staleTemporaryAge)
	for _, path := range []string{stale, bystander, otherSet} {
		if err := os.Chtimes(path, long, long); err != nil {
			t.Fatal(err)
		}
	}

	f.scan()

	if _, err := os.Stat(stale); err == nil {
		t.Error("stale temporary of vanished media was not swept")
	}
	for name, path := range map[string]string{"recent temporary": recent, "thumbnail": bystander, "temporary of a set that was not scanned": otherSet} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}

// TestScanWithCache_LeavesTemporariesInTheSetAlone: with a cache the
// scanner cleans up in the cache only. A file in a set's .thumbnails
// directory that merely looks like one of its temporaries is not the
// server's to delete: the set may be somebody else's library.
func TestScanWithCache_LeavesTemporariesInTheSetAlone(t *testing.T) {
	f := newMigrationFixture(t, nil)
	f.cache = t.TempDir()
	f.write(f.abs("clip.mp4"), "source")
	foreign := f.abs(".thumbnails/.tmp-foreign.jpg")
	f.write(foreign, "theirs")
	long := time.Now().Add(-2 * staleTemporaryAge)
	if err := os.Chtimes(foreign, long, long); err != nil {
		t.Fatal(err)
	}

	f.scan()

	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("a file in the set's .thumbnails directory was deleted: %v", err)
	}

	// Without a cache that directory is the scanner's own and is swept.
	f.cache = ""
	f.scan()
	if _, err := os.Stat(foreign); err == nil {
		t.Error("without a cache the stale temporary beside the media was not swept")
	}
}

// TestStaleKindOf_Cache: which stored thumbnail paths a scan replaces when
// thumbnails can live in a cache, and which it must leave alone.
func TestStaleKindOf_Cache(t *testing.T) {
	const root, cache, set = "/media", "/thumbs", "/media/set"
	src := set + "/a/clip.mp4"
	layout := thumb.NewLayout(root, cache)
	noCache := thumb.NewLayout(root, "")
	cached, _ := layout.Cached(src)
	const elsewhere, otherSet = "/elsewhere/.thumbnails/clip.mp4.jpg", "/media/other/.thumbnails/clip.mp4.jpg"
	const otherCached = "/thumbs/set/b/.thumbnails/clip.mp4.jpg"
	present := map[string]bool{cached: true, elsewhere: true, otherSet: true, otherCached: true}
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
		{"another file's cache path that is gone", layout, "/thumbs/set/a/.thumbnails/other.mp4.jpg", staleLost},
		// Not this file's cache path, but in the cache and present: the
		// server shows it, so it is left alone like a chosen file.
		{"another file's cache path, present", layout, otherCached, 0},
		// Outside the media root nothing but the current cache is served:
		// such a thumbnail is lost whether or not its file still exists.
		{"abandoned cache, file still there", layout, elsewhere, staleLost},
		{"abandoned cache, file gone", layout, "/old-cache/set/a/.thumbnails/clip.mp4.jpg", staleLost},
		{"cache switched off, file still there", noCache, cached, staleLost},
		{"cache switched off, file gone", noCache, "/thumbs/set/a/.thumbnails/gone.jpg", staleLost},
		// Inside the media root a thumbnail of another set keeps working.
		{"thumbnail in another set, present", layout, otherSet, 0},
		{"thumbnail in another set, gone", noCache, "/media/other/.thumbnails/gone.jpg", staleLost},
		// Never a generated thumbnail: an ordinary file is left alone.
		{"ordinary file outside the root that is gone", layout, "/elsewhere/cover.jpg", 0},
		{"ordinary file outside the root", noCache, "/elsewhere/cover.jpg", 0},
		// A scanner that was not told its media root (tests, mostly) keeps
		// the old rule: an existing file elsewhere is left alone.
		{"media root unknown, file still there", thumb.Layout{}, elsewhere, 0},
		{"media root unknown, file gone", thumb.Layout{}, "/gone/.thumbnails/clip.mp4.jpg", staleLost},
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
