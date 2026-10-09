package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// oldThumb is the content of every thumbnail seeded under the old naming
// scheme; a migrated file still holding it was renamed, not regenerated.
const oldThumb = "old thumbnail"

// seededMedia describes one pre-existing media row: its set-relative source
// path and the set-relative thumbnail path stored in the database ("" for
// none). noSource leaves the source file off disk.
type seededMedia struct {
	rel      string
	thumb    string
	noSource bool
}

// migrationFixture is a real set directory plus a real SQLite store holding
// rows as an older release would have left them.
type migrationFixture struct {
	t       *testing.T
	root    string
	setPath string
	store   *repository.SQLite
	setID   int64
	genErr  error
	// generated records the sources the generator was asked to render.
	generated []string
}

func newMigrationFixture(t *testing.T, seed []seededMedia) *migrationFixture {
	t.Helper()
	ctx := context.Background()
	f := &migrationFixture{t: t, root: t.TempDir()}
	f.setPath = filepath.Join(f.root, "set")
	store, err := repository.Open(filepath.Join(t.TempDir(), "player.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f.store = store
	if f.setID, err = store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "set", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, sm := range seed {
		src := f.abs(sm.rel)
		if !sm.noSource {
			f.write(sm.rel, "source")
		}
		m := &model.Media{
			SetID: f.setID, RelPath: sm.rel, FileName: filepath.Base(sm.rel), AbsPath: src,
			Type: mediatype.TypeForExt(src), CreatedAt: time.Now(),
		}
		if sm.thumb != "" {
			m.ThumbnailPath = f.abs(sm.thumb)
			if sm.thumb != sm.rel {
				f.write(sm.thumb, oldThumb)
			}
		}
		if _, err := store.CreateMedia(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *migrationFixture) abs(rel string) string {
	return filepath.Join(f.setPath, filepath.FromSlash(rel))
}

func (f *migrationFixture) write(rel, content string) {
	f.t.Helper()
	path := f.abs(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// scan runs a full rescan. The generator writes "thumb of <source>" so tests
// can tell which source a thumbnail on disk really belongs to.
func (f *migrationFixture) scan() {
	f.t.Helper()
	gen := &thumb.MockGenerator{GenerateFunc: func(_ context.Context, in, out string, _ float64) error {
		f.generated = append(f.generated, in)
		if f.genErr != nil {
			return f.genErr
		}
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}}
	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		return &model.Metadata{}, nil
	}}
	s := NewFSScanner(f.store, prober, gen, clock.RealClock{}, f.root)
	s.workers = 1
	if err := s.Scan(context.Background(), f.root, nil); err != nil {
		f.t.Fatal(err)
	}
}

// thumbOf returns the set-relative thumbnail path stored for rel.
func (f *migrationFixture) thumbOf(rel string) string {
	f.t.Helper()
	media, err := f.store.ListMedia(context.Background(), repository.MediaFilter{SetID: &f.setID, IncludeDeleted: true})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, m := range media {
		if m.RelPath != rel {
			continue
		}
		if m.ThumbnailPath == "" {
			return ""
		}
		got, err := filepath.Rel(f.setPath, m.ThumbnailPath)
		if err != nil {
			f.t.Fatal(err)
		}
		return filepath.ToSlash(got)
	}
	f.t.Fatalf("no media row for %q", rel)
	return ""
}

// assertThumb checks the stored thumbnail path of rel and the content of
// the file it points to.
func (f *migrationFixture) assertThumb(rel, wantThumb, wantContent string) {
	f.t.Helper()
	if got := f.thumbOf(rel); got != wantThumb {
		f.t.Errorf("thumbnail of %q = %q, want %q", rel, got, wantThumb)
		return
	}
	content, err := os.ReadFile(f.abs(wantThumb))
	if err != nil {
		f.t.Errorf("thumbnail of %q is broken: %v", rel, err)
		return
	}
	if string(content) != wantContent {
		f.t.Errorf("thumbnail of %q holds %q, want %q", rel, content, wantContent)
	}
}

func (f *migrationFixture) assertGone(rel string) {
	f.t.Helper()
	if _, err := os.Stat(f.abs(rel)); !errors.Is(err, os.ErrNotExist) {
		f.t.Errorf("%q should have been removed (stat err = %v)", rel, err)
	}
}

func (f *migrationFixture) fresh(rel string) string { return "thumb of " + f.abs(rel) }

// TestMigrateThumbnails_SharedStem is the reported bug: two files sharing a
// stem shared one thumbnail. After a rescan each has its own, generated
// from its own source, and the shared file is gone.
func TestMigrateThumbnails_SharedStem(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
		{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
	})
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_SameNameInTwoFolders covers the scanner keeping all
// thumbnails of a set flat in one directory.
func TestMigrateThumbnails_SameNameInTwoFolders(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "a/clip.mp4", thumb: ".thumbnails/clip.jpg"},
		{rel: "b/clip.mp4", thumb: ".thumbnails/clip.jpg"},
	})
	f.scan()
	f.assertThumb("a/clip.mp4", ".thumbnails/a/clip.mp4.jpg", f.fresh("a/clip.mp4"))
	f.assertThumb("b/clip.mp4", ".thumbnails/b/clip.mp4.jpg", f.fresh("b/clip.mp4"))
	f.assertGone(".thumbnails/clip.jpg")
}

// TestMigrateThumbnails_ExclusiveIsRenamed: a thumbnail only one row uses is
// moved, not regenerated, so the frame a user picked survives the upgrade.
// Covers both layouts: the scanner's (set root) and the upload's (beside
// the source).
func TestMigrateThumbnails_ExclusiveIsRenamed(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"},
		{rel: "scanned/deep.mp4", thumb: ".thumbnails/deep.jpg"},
		{rel: "up/load.png", thumb: "up/.thumbnails/load.jpg"},
	})
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.mp4.jpg", oldThumb)
	f.assertThumb("scanned/deep.mp4", ".thumbnails/scanned/deep.mp4.jpg", oldThumb)
	f.assertThumb("up/load.png", "up/.thumbnails/load.png.jpg", oldThumb)
	for _, old := range []string{".thumbnails/clip.jpg", ".thumbnails/deep.jpg", "up/.thumbnails/load.jpg"} {
		f.assertGone(old)
	}
	if len(f.generated) != 0 {
		t.Errorf("generator ran for %v, want renames only", f.generated)
	}
}

