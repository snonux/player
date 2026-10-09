package scanner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// faultyStore is the real store whose thumbnail update can be made to fail,
// or to wait (beforeUpdate) so a test can force an interleaving.
type faultyStore struct {
	*repository.SQLite
	updateErr    error
	beforeUpdate func()
	listSets     atomic.Int32 // number of ListSets calls
}

func (s *faultyStore) ListSets(ctx context.Context) ([]model.Set, error) {
	s.listSets.Add(1)
	return s.SQLite.ListSets(ctx)
}

func (s *faultyStore) UpdateMediaThumbnail(ctx context.Context, id int64, path string) error {
	if s.beforeUpdate != nil {
		s.beforeUpdate()
	}
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.SQLite.UpdateMediaThumbnail(ctx, id, path)
}

// migrationFixture is a real media root with one set ("set") plus a real
// SQLite store holding rows as an older release would have left them.
type migrationFixture struct {
	t       *testing.T
	root    string
	setPath string
	store   *faultyStore
	setID   int64
	// gen replaces the default generator, which writes "thumb of <source>".
	gen func(in, out string) error

	mu sync.Mutex
	// generated records the sources the generator was asked to render.
	generated []string
}

func newMigrationFixture(t *testing.T, seed []seededMedia) *migrationFixture {
	t.Helper()
	ctx := context.Background()
	f := &migrationFixture{t: t, root: t.TempDir()}
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

// seed creates one row and its files. A stored thumbnail in a .thumbnails
// directory is filled with old(<its path>); rows seeded with the same path
// share that file, like colliding files did.
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

// scanner builds a production scanner (real filesystem, real thumb.FSMaker)
// around the fixture's store and a generator that writes text files.
func (f *migrationFixture) scanner() *FSScanner {
	gen := &thumb.MockGenerator{GenerateFunc: func(_ context.Context, in, out string, _ float64) error {
		f.mu.Lock()
		f.generated = append(f.generated, in)
		gen := f.gen
		f.mu.Unlock()
		if gen != nil {
			return gen(in, out)
		}
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}}
	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		return &model.Metadata{}, nil
	}}
	return NewFSScanner(f.store, prober, gen, clock.RealClock{}, f.root)
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

// assertFresh checks that rel has its own, newly generated thumbnail at its
// current path.
func (f *migrationFixture) assertFresh(rel string) {
	f.t.Helper()
	want, err := filepath.Rel(f.setPath, thumb.ThumbnailPathFor(f.abs(rel)))
	if err != nil {
		f.t.Fatal(err)
	}
	f.assertThumb(rel, filepath.ToSlash(want), "thumb of "+f.abs(rel))
}

func (f *migrationFixture) assertGone(rel string) {
	f.t.Helper()
	if _, err := os.Stat(f.abs(rel)); !errors.Is(err, fs.ErrNotExist) {
		f.t.Errorf("%q should not exist (stat err = %v)", rel, err)
	}
}

func (f *migrationFixture) assertNothingGenerated() {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.generated) != 0 {
		f.t.Errorf("generator ran for %v, want no generation", f.generated)
	}
}

// assertNoTemporaries fails if a generator run left a temporary file in one
// of the given set-relative directories.
func (f *migrationFixture) assertNoTemporaries(dirs ...string) {
	f.t.Helper()
	for _, dir := range dirs {
		left, _ := filepath.Glob(filepath.Join(f.abs(dir), ".tmp-*"))
		if len(left) != 0 {
			f.t.Errorf("temporary files left behind: %v", left)
		}
	}
}

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
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", "thumb of "+f.abs("holiday.mp4"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.png.jpg", "thumb of "+f.abs("holiday.png"))
	f.assertGone(".thumbnails/holiday.jpg")
	f.assertNoTemporaries(".thumbnails")
}

// TestMigrateThumbnails_SameNameInTwoFolders covers the old scanner keeping
// all thumbnails of a set flat in one directory.
func TestMigrateThumbnails_SameNameInTwoFolders(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "a/clip.mp4", thumb: ".thumbnails/clip.jpg"},
		{rel: "b/clip.mp4", thumb: ".thumbnails/clip.jpg"},
	})
	f.scan()
	f.assertThumb("a/clip.mp4", "a/.thumbnails/clip.mp4.jpg", "thumb of "+f.abs("a/clip.mp4"))
	f.assertThumb("b/clip.mp4", "b/.thumbnails/clip.mp4.jpg", "thumb of "+f.abs("b/clip.mp4"))
	f.assertGone(".thumbnails/clip.jpg")
}

