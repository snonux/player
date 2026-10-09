package scanner

import (
	"context"
	"errors"
	"io/fs"
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

// seededMedia describes one pre-existing media row: its set-relative source
// path and the thumbnail path stored in the database, set-relative ("" for
// none) unless absThumb gives an absolute one. noSource leaves the source
// file off disk; deleted soft-deletes the row.
type seededMedia struct {
	rel      string
	thumb    string
	absThumb string
	noSource bool
	deleted  bool
}

// faultyFS is the real filesystem with injectable failures and hooks.
type faultyFS struct {
	osFS
	renameErr func(oldPath, newPath string) error
	mkdirErr  error
	removeErr error
}

func (f *faultyFS) Rename(oldPath, newPath string) error {
	if f.renameErr != nil {
		if err := f.renameErr(oldPath, newPath); err != nil {
			return err
		}
	}
	return f.osFS.Rename(oldPath, newPath)
}

func (f *faultyFS) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	return f.osFS.MkdirAll(path, perm)
}

func (f *faultyFS) Remove(name string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	return f.osFS.Remove(name)
}

// faultyStore is the real store whose thumbnail update can be made to fail.
type faultyStore struct {
	*repository.SQLite
	updateErr error
}

func (s *faultyStore) UpdateMediaThumbnail(ctx context.Context, id int64, path string) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.SQLite.UpdateMediaThumbnail(ctx, id, path)
}

// migrationFixture is a real set directory plus a real SQLite store holding
// rows as an older release would have left them.
type migrationFixture struct {
	t       *testing.T
	root    string
	setPath string
	store   *faultyStore
	setID   int64
	fs      *faultyFS
	// gen replaces the default generator, which writes "thumb of <source>".
	gen func(in, out string) error
	// generated records the sources the generator was asked to render.
	generated []string
}