// TestMigrateThumbnails_OldPathIsAnotherRowsNewPath: "holiday.mp4.jpg" is the
// old thumbnail of holiday.mp4.png and the new one of holiday.mp4. It must
// neither be handed to the wrong row nor deleted.
func TestMigrateThumbnails_OldPathIsAnotherRowsNewPath(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
		{rel: "holiday.mp4.png", thumb: ".thumbnails/holiday.mp4.jpg"},
	})
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", oldThumb)
	f.assertThumb("holiday.mp4.png", ".thumbnails/holiday.mp4.png.jpg", f.fresh("holiday.mp4.png"))
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_MissingOldFileIsRegenerated: a row whose old
// thumbnail vanished from disk cannot be renamed and gets a fresh one.
func TestMigrateThumbnails_MissingOldFileIsRegenerated(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"}})
	if err := os.Remove(f.abs(".thumbnails/clip.jpg")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.mp4.jpg", f.fresh("clip.mp4"))
}

// TestMigrateThumbnails_LeavesOthersAlone lists everything the migration
// must not touch: current names, non-generated thumbnails, rows without a
// thumbnail, and rows whose source file is gone.
func TestMigrateThumbnails_LeavesOthersAlone(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "new.mp4", thumb: ".thumbnails/new.mp4.jpg"},
		{rel: "sub/new.mp4", thumb: ".thumbnails/sub/new.mp4.jpg"},
		{rel: "up/new.mp4", thumb: "up/.thumbnails/new.mp4.jpg"},
		{rel: "own.png", thumb: "own.png"}, // image serving as its own thumbnail
		{rel: "album/art.png", thumb: "album/art.png"},
		{rel: "album/track.mp3", thumb: "album/art.png"}, // audio cover (seeding overwrites art.png with oldThumb)
		{rel: "bare.mp4"},
		{rel: "gone.mp4", thumb: ".thumbnails/gone.jpg", noSource: true},
	})
	f.scan()
	f.scan() // a second rescan must be a no-op as well

	for rel, want := range map[string]string{
		"new.mp4": ".thumbnails/new.mp4.jpg", "sub/new.mp4": ".thumbnails/sub/new.mp4.jpg",
		"up/new.mp4": "up/.thumbnails/new.mp4.jpg", "album/track.mp3": "album/art.png",
		"gone.mp4": ".thumbnails/gone.jpg",
	} {
		f.assertThumb(rel, want, oldThumb)
	}
	if got := f.thumbOf("own.png"); got != "own.png" {
		t.Errorf("thumbnail of own.png = %q, want own.png", got)
	}
	if got := f.thumbOf("bare.mp4"); got != "" {
		t.Errorf("thumbnail of bare.mp4 = %q, want none", got)
	}
	if len(f.generated) != 0 {
		t.Errorf("generator ran for %v, want nothing", f.generated)
	}
}