// TestMigrateThumbnails_UnsharedOldNames: a thumbnail under an old name is
// regenerated even when only one row uses it, because nothing proves which
// media the old file shows. Covers both old layouts: the scanner's (set
// root) and the upload's (beside the source).
func TestMigrateThumbnails_UnsharedOldNames(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"},
		{rel: "scanned/deep.mp4", thumb: ".thumbnails/deep.jpg"},
		{rel: "up/load.png", thumb: "up/.thumbnails/load.jpg"},
	})
	f.scan()
	for _, rel := range []string{"clip.mp4", "scanned/deep.mp4", "up/load.png"} {
		f.assertFresh(rel)
	}
	for _, stale := range []string{".thumbnails/clip.jpg", ".thumbnails/deep.jpg", "up/.thumbnails/load.jpg"} {
		f.assertGone(stale)
	}
}

// forceSwitchAfter makes the scan run in the one order in which deleting a
// migrated row's old thumbnail can hurt another file, and returns the
// scanner to use. The old thumbnail of "0sub/holiday.mp4.png" (old scanner
// layout: flat in the set's .thumbnails) is ".thumbnails/holiday.mp4.jpg",
// which is also the current thumbnail path of "holiday.mp4".
//
// With a single worker the files are handled in walk order, the image
// first. Its row switch is held back until the video's thumbnail has been
// generated at the shared path, so the switcher decides about deleting
// "the image's old file" while that file already is the video's new
// thumbnail and no row stores it yet.
func (f *migrationFixture) forceSwitchAfter(videoRel string) *FSScanner {
	generated := make(chan struct{})
	var once, held sync.Once
	f.gen = func(in, out string) error {
		err := os.WriteFile(out, []byte("thumb of "+in), 0o644)
		if in == f.abs(videoRel) {
			once.Do(func() { close(generated) })
		}
		return err
	}
	f.store.beforeUpdate = func() {
		held.Do(func() {
			select {
			case <-generated:
			case <-time.After(10 * time.Second):
				f.t.Error("the video's thumbnail was never generated")
			}
		})
	}
	s := f.scanner()
	s.workers = 1
	return s
}

// TestMigrateThumbnails_OldPathIsAnotherRowsNewPath: the old thumbnail of
// one row is the new thumbnail of another. The path must not be deleted as
// "the old file" of the first row, and both end up with their own picture.
func TestMigrateThumbnails_OldPathIsAnotherRowsNewPath(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "0sub/holiday.mp4.png", thumb: ".thumbnails/holiday.mp4.jpg"},
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
	})
	if err := f.forceSwitchAfter("holiday.mp4").Scan(context.Background(), f.root, nil); err != nil {
		t.Fatal(err)
	}
	f.assertFresh("holiday.mp4")
	f.assertFresh("0sub/holiday.mp4.png")
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestMigrateThumbnails_NewFileOnAnIndexedRowsOldPath: the same clash with a
// file added since the last scan, which has no row at all while the indexed
// row is switched.
func TestMigrateThumbnails_NewFileOnAnIndexedRowsOldPath(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "0sub/holiday.mp4.png", thumb: ".thumbnails/holiday.mp4.jpg"}})
	f.write(f.abs("holiday.mp4"), "source") // on disk, not indexed yet
	if err := f.forceSwitchAfter("holiday.mp4").Scan(context.Background(), f.root, nil); err != nil {
		t.Fatal(err)
	}
	f.assertFresh("0sub/holiday.mp4.png")
	f.assertFresh("holiday.mp4")
}

// TestMigrateThumbnails_MissingThumbnailIsRegenerated: a row whose thumbnail
// is at its current path but gone from disk (deleted by hand, or lost to an
// upload racing a migration) is repaired by the next rescan.
func TestMigrateThumbnails_MissingThumbnailIsRegenerated(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "a/clip.mp4", thumb: "a/.thumbnails/clip.mp4.jpg"},
		{rel: "kept.mp4", thumb: ".thumbnails/kept.mp4.jpg"},
	})
	if err := os.Remove(f.abs("a/.thumbnails/clip.mp4.jpg")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	f.assertFresh("a/clip.mp4")
	f.assertThumb("kept.mp4", ".thumbnails/kept.mp4.jpg", old("kept.mp4.jpg")) // present: not touched
}