func newMigrationFixture(t *testing.T, seed []seededMedia) *migrationFixture {
	t.Helper()
	ctx := context.Background()
	f := &migrationFixture{t: t, root: t.TempDir(), fs: &faultyFS{}}
	f.setPath = filepath.Join(f.root, "set")
	db, err := repository.Open(filepath.Join(t.TempDir(), "player.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f.store = &faultyStore{SQLite: db}
	if f.setID, err = db.CreateSet(ctx, &model.Set{Name: "set", RootPath: "set", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.setPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sm := range seed {
		f.seed(sm)
	}
	return f
}

// seed creates one row and its files. A generated thumbnail is filled with
// old(<its path>), so a test can tell which old file a migrated thumbnail
// came from; several rows seeded with one path share that file.
func (f *migrationFixture) seed(sm seededMedia) {
	f.t.Helper()
	ctx := context.Background()
	src := f.abs(sm.rel)
	if !sm.noSource {
		f.write(src, "source")
	}
	m := &model.Media{
		SetID: f.setID, RelPath: sm.rel, FileName: filepath.Base(sm.rel), AbsPath: src,
		Type: mediatype.TypeForExt(src), CreatedAt: time.Now(),
	}
	switch {
	case sm.absThumb != "":
		m.ThumbnailPath = sm.absThumb
	case sm.thumb != "":
		m.ThumbnailPath = f.abs(sm.thumb)
	}
	if thumb.IsGenerated(m.ThumbnailPath) {
		f.write(m.ThumbnailPath, old(m.ThumbnailPath))
	}
	id, err := f.store.CreateMedia(ctx, m)
	if err != nil {
		f.t.Fatal(err)
	}
	if sm.deleted {
		if err := f.store.SoftDeleteMedia(ctx, id); err != nil {
			f.t.Fatal(err)
		}
	}
}

func old(path string) string { return "old " + filepath.Base(path) }

func (f *migrationFixture) abs(rel string) string {
	return filepath.Join(f.setPath, filepath.FromSlash(rel))
}

func (f *migrationFixture) write(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *migrationFixture) scanner() *FSScanner {
	gen := &thumb.MockGenerator{GenerateFunc: func(_ context.Context, in, out string, _ float64) error {
		f.generated = append(f.generated, in)
		if f.gen != nil {
			return f.gen(in, out)
		}
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}}
	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		return &model.Metadata{}, nil
	}}
	s := NewFSScanner(f.store, prober, gen, clock.RealClock{}, f.root)
	s.workers = 1
	s.migFS = f.fs
	return s
}

// scan runs a full rescan that must succeed.
func (f *migrationFixture) scan() {
	f.t.Helper()
	if err := f.scanner().Scan(context.Background(), f.root, nil); err != nil {
		f.t.Fatal(err)
	}
}

// row returns the media row of rel, soft-deleted ones included.
func (f *migrationFixture) row(rel string) model.Media {
	f.t.Helper()
	media, err := f.store.ListMedia(context.Background(), repository.MediaFilter{SetID: &f.setID, IncludeDeleted: true})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, m := range media {
		if m.RelPath == rel {
			return m
		}
	}
	f.t.Fatalf("no media row for %q", rel)
	return model.Media{}
}

// assertThumb checks the stored thumbnail path of rel (set-relative) and the
// content of the file it points to: a row must never point at nothing.
func (f *migrationFixture) assertThumb(rel, wantThumb, wantContent string) {
	f.t.Helper()
	got := f.row(rel).ThumbnailPath
	if got != f.abs(wantThumb) {
		f.t.Errorf("thumbnail of %q = %q, want %q", rel, got, f.abs(wantThumb))
		return
	}
	content, err := os.ReadFile(got)
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
	if _, err := os.Stat(f.abs(rel)); !errors.Is(err, fs.ErrNotExist) {
		f.t.Errorf("%q should not exist (stat err = %v)", rel, err)
	}
}

func (f *migrationFixture) assertNothingGenerated() {
	f.t.Helper()
	if len(f.generated) != 0 {
		f.t.Errorf("generator ran for %v, want no generation", f.generated)
	}
}

func (f *migrationFixture) fresh(rel string) string { return "thumb of " + f.abs(rel) }

// sharedStem is the reported bug: two files sharing a stem share a thumbnail.
var sharedStem = []seededMedia{
	{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
	{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
}

// TestMigrateThumbnails_SharedStem: after a rescan each file has its own
// thumbnail, generated from its own source, and the shared file is gone.
func TestMigrateThumbnails_SharedStem(t *testing.T) {
	f := newMigrationFixture(t, sharedStem)
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_SameNameInTwoFolders covers the old scanner keeping
// all thumbnails of a set flat in one directory.
func TestMigrateThumbnails_SameNameInTwoFolders(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "a/clip.mp4", thumb: ".thumbnails/clip.jpg"},
		{rel: "b/clip.mp4", thumb: ".thumbnails/clip.jpg"},
	})
	f.scan()
	f.assertThumb("a/clip.mp4", "a/.thumbnails/clip.mp4.jpg", f.fresh("a/clip.mp4"))
	f.assertThumb("b/clip.mp4", "b/.thumbnails/clip.mp4.jpg", f.fresh("b/clip.mp4"))
	f.assertGone(".thumbnails/clip.jpg")
}

// TestMigrateThumbnails_ExclusiveIsRenamed: a thumbnail only one row can have
// written is moved, not regenerated, so the frame a user picked survives the
// upgrade. Covers both old layouts: the scanner's (set root) and the
// upload's (beside the source).
func TestMigrateThumbnails_ExclusiveIsRenamed(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"},
		{rel: "scanned/deep.mp4", thumb: ".thumbnails/deep.jpg"},
		{rel: "up/load.png", thumb: "up/.thumbnails/load.jpg"},
	})
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.mp4.jpg", old("clip.jpg"))
	f.assertThumb("scanned/deep.mp4", "scanned/.thumbnails/deep.mp4.jpg", old("deep.jpg"))
	f.assertThumb("up/load.png", "up/.thumbnails/load.png.jpg", old("load.jpg"))
	for _, stale := range []string{".thumbnails/clip.jpg", ".thumbnails/deep.jpg", "up/.thumbnails/load.jpg"} {
		f.assertGone(stale)
	}
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_OldPathIsAnotherRowsNewPath: "holiday.mp4.jpg" is the
// old thumbnail of holiday.mp4.png and the new one of holiday.mp4. Each row
// must keep its own picture.
func TestMigrateThumbnails_OldPathIsAnotherRowsNewPath(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
		{rel: "holiday.mp4.png", thumb: ".thumbnails/holiday.mp4.jpg"},
	})
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.mp4.png", ".thumbnails/holiday.mp4.png.jpg", old("holiday.mp4.jpg"))
	f.assertGone(".thumbnails/holiday.jpg")
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_DestinationStillInUse: as above, but the row in the
// way cannot move because its thumbnail must be regenerated (a second file
// could have written it) and the generator fails. The first row must then
// wait instead of overwriting a thumbnail the other row still shows.
func TestMigrateThumbnails_DestinationStillInUse(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
		{rel: "holiday.mp4.png", thumb: ".thumbnails/holiday.mp4.jpg"},
		{rel: "sub/holiday.mp4.png", thumb: "sub/.thumbnails/holiday.mp4.png.jpg"},
	})
	f.gen = func(string, string) error { return errors.New("ffmpeg boom") }
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.mp4.png", ".thumbnails/holiday.mp4.jpg", old("holiday.mp4.jpg"))

	f.gen = nil
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.mp4.png", ".thumbnails/holiday.mp4.png.jpg", f.fresh("holiday.mp4.png"))
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_RunsBeforeNewFilesAreProbed: a file added since the
// last scan gets the thumbnail path an indexed row still uses under the old
// naming. That row must have moved away, with its own picture, before the
// new file's thumbnail is written there.
func TestMigrateThumbnails_RunsBeforeNewFilesAreProbed(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "holiday.mp4.png", thumb: ".thumbnails/holiday.mp4.jpg"}})
	f.write(f.abs("holiday.mp4"), "source") // on disk, not indexed yet
	f.scan()
	f.assertThumb("holiday.mp4.png", ".thumbnails/holiday.mp4.png.jpg", old("holiday.mp4.jpg"))
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
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
// must not touch: current names, thumbnails that are ordinary files, rows
// without a thumbnail, and rows whose source file is gone.
func TestMigrateThumbnails_LeavesOthersAlone(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "new.mp4", thumb: ".thumbnails/new.mp4.jpg"},
		{rel: "sub/new.mp4", thumb: "sub/.thumbnails/new.mp4.jpg"},
		{rel: "own.png", thumb: "own.png"}, // image serving as its own thumbnail
		{rel: "album/art.png", thumb: "album/art.png"},
		{rel: "album/track.mp3", thumb: "album/art.png"}, // audio cover
		{rel: "bare.mp4"},
		{rel: "gone.mp4", thumb: ".thumbnails/gone.jpg", noSource: true},
	})
	f.scan()
	f.scan() // a second rescan must be a no-op as well

	f.assertThumb("new.mp4", ".thumbnails/new.mp4.jpg", old("new.mp4.jpg"))
	f.assertThumb("sub/new.mp4", "sub/.thumbnails/new.mp4.jpg", old("new.mp4.jpg"))
	f.assertThumb("gone.mp4", ".thumbnails/gone.jpg", old("gone.jpg"))
	f.assertThumb("own.png", "own.png", "source")
	f.assertThumb("album/track.mp3", "album/art.png", "source")
	if got := f.row("bare.mp4").ThumbnailPath; got != "" {
		t.Errorf("thumbnail of bare.mp4 = %q, want none", got)
	}
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_StoredPathOutsideSet: a stale absolute path (media
// root moved, database copied from another host) may point anywhere. The
// scanner must not rename, generate or delete files outside the set.
func TestMigrateThumbnails_StoredPathOutsideSet(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "elsewhere", thumb.DirName, "clip.jpg")
	f := newMigrationFixture(t, []seededMedia{
		{rel: "clip.mp4", absThumb: outside},
		{rel: "other.mp4", absThumb: outside},
	})
	f.scan()
	for _, rel := range []string{"clip.mp4", "other.mp4"} {
		if got := f.row(rel).ThumbnailPath; got != outside {
			t.Errorf("thumbnail of %s = %q, want it untouched at %q", rel, got, outside)
		}
	}
	if content, err := os.ReadFile(outside); err != nil || string(content) != old(outside) {
		t.Errorf("file outside the set was touched: %q, %v", content, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(outside))
	if len(entries) != 1 {
		t.Errorf("directory outside the set now holds %d entries, want 1", len(entries))
	}
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_GeneratorFailures: when a shared thumbnail cannot be
// regenerated, the rows keep their old, still working path and the shared
// file stays; the next rescan with a working generator finishes the job.
// "Failure" includes a generator that reports success without writing a
// usable file, which ffmpeg does for some inputs.
func TestMigrateThumbnails_GeneratorFailures(t *testing.T) {
	tests := []struct {
		name string
		gen  func(in, out string) error
	}{
		{"error", func(string, string) error { return errors.New("ffmpeg boom") }},
		{"success without output", func(string, string) error { return nil }},
		{"success with empty output", func(_, out string) error { return os.WriteFile(out, nil, 0o644) }},
		{"error after partial output", func(_, out string) error {
			_ = os.WriteFile(out, []byte("half a jp"), 0o644)
			return errors.New("killed")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrationFixture(t, sharedStem)
			f.gen = tt.gen
			f.scan()
			f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", old("holiday.jpg"))
			f.assertThumb("holiday.png", ".thumbnails/holiday.jpg", old("holiday.jpg"))
			// Nothing half-done may be left where a later run would adopt it.
			f.assertGone(".thumbnails/holiday.mp4.jpg")
			f.assertGone(".thumbnails/holiday.png.jpg")

			f.gen = nil
			f.scan()
			f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
			f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
			f.assertGone(".thumbnails/holiday.jpg")
		})
	}
}

// TestMigrateThumbnails_SharerAlreadyMoved: one of two sharers left the
// shared file before the rescan (the user regenerated its thumbnail, or an
// earlier migration was interrupted). The row left behind is the only one
// still storing the shared path, but the file may show the other media, so
// it must be regenerated, not inherited by rename.
func TestMigrateThumbnails_SharerAlreadyMoved(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.mp4.jpg"},
		{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
	})
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", old("holiday.mp4.jpg"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_SharerWithoutSource: a sharer whose source file is
// gone cannot be migrated and keeps pointing at the shared file, which
// therefore has to stay; the other sharer still gets its own thumbnail.
func TestMigrateThumbnails_SharerWithoutSource(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg", noSource: true},
		{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
	})
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
}

// TestMigrateThumbnails_SoftDeletedSharer: a trashed file is still on disk
// and can be restored, so its row is migrated like any other.
func TestMigrateThumbnails_SoftDeletedSharer(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg", deleted: true},
		{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
	})
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	f.assertGone(".thumbnails/holiday.jpg")
	if f.row("holiday.mp4").DeletedAt == nil {
		t.Error("migration restored a soft-deleted row")
	}
}