// TestMigrateThumbnails_GeneratorFailureKeepsOldThumbnail: when a shared
// thumbnail cannot be regenerated the rows keep their old, still working
// path, and the next rescan (with a working generator) finishes the job.
func TestMigrateThumbnails_GeneratorFailureKeepsOldThumbnail(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
		{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
	})
	f.genErr = errors.New("ffmpeg boom")
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", oldThumb)
	f.assertThumb("holiday.png", ".thumbnails/holiday.jpg", oldThumb)

	f.genErr = nil
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_StoreFailureKeepsSharedFile: if the database update
// fails the row still points at the old path, so that file must survive.
func TestMigrateThumbnails_StoreFailureKeepsSharedFile(t *testing.T) {
	setPath := filepath.Join(t.TempDir(), "set")
	old := filepath.Join(setPath, thumb.DirName, "holiday.jpg")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(oldThumb), 0o644); err != nil {
		t.Fatal(err)
	}
	existing := map[string]model.Media{
		"holiday.mp4": {ID: 1, RelPath: "holiday.mp4", Type: model.MediaTypeVideo, ThumbnailPath: old},
		"holiday.png": {ID: 2, RelPath: "holiday.png", Type: model.MediaTypeImage, ThumbnailPath: old},
	}
	seen := map[string]struct{}{"holiday.mp4": {}, "holiday.png": {}}

	store := repository.NewMockStore()
	var updated []int64
	store.MediaRepo.UpdateMediaThumbnailFunc = func(_ context.Context, id int64, _ string) error {
		if id == 2 {
			return errors.New("database is locked")
		}
		updated = append(updated, id)
		return nil
	}
	s := &FSScanner{store: store, fs: osFS{}, thumbMkr: thumb.NewFSMaker(&thumb.MockGenerator{}, nil, nil)}
	s.migrateThumbnails(context.Background(), existing, seen, setPath, "set")

	if len(updated) != 1 || updated[0] != 1 {
		t.Errorf("updated rows = %v, want [1]", updated)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("old thumbnail still referenced by row 2 was removed: %v", err)
	}
}

// TestMigrateThumbnails_CancelledContextDoesNothing guards the early exit
// taken when a rescan is superseded or the server shuts down.
func TestMigrateThumbnails_CancelledContextDoesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	old := filepath.Join(t.TempDir(), "set", thumb.DirName, "clip.jpg")
	existing := map[string]model.Media{
		"clip.mp4": {ID: 1, RelPath: "clip.mp4", Type: model.MediaTypeVideo, ThumbnailPath: old},
	}
	store := repository.NewMockStore()
	store.MediaRepo.UpdateMediaThumbnailFunc = func(context.Context, int64, string) error {
		t.Error("row updated despite cancelled context")
		return nil
	}
	s := &FSScanner{store: store, fs: osFS{}, thumbMkr: thumb.NewFSMaker(&thumb.MockGenerator{}, nil, nil)}
	s.migrateThumbnails(ctx, existing, map[string]struct{}{"clip.mp4": {}}, filepath.Dir(filepath.Dir(old)), "set")
}