// TestScan_SweepsStaleTemporaryThumbnails: a generation killed with the
// server leaves its temporary file behind. A rescan removes such files once
// they are old, but not a recent one (a generation may be running) and
// nothing that is not a temporary file.
func TestScan_SweepsStaleTemporaryThumbnails(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "new.mp4", thumb: ".thumbnails/new.mp4.jpg"},
		{rel: "a/new.mp4", thumb: "a/.thumbnails/new.mp4.jpg"},
	})
	longAgo := time.Now().Add(-2 * time.Hour)
	age := func(rel string) {
		t.Helper()
		f.write(f.abs(rel), "half a jp")
		if err := os.Chtimes(f.abs(rel), longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
	}
	age(".thumbnails/.tmp-111.jpg")
	age("a/.thumbnails/.tmp-222.jpg")
	age(".thumbnails/old-but-not-temporary.jpg")
	age(".tmp-333.jpg") // not in a .thumbnails directory
	f.write(f.abs(".thumbnails/.tmp-444.jpg"), "in progress")
	if err := os.Chtimes(f.abs(".thumbnails/new.mp4.jpg"), longAgo, longAgo); err != nil {
		t.Fatal(err)
	}

	f.scan()
	f.assertGone(".thumbnails/.tmp-111.jpg")
	f.assertGone("a/.thumbnails/.tmp-222.jpg")
	for _, kept := range []string{".thumbnails/old-but-not-temporary.jpg", ".tmp-333.jpg", ".thumbnails/.tmp-444.jpg", ".thumbnails/new.mp4.jpg"} {
		if _, err := os.Stat(f.abs(kept)); err != nil {
			t.Errorf("%q should have been kept: %v", kept, err)
		}
	}
}

// TestMigrateThumbnails_LeftoverAtDestination: a file already sitting at a
// row's new path is of unknown origin (garbage collection never deleted
// thumbnails, so "movie.mp4.jpg" may be the old thumbnail of a long purged
// "movie.mp4.jpg" image) and may be empty. It must be replaced, never
// adopted, and the row's thumbnail must not be lost over it.
func TestMigrateThumbnails_LeftoverAtDestination(t *testing.T) {
	for name, leftover := range map[string]string{"orphan of another file": "someone else's picture", "empty file": ""} {
		t.Run(name, func(t *testing.T) {
			f := newMigrationFixture(t, []seededMedia{{rel: "movie.mp4", thumb: ".thumbnails/movie.jpg"}})
			f.write(f.abs(".thumbnails/movie.mp4.jpg"), leftover)
			f.scan()
			f.assertFresh("movie.mp4")
			f.assertGone(".thumbnails/movie.jpg")
		})
	}
}

// TestMigrateThumbnails_LeftoverSurvivesFailedGeneration: with a file at the
// destination and a failing generator, neither file is touched and the row
// keeps its old thumbnail.
func TestMigrateThumbnails_LeftoverSurvivesFailedGeneration(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "movie.mp4", thumb: ".thumbnails/movie.jpg"}})
	f.write(f.abs(".thumbnails/movie.mp4.jpg"), "leftover")
	f.gen = func(string, string) error { return errors.New("ffmpeg boom") }
	f.scan()
	f.assertThumb("movie.mp4", ".thumbnails/movie.jpg", old("movie.jpg"))
	if got, _ := os.ReadFile(f.abs(".thumbnails/movie.mp4.jpg")); string(got) != "leftover" {
		t.Errorf("file at the destination was changed to %q", got)
	}
}

// TestMigrateThumbnails_DestinationStoredByUnmigratableRow: the new path of
// holiday.mp4 is still stored by a row that cannot be migrated because its
// source file is gone. That must not hold holiday.mp4 back.
func TestMigrateThumbnails_DestinationStoredByUnmigratableRow(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg"},
		{rel: "holiday.mp4.png", thumb: ".thumbnails/holiday.mp4.jpg", noSource: true},
	})
	f.scan()
	f.assertFresh("holiday.mp4")
	f.assertGone(".thumbnails/holiday.jpg")
	if got := f.row("holiday.mp4.png").ThumbnailPath; got != f.abs(".thumbnails/holiday.mp4.jpg") {
		t.Errorf("row without source was changed: %q", got)
	}
}

