package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/scanner"
	"codeberg.org/snonux/player/internal/thumb"
)

// writingThumbGen writes "thumb of <input>" to the output path so a test can
// tell which source a thumbnail file on disk was generated from.
func writingThumbGen() *mockThumbGenerator {
	return &mockThumbGenerator{GenerateFunc: func(_ context.Context, in, out string, _ float64) error {
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}}
}

type updateRecorder struct{ updates int }

func (u *updateRecorder) UpdateMedia(context.Context, *model.Media) error {
	u.updates++
	return nil
}

// TestImportMediaFile_SameStemGetsOwnThumbnail is the upload/podcast side of
// the stem collision: holiday.mp4 and holiday.png in one folder used to
// write the same holiday.jpg.
func TestImportMediaFile_SameStemGetsOwnThumbnail(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	video := &model.Media{AbsPath: filepath.Join(dir, "holiday.mp4"), Type: model.MediaTypeVideo}
	image := &model.Media{AbsPath: filepath.Join(dir, "holiday.png"), Type: model.MediaTypeImage}
	store := &updateRecorder{}
	for _, m := range []*model.Media{video, image} {
		if err := ImportMediaFile(ctx, store, m, &mockProber{}, writingThumbGen()); err != nil {
			t.Fatalf("import %s: %v", m.AbsPath, err)
		}
	}

	if video.ThumbnailPath == image.ThumbnailPath {
		t.Fatalf("both files share thumbnail %q", video.ThumbnailPath)
	}
	if want := filepath.Join(dir, ".thumbnails", "holiday.mp4.jpg"); video.ThumbnailPath != want {
		t.Errorf("video thumbnail = %q, want %q", video.ThumbnailPath, want)
	}
	for _, m := range []*model.Media{video, image} {
		got, err := os.ReadFile(m.ThumbnailPath)
		if err != nil || string(got) != "thumb of "+m.AbsPath {
			t.Errorf("thumbnail of %s holds %q (err %v)", m.AbsPath, got, err)
		}
	}
	if store.updates != 2 {
		t.Errorf("updates = %d, want 2", store.updates)
	}
}

func TestImportMediaFile_ThumbnailNegativeCases(t *testing.T) {
	ctx := context.Background()
	failing := &mockThumbGenerator{GenerateFunc: func(context.Context, string, string, float64) error {
		return errors.New("ffmpeg boom")
	}}
	tests := []struct {
		name      string
		file      string
		mediaType model.MediaType
		gen       *mockThumbGenerator
		wantErr   bool
		wantThumb string // relative to the media directory; "" for none
	}{
		{"svg is its own thumbnail", "logo.svg", model.MediaTypeImage, writingThumbGen(), false, "logo.svg"},
		{"audio gets no thumbnail", "song.mp3", model.MediaTypeAudio, writingThumbGen(), false, ""},
		{"generator failure leaves no path behind", "clip.mp4", model.MediaTypeVideo, failing, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			m := &model.Media{AbsPath: filepath.Join(dir, tt.file), Type: tt.mediaType}
			store := &updateRecorder{}
			err := ImportMediaFile(ctx, store, m, &mockProber{}, tt.gen)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			want := ""
			if tt.wantThumb != "" {
				want = filepath.Join(dir, tt.wantThumb)
			}
			if m.ThumbnailPath != want {
				t.Errorf("thumbnail = %q, want %q", m.ThumbnailPath, want)
			}
			if tt.wantErr && store.updates != 0 {
				t.Errorf("media row updated despite the failure")
			}
		})
	}
}

// thumbFixture is a real SQLite store and a real set directory, shared by
// the media service and the scanner like in production.
type thumbFixture struct {
	t       *testing.T
	root    string
	setPath string
	store   *repository.SQLite
	setID   int64
	admin   int64
	svc     *mediaService
}