// TestMigrateThumbnails_UpdateFailure: when the row cannot be switched, a
// renamed thumbnail is moved back and a shared one is kept, so both rows
// keep a working thumbnail. The next rescan migrates them, and the renamed
// one still holds the original picture.
func TestMigrateThumbnails_UpdateFailure(t *testing.T) {
	f := newMigrationFixture(t, append([]seededMedia{{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"}}, sharedStem...))
	f.store.updateErr = errors.New("database is locked")
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.jpg", old("clip.jpg"))
	f.assertGone(".thumbnails/clip.mp4.jpg")
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.jpg", old("holiday.jpg"))

	f.store.updateErr = nil
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.mp4.jpg", old("clip.jpg"))
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_RollbackFailureHealsOnRescan: the worst case for a
// rename. The row cannot be switched and the file cannot be moved back, so
// the row points at nothing until the next rescan, which must find the
// thumbnail at its new path and adopt it without regenerating.
func TestMigrateThumbnails_RollbackFailureHealsOnRescan(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"}})
	f.store.updateErr = errors.New("database is locked")
	f.fs.renameErr = func(_, newPath string) error {
		if newPath == f.abs(".thumbnails/clip.jpg") {
			return fs.ErrPermission // the move back
		}
		return nil
	}
	f.scan()
	if got := f.row("clip.mp4").ThumbnailPath; got != f.abs(".thumbnails/clip.jpg") {
		t.Fatalf("row was switched despite the failed update: %q", got)
	}

	f.store.updateErr, f.fs.renameErr = nil, nil
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.mp4.jpg", old("clip.jpg"))
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_FilesystemErrors: a rename that fails for a reason
// other than a missing file (read-only mount, foreign owner) must not fall
// back to generating, which would fail the same way after an ffmpeg run per
// row on every rescan. The row keeps its old thumbnail.
func TestMigrateThumbnails_FilesystemErrors(t *testing.T) {
	tests := []struct {
		name   string
		inject func(f *faultyFS)
	}{
		{"rename permission denied", func(f *faultyFS) {
			f.renameErr = func(string, string) error { return fs.ErrPermission }
		}},
		{"rename on read-only filesystem", func(f *faultyFS) {
			f.renameErr = func(string, string) error { return errors.New("read-only file system") }
		}},
		{"mkdir fails", func(f *faultyFS) { f.mkdirErr = fs.ErrPermission }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrationFixture(t, []seededMedia{{rel: "scanned/deep.mp4", thumb: ".thumbnails/deep.jpg"}})
			tt.inject(f.fs)
			f.scan()
			f.assertThumb("scanned/deep.mp4", ".thumbnails/deep.jpg", old("deep.jpg"))
			f.assertNothingGenerated()
		})
	}
}

// TestMigrateThumbnails_ReadOnlyDirectory is the same on a real read-only
// directory instead of an injected error.
func TestMigrateThumbnails_ReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newMigrationFixture(t, []seededMedia{{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"}})
	dir := f.abs(thumb.DirName)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.jpg", old("clip.jpg"))
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_RemoveFailure: failing to delete the old shared file
// is only a leftover; the rows are migrated all the same.
func TestMigrateThumbnails_RemoveFailure(t *testing.T) {
	f := newMigrationFixture(t, sharedStem)
	f.fs.removeErr = fs.ErrPermission
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
}

// TestMigrateThumbnails_CancelledMidRun cancels the scan while the first of
// three thumbnails is being renamed (what a second click on Rescan does).
// The row in flight must end up consistent, the others untouched and still
// working, and the next rescan must finish without regenerating anything.
func TestMigrateThumbnails_CancelledMidRun(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "a.mp4", thumb: ".thumbnails/a.jpg"},
		{rel: "b.mp4", thumb: ".thumbnails/b.jpg"},
		{rel: "c.mp4", thumb: ".thumbnails/c.jpg"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	f.fs.renameErr = func(string, string) error {
		cancel()
		return nil
	}
	_ = f.scanner().Scan(ctx, f.root, nil) // may or may not report the cancellation

	f.assertThumb("a.mp4", ".thumbnails/a.mp4.jpg", old("a.jpg"))
	f.assertThumb("b.mp4", ".thumbnails/b.jpg", old("b.jpg"))
	f.assertThumb("c.mp4", ".thumbnails/c.jpg", old("c.jpg"))

	f.fs.renameErr = nil
	f.scan()
	for _, name := range []string{"a", "b", "c"} {
		f.assertThumb(name+".mp4", ".thumbnails/"+name+".mp4.jpg", old(name+".jpg"))
	}
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_CancelledBeforeStart: a scan that is already
// cancelled changes nothing.
func TestMigrateThumbnails_CancelledBeforeStart(t *testing.T) {
	f := newMigrationFixture(t, sharedStem)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.scanner().Scan(ctx, f.root, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Scan error = %v, want context.Canceled", err)
	}
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_ReportsProgress: migrated rows count as files, so
// the admin UI shows the scan moving while thumbnails are migrated.
func TestMigrateThumbnails_ReportsProgress(t *testing.T) {
	f := newMigrationFixture(t, sharedStem)
	progress := &model.ScanProgress{}
	if err := f.scanner().Scan(context.Background(), f.root, progress); err != nil {
		t.Fatal(err)
	}
	// Two rows to migrate plus the two media files walked by the scan.
	if got := progress.Copy(); got.FilesTotal != 4 || got.FilesDone != 4 {
		t.Errorf("progress = %d/%d files, want 4/4", got.FilesDone, got.FilesTotal)
	}
}

// TestThumbnailClaims pins the rule that decides between renaming and
// regenerating.
func TestThumbnailClaims(t *testing.T) {
	set := "/m/set"
	row := func(rel, stored string, typ model.MediaType) model.Media {
		return model.Media{RelPath: rel, Type: typ, ThumbnailPath: stored}
	}
	existing := map[string]model.Media{}
	for _, m := range []model.Media{
		row("holiday.mp4", "/m/set/.thumbnails/holiday.mp4.jpg", model.MediaTypeVideo), // already moved
		row("holiday.png", "/m/set/.thumbnails/holiday.jpg", model.MediaTypeImage),
		row("solo.mp4", "/m/set/.thumbnails/solo.jpg", model.MediaTypeVideo),
		row("a/clip.mp4", "/m/set/a/.thumbnails/clip.jpg", model.MediaTypeVideo), // uploaded
		row("b/clip.mp4", "/m/set/b/.thumbnails/clip.jpg", model.MediaTypeVideo), // uploaded
		row("song.mp3", "/m/set/cover.jpg", model.MediaTypeAudio),
		row("tune.mp3", "/m/set/cover.jpg", model.MediaTypeAudio),
	} {
		existing[m.RelPath] = m
	}
	claims := thumbnailClaims(existing, set)
	for path, want := range map[string]int{
		"/m/set/.thumbnails/holiday.jpg":     2, // stored by one row, old name of the other
		"/m/set/.thumbnails/holiday.mp4.jpg": 1,
		"/m/set/.thumbnails/solo.jpg":        1,
		"/m/set/a/.thumbnails/clip.jpg":      1,
		"/m/set/b/.thumbnails/clip.jpg":      1,
		"/m/set/.thumbnails/clip.jpg":        2, // old scanner name of both clips
		"/m/set/cover.jpg":                   2,
		"/m/set/.thumbnails/song.jpg":        0, // audio never had a generated thumbnail
	} {
		if got := claims[path]; got != want {
			t.Errorf("claims[%q] = %d, want %d", path, got, want)
		}
	}
}