// TestMigrateThumbnails_LeavesOthersAlone lists everything a rescan must not
// touch: thumbnails at their current path, thumbnails that are ordinary
// files chosen for the purpose (an audio cover, an SVG standing in for
// itself), and rows whose source file is gone.
func TestMigrateThumbnails_LeavesOthersAlone(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "new.mp4", thumb: ".thumbnails/new.mp4.jpg"},
		{rel: "sub/new.mp4", thumb: "sub/.thumbnails/new.mp4.jpg"},
		{rel: "logo.svg", thumb: "logo.svg"},
		{rel: "album/art.png", thumb: "album/.thumbnails/art.png.jpg"},
		{rel: "album/track.mp3", thumb: "album/art.png"}, // audio cover
		{rel: "gone.mp4", thumb: ".thumbnails/gone.jpg", noSource: true},
		{rel: "gone-bare.mp4", noSource: true},
	})
	f.scan()
	f.scan() // a second rescan must be a no-op as well

	f.assertThumb("new.mp4", ".thumbnails/new.mp4.jpg", old("new.mp4.jpg"))
	f.assertThumb("sub/new.mp4", "sub/.thumbnails/new.mp4.jpg", old("new.mp4.jpg"))
	f.assertThumb("gone.mp4", ".thumbnails/gone.jpg", old("gone.jpg"))
	f.assertThumb("logo.svg", "logo.svg", "source")
	f.assertThumb("album/track.mp3", "album/art.png", "source")
	if got := f.row("gone-bare.mp4").ThumbnailPath; got != "" {
		t.Errorf("thumbnail of gone-bare.mp4 = %q, want none", got)
	}
	f.assertNothingGenerated()
}

// TestScan_RetriesMediaWithoutThumbnail: a video indexed without a thumbnail
// and an image standing in as its own (generation failed when they were
// indexed) get one on the next rescan. The image is a media file and must
// of course not be deleted as "the old thumbnail".
func TestScan_RetriesMediaWithoutThumbnail(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "bare.mp4"},
		{rel: "a/own.png", thumb: "a/own.png"},
		{rel: "trashed.mp4", deleted: true},
	})
	f.scan()
	f.assertFresh("bare.mp4")
	f.assertFresh("a/own.png")
	f.assertFresh("trashed.mp4")
	if got, err := os.ReadFile(f.abs("a/own.png")); err != nil || string(got) != "source" {
		t.Errorf("the image itself was deleted or changed: %q, %v", got, err)
	}
}

// TestScan_RetryWithoutThumbnailIsBoundedAndHarmless: while generation keeps
// failing, such rows stay as they are and each rescan tries exactly once.
func TestScan_RetryWithoutThumbnailIsBoundedAndHarmless(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "bare.mp4"}, {rel: "own.png", thumb: "own.png"}})
	f.gen = func(string, string) error { return errors.New("ffmpeg boom") }
	f.scan()
	f.scan()
	if got := f.row("bare.mp4").ThumbnailPath; got != "" {
		t.Errorf("thumbnail of bare.mp4 = %q, want none", got)
	}
	f.assertThumb("own.png", "own.png", "source")
	f.mu.Lock()
	attempts := len(f.generated)
	f.mu.Unlock()
	if attempts != 4 {
		t.Errorf("generator ran %d times for 2 rows in 2 rescans, want 4", attempts)
	}
}

// TestMigrateThumbnails_IsRepeatable: once migrated, a rescan generates
// nothing and changes nothing.
func TestMigrateThumbnails_IsRepeatable(t *testing.T) {
	f := newMigrationFixture(t, sharedStem)
	f.scan()
	f.mu.Lock()
	f.generated = nil
	f.mu.Unlock()
	f.scan()
	f.assertNothingGenerated()
	f.assertFresh("holiday.mp4")
	f.assertFresh("holiday.png")
}