func newThumbFixture(t *testing.T) *thumbFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	store, err := repository.Open(filepath.Join(t.TempDir(), "player.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f := &thumbFixture{t: t, root: t.TempDir(), store: store}
	f.setPath = filepath.Join(f.root, "set")
	if f.admin, err = store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "h", IsAdmin: true, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if f.setID, err = store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "set", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	f.svc = NewMediaService(store, &clock.MockClock{T: now}, f.root, writingThumbGen(), &mockProber{})
	return f
}

func (f *thumbFixture) abs(rel string) string {
	return filepath.Join(f.setPath, filepath.FromSlash(rel))
}

// add indexes a media file; thumbRel is its stored thumbnail, set-relative
// ("" for none, absolute paths are stored as given). A generated thumbnail
// file is created holding "old".
func (f *thumbFixture) add(rel, thumbRel string) int64 {
	f.t.Helper()
	f.writeFile(f.abs(rel), "source")
	m := &model.Media{SetID: f.setID, RelPath: rel, FileName: filepath.Base(rel), AbsPath: f.abs(rel), Type: model.MediaTypeVideo, CreatedAt: time.Now()}
	if filepath.Ext(rel) == ".png" {
		m.Type = model.MediaTypeImage
	}
	switch {
	case filepath.IsAbs(thumbRel):
		m.ThumbnailPath = thumbRel
	case thumbRel != "":
		m.ThumbnailPath = f.abs(thumbRel)
	}
	if thumb.IsGenerated(m.ThumbnailPath) {
		f.writeFile(m.ThumbnailPath, "old")
	}
	id, err := f.store.CreateMedia(context.Background(), m)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *thumbFixture) writeFile(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *thumbFixture) regenerate(id int64) {
	f.t.Helper()
	if err := f.svc.RegenerateThumbnail(context.Background(), id, f.admin); err != nil {
		f.t.Fatalf("regenerate: %v", err)
	}
}

// assertThumb checks the stored thumbnail of media id and the file content.
func (f *thumbFixture) assertThumb(id int64, wantRel, wantContent string) {
	f.t.Helper()
	m, err := f.store.GetMediaByID(context.Background(), id)
	if err != nil || m == nil {
		f.t.Fatalf("get media %d: %v", id, err)
	}
	if m.ThumbnailPath != f.abs(wantRel) {
		f.t.Errorf("thumbnail of %s = %q, want %q", m.RelPath, m.ThumbnailPath, f.abs(wantRel))
		return
	}
	got, err := os.ReadFile(m.ThumbnailPath)
	if err != nil || string(got) != wantContent {
		f.t.Errorf("thumbnail of %s holds %q (err %v), want %q", m.RelPath, got, err, wantContent)
	}
}

func (f *thumbFixture) fresh(rel string) string { return "thumb of " + f.abs(rel) }

// TestRegenerateThumbnail_Destination: whatever path is stored, the new
// thumbnail is written to the media's own current path beside the source,
// and a generated thumbnail it replaces does not stay behind as an orphan.
func TestRegenerateThumbnail_Destination(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		stored   string
		want     string
		wantGone bool // the previously stored file must be deleted
	}{
		{"no thumbnail yet", "a/video.mp4", "", "a/.thumbnails/video.mp4.jpg", false},
		{"current path is overwritten in place", "a/video.mp4", "a/.thumbnails/video.mp4.jpg", "a/.thumbnails/video.mp4.jpg", false},
		{"old upload name is replaced", "a/video.mp4", "a/.thumbnails/video.jpg", "a/.thumbnails/video.mp4.jpg", true},
		{"old scanner name in the set root is replaced", "a/video.mp4", ".thumbnails/video.jpg", "a/.thumbnails/video.mp4.jpg", true},
		{"image serving as its own thumbnail is kept", "a/photo.png", "a/photo.png", "a/.thumbnails/photo.png.jpg", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newThumbFixture(t)
			id := f.add(tt.source, tt.stored)
			f.regenerate(id)
			f.assertThumb(id, tt.want, f.fresh(tt.source))
			if tt.stored == "" || tt.stored == tt.want {
				return
			}
			_, err := os.Stat(f.abs(tt.stored))
			if gone := errors.Is(err, os.ErrNotExist); gone != tt.wantGone {
				t.Errorf("previous thumbnail %q gone = %v, want %v", tt.stored, gone, tt.wantGone)
			}
		})
	}
}

// TestRegenerateThumbnail_KeepsFilesItMustNotDelete: the replaced file stays
// when another row (even a trashed one) still points at it, and when the
// stored path is a stale one outside the source's own directory tree.
func TestRegenerateThumbnail_KeepsFilesItMustNotDelete(t *testing.T) {
	t.Run("shared with another row", func(t *testing.T) {
		f := newThumbFixture(t)
		video := f.add("holiday.mp4", ".thumbnails/holiday.jpg")
		image := f.add("holiday.png", ".thumbnails/holiday.jpg")
		f.regenerate(video)
		f.assertThumb(video, ".thumbnails/holiday.mp4.jpg", f.fresh("holiday.mp4"))
		f.assertThumb(image, ".thumbnails/holiday.jpg", "old")
	})
	t.Run("shared with a trashed row", func(t *testing.T) {
		f := newThumbFixture(t)
		video := f.add("holiday.mp4", ".thumbnails/holiday.jpg")
		image := f.add("holiday.png", ".thumbnails/holiday.jpg")
		if err := f.store.SoftDeleteMedia(context.Background(), image); err != nil {
			t.Fatal(err)
		}
		f.regenerate(video)
		if _, err := os.Stat(f.abs(".thumbnails/holiday.jpg")); err != nil {
			t.Errorf("thumbnail of the trashed row was removed: %v", err)
		}
	})
	t.Run("stale path outside the source's tree", func(t *testing.T) {
		f := newThumbFixture(t)
		outside := filepath.Join(t.TempDir(), thumb.DirName, "video.jpg")
		id := f.add("a/video.mp4", outside)
		f.regenerate(id)
		f.assertThumb(id, "a/.thumbnails/video.mp4.jpg", f.fresh("a/video.mp4"))
		if _, err := os.Stat(outside); err != nil {
			t.Errorf("file outside the media's tree was removed: %v", err)
		}
	})
}