// TestMigrateThumbnails_StoredPathOutsideSet: a stale absolute path (media
// root moved, database copied from another host) may point anywhere. The
// scanner must not generate for, or delete, files outside the set.
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
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_GeneratorFailures: when no new thumbnail can be
// generated, the rows keep their old, still working path, the shared file
// stays, and nothing half-done is left at the new paths; the next rescan
// with a working generator finishes the job. "Failure" includes a generator
// that reports success without writing a usable file, which ffmpeg does for
// some inputs.
func TestMigrateThumbnails_GeneratorFailures(t *testing.T) {
	tests := []struct {
		name string
		gen  func(in, out string) error
	}{
		{"error", func(string, string) error { return errors.New("ffmpeg boom") }},
		{"success without output", func(_, out string) error { return os.Remove(out) }},
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
			f.assertGone(".thumbnails/holiday.mp4.jpg")
			f.assertGone(".thumbnails/holiday.png.jpg")
			f.assertNoTemporaries(".thumbnails")

			f.gen = nil
			f.scan()
			f.assertFresh("holiday.mp4")
			f.assertFresh("holiday.png")
			f.assertGone(".thumbnails/holiday.jpg")
		})
	}
}

// TestMigrateThumbnails_SharerAlreadyMoved: one of two sharers left the
// shared file before the rescan (the user regenerated its thumbnail, or an
// earlier rescan was interrupted). The row left behind must get its own
// thumbnail too, not inherit the file that may show the other media.
func TestMigrateThumbnails_SharerAlreadyMoved(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.mp4.jpg"},
		{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
	})
	f.scan()
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.mp4.jpg", old("holiday.mp4.jpg"))
	f.assertFresh("holiday.png")
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
	f.assertFresh("holiday.png")
}

// TestMigrateThumbnails_SoftDeletedSharer: a trashed file is still on disk
// and can be restored, so its row is migrated like any other and stays
// trashed.
func TestMigrateThumbnails_SoftDeletedSharer(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "holiday.mp4", thumb: ".thumbnails/holiday.jpg", deleted: true},
		{rel: "holiday.png", thumb: ".thumbnails/holiday.jpg"},
	})
	f.scan()
	f.assertFresh("holiday.mp4")
	f.assertFresh("holiday.png")
	f.assertGone(".thumbnails/holiday.jpg")
	if f.row("holiday.mp4").DeletedAt == nil {
		t.Error("migration restored a soft-deleted row")
	}
}

// TestMigrateThumbnails_UpdateFailure: when a row cannot be switched it
// keeps its old thumbnail, which must not be deleted. The next rescan
// switches it.
func TestMigrateThumbnails_UpdateFailure(t *testing.T) {
	f := newMigrationFixture(t, append([]seededMedia{{rel: "clip.mp4", thumb: ".thumbnails/clip.jpg"}}, sharedStem...))
	f.store.updateErr = errors.New("database is locked")
	f.scan()
	f.assertThumb("clip.mp4", ".thumbnails/clip.jpg", old("clip.jpg"))
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.jpg", old("holiday.jpg"))

	f.store.updateErr = nil
	f.scan()
	for _, rel := range []string{"clip.mp4", "holiday.mp4", "holiday.png"} {
		f.assertFresh(rel)
	}
	f.assertGone(".thumbnails/clip.jpg")
	f.assertGone(".thumbnails/holiday.jpg")
}

// TestScan_UnwritableFolder: a folder the server cannot write to (media
// copied in by another user, read-only export) must not fail the scan. Its
// new files are indexed without a generated thumbnail, its indexed rows keep
// their old thumbnail, later sets are still scanned, and the generator is
// not started for it, however often the folder is rescanned.
func TestScan_UnwritableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newMigrationFixture(t, []seededMedia{{rel: "ro/indexed.mp4", thumb: ".thumbnails/indexed.jpg"}})
	f.write(f.abs("ro/new.mp4"), "source")
	f.write(f.abs("ro/new.png"), "source")
	later := filepath.Join(f.root, "zzz-later-set", "song.mp3")
	f.write(later, "source")
	if err := os.Chmod(f.abs("ro"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.abs("ro"), 0o755) })

	f.scan()
	f.scan()

	if got := f.row("ro/new.mp4").ThumbnailPath; got != "" {
		t.Errorf("thumbnail of the new video = %q, want none", got)
	}
	f.assertThumb("ro/new.png", "ro/new.png", "source")
	f.assertThumb("ro/indexed.mp4", ".thumbnails/indexed.jpg", old("indexed.jpg"))
	f.assertNothingGenerated()

	sets, err := f.store.ListSets(context.Background())
	if err != nil || len(sets) != 2 {
		t.Fatalf("sets = %v (err %v), want the set after the read-only one to be scanned too", sets, err)
	}
}