// TestRegenerateThumbnail_FailureKeepsStoredPath: a failed regeneration must
// not persist a path to a thumbnail that was never written, nor delete the
// thumbnail the row still uses.
func TestRegenerateThumbnail_FailureKeepsStoredPath(t *testing.T) {
	f := newThumbFixture(t)
	id := f.add("video.mp4", ".thumbnails/video.jpg")
	gen := &mockThumbGenerator{GenerateFunc: func(context.Context, string, string, float64) error {
		return errors.New("ffmpeg boom")
	}}
	svc := NewMediaService(f.store, newMockClock(), f.root, gen, &mockProber{})
	if err := svc.RegenerateThumbnail(context.Background(), id, f.admin); err == nil {
		t.Fatal("expected error")
	}
	f.assertThumb(id, ".thumbnails/video.jpg", "old")
}

// TestRegenerateThenRescan runs the two code paths that move thumbnails
// against one database, in the order a user hits them after an upgrade:
// holiday.mp4 and holiday.png share holiday.jpg, the user regenerates the
// video's thumbnail, then an admin rescans. The image must not inherit the
// shared file (it may show the video); the video keeps the regenerated one.
func TestRegenerateThenRescan(t *testing.T) {
	f := newThumbFixture(t)
	video := f.add("holiday.mp4", ".thumbnails/holiday.jpg")
	image := f.add("holiday.png", ".thumbnails/holiday.jpg")
	solo := f.add("a/solo.mp4", ".thumbnails/solo.jpg")

	f.regenerate(video)
	f.writeFile(f.abs(".thumbnails/holiday.mp4.jpg"), "picked frame") // stands for the user's choice

	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		return &model.Metadata{}, nil
	}}
	gen := &thumb.MockGenerator{GenerateFunc: writingThumbGen().GenerateFunc}
	sc := scanner.NewFSScanner(f.store, prober, gen, clock.RealClock{}, f.root)
	if err := sc.Scan(context.Background(), f.root, nil); err != nil {
		t.Fatal(err)
	}

	f.assertThumb(video, ".thumbnails/holiday.mp4.jpg", "picked frame")
	f.assertThumb(image, ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	f.assertThumb(solo, "a/.thumbnails/solo.mp4.jpg", "old") // renamed, not regenerated
	for _, stale := range []string{".thumbnails/holiday.jpg", ".thumbnails/solo.jpg"} {
		if _, err := os.Stat(f.abs(stale)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale thumbnail %q left behind (stat err = %v)", stale, err)
		}
	}

	// Regenerating after the migration overwrites in place: no new files.
	f.regenerate(image)
	f.assertThumb(image, ".thumbnails/holiday.png.jpg", f.fresh("holiday.png"))
	entries, err := os.ReadDir(f.abs(".thumbnails"))
	if err != nil || len(entries) != 2 {
		t.Errorf(".thumbnails holds %d entries (err %v), want the 2 current thumbnails", len(entries), err)
	}
}

// TestUnsubscribeFeed_RemovesArtworkThumbnail: the scanner puts the
// thumbnail of a feed's cover.jpg into the feed folder's own .thumbnails
// directory, which must not keep the folder alive after unsubscribing.
func TestUnsubscribeFeed_RemovesArtworkThumbnail(t *testing.T) {
	f := newUnsubscribeFixture(t)
	coverThumb := thumb.ThumbnailPathFor(f.otherCoverImg)
	if err := os.MkdirAll(filepath.Dir(coverThumb), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverThumb, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnsubscribeFeed(context.Background(), f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Dir(f.otherFile)) {
		t.Fatal("feed folder was left behind because of its artwork thumbnail")
	}
}

// TestUnsubscribeFeed_KeepsOtherThumbnails: only the artwork's thumbnail is
// feed-owned; any other file in .thumbnails keeps the folder in place.
func TestUnsubscribeFeed_KeepsOtherThumbnails(t *testing.T) {
	f := newUnsubscribeFixture(t)
	foreign := filepath.Join(thumb.ThumbnailDir(filepath.Dir(f.otherFile)), "mine.mp4.jpg")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnsubscribeFeed(context.Background(), f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if !fileExists(foreign) {
		t.Fatal("a thumbnail that is not feed artwork was deleted")
	}
}