// TestMigrateThumbnails_RemoveFailure: failing to delete the old file is
// only a leftover; the row is migrated all the same. The old scanner kept
// the thumbnail in the set's .thumbnails directory, read-only here, while
// the new one goes next to the source.
func TestMigrateThumbnails_RemoveFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newMigrationFixture(t, []seededMedia{{rel: "a/clip.mp4", thumb: ".thumbnails/clip.jpg"}})
	dir := f.abs(thumb.DirName)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	f.scan()
	f.assertFresh("a/clip.mp4")
	if _, err := os.Stat(f.abs(".thumbnails/clip.jpg")); err != nil {
		t.Errorf("expected the undeletable old file to remain: %v", err)
	}
}

// TestMigrateThumbnails_CancelledMidRun cancels the scan while the first
// thumbnail is being generated (what a second click on Rescan does). Every
// row must still point at an existing thumbnail, old or new, and the next
// rescan must finish the migration.
func TestMigrateThumbnails_CancelledMidRun(t *testing.T) {
	rels := []string{"a.mp4", "b.mp4", "c.mp4", "d.mp4"}
	var seed []seededMedia
	for _, rel := range rels {
		seed = append(seed, seededMedia{rel: rel, thumb: ".thumbnails/shared.jpg"})
	}
	f := newMigrationFixture(t, seed)
	ctx, cancel := context.WithCancel(context.Background())
	f.gen = func(in, out string) error {
		cancel()
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}
	_ = f.scanner().Scan(ctx, f.root, nil) // may or may not report the cancellation

	migrated := 0
	for _, rel := range rels {
		row := f.row(rel)
		if _, err := os.Stat(row.ThumbnailPath); err != nil {
			t.Errorf("%s points at a missing thumbnail after the cancelled scan: %v", rel, err)
		}
		if row.ThumbnailPath != f.abs(".thumbnails/shared.jpg") {
			migrated++
		}
	}
	if migrated == len(rels) {
		t.Error("cancelling had no effect, the test proves nothing")
	}
	f.assertNoTemporaries(".thumbnails")

	f.gen = nil
	f.scan()
	for _, rel := range rels {
		f.assertFresh(rel)
	}
	f.assertGone(".thumbnails/shared.jpg")
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
	// Scan must give up right after it got its turn, before touching the
	// database or the disk: a rescan superseded while it waited for the
	// previous one must not start working with a dead context.
	if n := f.store.listSets.Load(); n != 0 {
		t.Errorf("the cancelled scan queried the store %d times", n)
	}
	f.assertThumb("holiday.mp4", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertThumb("holiday.png", ".thumbnails/holiday.jpg", old("holiday.jpg"))
	f.assertNothingGenerated()
}

// TestMigrateThumbnails_ReportsProgress: migrated rows are files of the scan
// like any other, so the admin UI shows the scan moving while thumbnails
// are regenerated.
func TestMigrateThumbnails_ReportsProgress(t *testing.T) {
	f := newMigrationFixture(t, sharedStem)
	progress := &model.ScanProgress{}
	if err := f.scanner().Scan(context.Background(), f.root, progress); err != nil {
		t.Fatal(err)
	}
	if got := progress.Copy(); got.FilesTotal != 2 || got.FilesDone != 2 {
		t.Errorf("progress = %d/%d files, want 2/2", got.FilesDone, got.FilesTotal)
	}
}

// TestScan_OverlappingScansRunOneAtATime: a rescan started while another is
// still running must wait for it. The first scan is held inside the
// generator; if the second were not serialised it would reach the generator
// too, for the same rows.
func TestScan_OverlappingScansRunOneAtATime(t *testing.T) {
	f := newMigrationFixture(t, sharedStem)
	var active, peak atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	f.gen = func(in, out string) error {
		if n := active.Add(1); n > peak.Load() {
			peak.Store(n)
		}
		defer active.Add(-1)
		entered <- struct{}{}
		<-release
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}
	s := f.scanner()
	s.workers = 1

	var wg sync.WaitGroup
	scan := func() {
		defer wg.Done()
		if err := s.Scan(context.Background(), f.root, nil); err != nil {
			t.Errorf("scan: %v", err)
		}
	}
	wg.Add(2)
	go scan()
	<-entered // the first scan is inside the generator
	go scan()
	time.Sleep(100 * time.Millisecond) // give an unserialised second scan time to get there
	close(release)
	wg.Wait()

	if peak.Load() != 1 {
		t.Errorf("generator ran %d times at once, want scans to run one at a time", peak.Load())
	}
	f.assertFresh("holiday.mp4")
	f.assertFresh("holiday.png")
}

// TestScan_ThumbnailsDirectoryIsNotASet: the server never creates a
// .thumbnails directory directly in the media root, but if one is there it
// is not a set: indexing it would turn its files into media whose
// "thumbnail" is the file itself, in a directory treated as disposable.
func TestScan_ThumbnailsDirectoryIsNotASet(t *testing.T) {
	f := newMigrationFixture(t, nil)
	pic := filepath.Join(f.root, thumb.DirName, "pic.svg")
	f.write(pic, "<svg/>")
	f.scan()
	sets, err := f.store.ListSets(context.Background())
	if err != nil || len(sets) != 1 || sets[0].RootPath != "set" {
		t.Errorf("sets = %+v (err %v), want only \"set\"", sets, err)
	}
	if _, err := os.Stat(pic); err != nil {
		t.Errorf("file in the root's .thumbnails directory was touched: %v", err)
	}
}

// TestStaleThumbnails pins which rows get a thumbnail generated, and why.
// As in a real scan, files below a hidden directory are never "seen" on
// disk: the only thing telling a media file in a .thumbnails directory from
// a generated thumbnail is that a row exists for it.
func TestStaleThumbnails(t *testing.T) {
	const set = "/m/set"
	row := func(rel, stored string) model.Media {
		return model.Media{RelPath: rel, Type: mediatype.TypeForExt(rel), ThumbnailPath: stored}
	}
	rows := []model.Media{
		row("old.mp4", "/m/set/.thumbnails/old.jpg"),             // old name
		row("a/old.png", "/m/set/.thumbnails/old.jpg"),           // old name (old scanner layout)
		row("lost.mp4", "/m/set/.thumbnails/lost.mp4.jpg"),       // current path, file missing
		row("current.mp4", "/m/set/.thumbnails/current.mp4.jpg"), // current path, file present
		row("none.mp4", ""),                                         // video without thumbnail
		row("own.png", "/m/set/own.png"),                            // image as its own thumbnail
		row("logo.svg", "/m/set/logo.svg"),                          // an SVG always is
		row("logo2.SVG", ""),                                        // ... whatever is stored
		row("song.mp3", "/m/set/.thumbnails/song.jpg"),              // audio is never touched
		row("silent.mp3", ""),                                       //
		row("gone.mp4", "/m/set/.thumbnails/gone.jpg"),              // source not on disk
		row("gone2.mp4", ""),                                        //
		row("outside.mp4", "/elsewhere/.thumbnails/outside.jpg"),    // not inside the set
		row("escape.mp4", "/m/set/../other/.thumbnails/escape.jpg"), // not inside the set either
		row("chosen.mp4", "/m/set/poster.jpg"),                      // an ordinary file as thumbnail
		row(".thumbnails/pic.png", ""),                              // media file in a .thumbnails dir
		row("uses-media.mp4", "/m/set/.thumbnails/pic.png"),         // "thumbnail" is that media file
		row("deep.mp4", "/m/set/.thumbnails/sub/deep.jpg"),          // not directly in .thumbnails
	}
	notOnDisk := map[string]bool{"gone.mp4": true, "gone2.mp4": true, ".thumbnails/pic.png": true}
	existing := map[string]model.Media{}
	seen := map[string]struct{}{}
	for _, m := range rows {
		existing[m.RelPath] = m
		if !notOnDisk[m.RelPath] {
			seen[m.RelPath] = struct{}{}
		}
	}
	exists := func(path string) bool { return path != "/m/set/.thumbnails/lost.mp4.jpg" }

	got := staleThumbnails(existing, seen, set, exists)
	want := map[string]staleKind{
		"old.mp4": staleOldName, "a/old.png": staleOldName, "lost.mp4": staleMissing,
		"none.mp4": staleNone, "own.png": staleNone,
	}
	for _, m := range rows {
		if got[m.RelPath] != want[m.RelPath] {
			t.Errorf("stale[%q] = %d, want %d", m.RelPath, got[m.RelPath], want[m.RelPath])
		}
	}
}
