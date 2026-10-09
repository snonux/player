package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

type updateRecorder struct {
	updates int
	err     error
}

func (u *updateRecorder) UpdateMedia(context.Context, *model.Media) error {
	u.updates++
	return u.err
}

// TestImportMediaFile_RowUpdateFailureRemovesThumbnail: when the row cannot
// be updated, upload and podcast download delete the file and the row. The
// thumbnail generated just before must go as well, with its directory, or
// it would be an orphan nothing ever removes.
func TestImportMediaFile_RowUpdateFailureRemovesThumbnail(t *testing.T) {
	dir := t.TempDir()
	m := &model.Media{AbsPath: filepath.Join(dir, "clip.mp4"), Type: model.MediaTypeVideo}
	store := &updateRecorder{err: errors.New("database is locked")}
	err := ImportMediaFile(context.Background(), store, m, &mockProber{}, newThumbnailMaker(writingThumbGen(), nil))
	if err == nil {
		t.Fatal("expected the update failure to be reported")
	}
	if fileExists(thumb.ThumbnailPathFor(m.AbsPath)) || fileExists(thumb.ThumbnailDir(dir)) {
		t.Error("thumbnail or its directory left behind after the failed import")
	}
}

// TestImportMediaFile_RowUpdateFailureKeepsSVG: an SVG is its own thumbnail;
// the cleanup after a failed update must not delete the media file.
func TestImportMediaFile_RowUpdateFailureKeepsSVG(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "logo.svg")
	if err := os.WriteFile(src, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &model.Media{AbsPath: src, Type: model.MediaTypeImage}
	store := &updateRecorder{err: errors.New("database is locked")}
	if err := ImportMediaFile(context.Background(), store, m, &mockProber{}, newThumbnailMaker(writingThumbGen(), nil)); err == nil {
		t.Fatal("expected the update failure to be reported")
	}
	if !fileExists(src) {
		t.Error("the SVG itself was deleted")
	}
}

// TestDownloadEpisode_UndoneDownloadRemovesThumbnail: a video episode gets a
// thumbnail when it is imported. If the download is then undone because the
// episode cannot be linked, the thumbnail goes with the file and the row.
func TestDownloadEpisode_UndoneDownloadRemovesThumbnail(t *testing.T) {
	f := newUnsubscribeFixture(t)
	f.svc.thumbs = newThumbnailMaker(writingThumbGen(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("video"))
	}))
	defer server.Close()
	episode, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.other, GUID: "v", Title: "v", EpisodeURL: server.URL + "/video.mp4", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.store = &cancelOnLink{SQLite: f.store, cancel: cancel}

	if _, err := f.svc.DownloadEpisode(ctx, episode, f.admin); err == nil {
		t.Fatal("expected the failed link to be reported")
	}
	folder := filepath.Dir(f.otherFile)
	if left, _ := filepath.Glob(filepath.Join(folder, "*video*")); len(left) != 0 {
		t.Fatalf("episode file left behind: %v", left)
	}
	if fileExists(thumb.ThumbnailDir(folder)) {
		entries, _ := os.ReadDir(thumb.ThumbnailDir(folder))
		t.Errorf("thumbnail of the undone download left behind: %v", entries)
	}
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
		if err := ImportMediaFile(ctx, store, m, &mockProber{}, newThumbnailMaker(writingThumbGen(), nil)); err != nil {
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

// TestImportMediaFile_ThumbnailNegativeCases: what is stored when no
// thumbnail is generated. A failing generator never fails the import (an
// upload or a podcast download must not be lost over a thumbnail); the item
// is stored like the scanner would index it.
func TestImportMediaFile_ThumbnailNegativeCases(t *testing.T) {
	ctx := context.Background()
	failing := &mockThumbGenerator{GenerateFunc: func(context.Context, string, string, float64) error {
		return errors.New("ffmpeg boom")
	}}
	empty := &mockThumbGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
		return os.WriteFile(out, nil, 0o644)
	}}
	tests := []struct {
		name      string
		file      string
		mediaType model.MediaType
		gen       *mockThumbGenerator
		wantThumb string // relative to the media directory; "" for none
	}{
		{"svg is its own thumbnail", "logo.svg", model.MediaTypeImage, writingThumbGen(), "logo.svg"},
		{"audio gets no thumbnail", "song.mp3", model.MediaTypeAudio, writingThumbGen(), ""},
		{"video, generator fails: no thumbnail", "clip.mp4", model.MediaTypeVideo, failing, ""},
		{"video, generator writes nothing: no thumbnail", "clip.mp4", model.MediaTypeVideo, empty, ""},
		{"image, generator fails: the image stands in", "pic.png", model.MediaTypeImage, failing, "pic.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			m := &model.Media{AbsPath: filepath.Join(dir, tt.file), Type: tt.mediaType}
			store := &updateRecorder{}
			if err := ImportMediaFile(ctx, store, m, &mockProber{}, newThumbnailMaker(tt.gen, nil)); err != nil {
				t.Fatalf("import failed: %v", err)
			}
			want := ""
			if tt.wantThumb != "" {
				want = filepath.Join(dir, tt.wantThumb)
			}
			if m.ThumbnailPath != want {
				t.Errorf("thumbnail = %q, want %q", m.ThumbnailPath, want)
			}
			if store.updates != 1 {
				t.Errorf("media row updated %d times, want 1", store.updates)
			}
			if left, _ := filepath.Glob(filepath.Join(dir, thumb.DirName, "*")); len(left) != 0 {
				t.Errorf("files left in the thumbnail directory: %v", left)
			}
		})
	}
}

// TestSetDirOf: the set directory is only trusted when the row's absolute
// and relative path fit together and the result lies below the media root.
func TestSetDirOf(t *testing.T) {
	const mediaRoot = "/media"
	tests := []struct {
		name    string
		absPath string
		relPath string
		want    string
		wantOK  bool
	}{
		{"file at the set root", "/media/set/clip.mp4", "clip.mp4", "/media/set", true},
		{"file in a subfolder", "/media/set/a/b/clip.mp4", "a/b/clip.mp4", "/media/set", true},
		{"paths that do not fit together", "/media/set/clip.mp4", "other.mp4", "", false},
		{"relative path only matches the end of a name", "/media/set/xclip.mp4", "clip.mp4", "", false},
		{"set outside the media root", "/elsewhere/set/clip.mp4", "clip.mp4", "", false},
		{"media root moved", "/old-media/set/clip.mp4", "clip.mp4", "", false},
		{"the media root itself is not a set", "/media/clip.mp4", "clip.mp4", "", false},
		{"empty relative path", "/media/set/clip.mp4", "", "", false},
		{"empty absolute path", "", "clip.mp4", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := setDirOf(mediaRoot, &model.Media{AbsPath: tt.absPath, RelPath: tt.relPath})
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("setDirOf = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestRegenerateThumbnail_SetOutsideMediaRoot: a row whose paths place its
// set outside the media root is stale; nothing is deleted on its word.
func TestRegenerateThumbnail_SetOutsideMediaRoot(t *testing.T) {
	f := newThumbFixture(t)
	id := f.add("video.mp4", ".thumbnails/video.jpg")
	// The same store and files, served from a different media root.
	svc := NewMediaService(f.store, newMockClock(), filepath.Join(f.root, "moved"), writingThumbGen(), &mockProber{})
	if err := svc.RegenerateThumbnail(context.Background(), id, f.admin); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if !fileExists(f.abs(".thumbnails/video.jpg")) {
		t.Error("previous thumbnail deleted although the row's set is outside the media root")
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
// shared file (it may show the video); the video, already at its current
// path, keeps the regenerated one.
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
	f.assertThumb(solo, "a/.thumbnails/solo.mp4.jpg", f.fresh("a/solo.mp4"))
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

// failingListStore is the real store, except that listing media fails.
type failingListStore struct{ *repository.SQLite }

func (failingListStore) ListMedia(context.Context, repository.MediaFilter) ([]model.Media, error) {
	return nil, errors.New("database is locked")
}

// TestRegenerateThumbnail_CleanupCannotListMedia: if the rows of the set
// cannot be listed, it is unknown whether another row still uses the
// replaced thumbnail, so it is kept; the regeneration itself succeeded.
func TestRegenerateThumbnail_CleanupCannotListMedia(t *testing.T) {
	f := newThumbFixture(t)
	id := f.add("video.mp4", ".thumbnails/video.jpg")
	svc := NewMediaService(failingListStore{f.store}, newMockClock(), f.root, writingThumbGen(), &mockProber{})
	if err := svc.RegenerateThumbnail(context.Background(), id, f.admin); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	f.assertThumb(id, ".thumbnails/video.mp4.jpg", f.fresh("video.mp4"))
	if _, err := os.Stat(f.abs(".thumbnails/video.jpg")); err != nil {
		t.Errorf("replaced thumbnail was removed without knowing who uses it: %v", err)
	}
}

// TestRegenerateThumbnail_NeverDeletesAMediaFile: a media file can live in a
// directory called .thumbnails, and another row can point at it as its
// thumbnail. No row stores that path as *its own* thumbnail here, so the
// only thing keeping the file is that it is a media file.
func TestRegenerateThumbnail_NeverDeletesAMediaFile(t *testing.T) {
	f := newThumbFixture(t)
	f.add(".thumbnails/pic.png", "")
	id := f.add("video.mp4", ".thumbnails/pic.png")
	f.writeFile(f.abs(".thumbnails/pic.png"), "source") // add() marked it "old"
	f.regenerate(id)
	f.assertThumb(id, ".thumbnails/video.mp4.jpg", f.fresh("video.mp4"))
	if got, err := os.ReadFile(f.abs(".thumbnails/pic.png")); err != nil || string(got) != "source" {
		t.Errorf("media file used as a thumbnail was deleted or changed: %q, %v", got, err)
	}
}

// TestRegenerateThumbnail_OnlyDeletesInsideTheSet: a stale stored path can
// point into any .thumbnails directory: of another set, of the media root,
// or above it. All of those are ancestors' or strangers' directories and
// must be left alone, exactly like the scanner leaves them alone.
func TestRegenerateThumbnail_OnlyDeletesInsideTheSet(t *testing.T) {
	f := newThumbFixture(t)
	for name, previous := range map[string]string{
		"another set":          filepath.Join(f.root, "other", thumb.DirName, "video.jpg"),
		"the media root":       filepath.Join(f.root, thumb.DirName, "video.jpg"),
		"above the media root": filepath.Join(filepath.Dir(f.root), thumb.DirName, "video.jpg"),
	} {
		t.Run(name, func(t *testing.T) {
			id := f.add("a/"+strings.ReplaceAll(name, " ", "-")+".mp4", previous)
			f.regenerate(id)
			if !fileExists(previous) {
				t.Errorf("thumbnail in %s was deleted", name)
			}
		})
	}
}

// TestRegenerateThumbnail_UnverifiedOutputChangesNothing: a generator that
// reports success without producing the thumbnail (ffmpeg does, for some
// output names) must not get the row switched or the old thumbnail deleted.
func TestRegenerateThumbnail_UnverifiedOutputChangesNothing(t *testing.T) {
	for name, gen := range map[string]func(out string) error{
		"no output":        func(out string) error { return os.Remove(out) },
		"empty output":     func(out string) error { return os.WriteFile(out, nil, 0o644) },
		"output elsewhere": func(out string) error { return os.WriteFile(out+".001.jpg", []byte("jpg"), 0o644) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newThumbFixture(t)
			id := f.add("video.mp4", ".thumbnails/video.jpg")
			silent := &mockThumbGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error { return gen(out) }}
			svc := NewMediaService(f.store, newMockClock(), f.root, silent, &mockProber{})
			if err := svc.RegenerateThumbnail(context.Background(), id, f.admin); err == nil {
				t.Fatal("expected an error for a thumbnail that was not produced")
			}
			f.assertThumb(id, ".thumbnails/video.jpg", "old")
			if fileExists(f.abs(".thumbnails/video.mp4.jpg")) {
				t.Error("an unverified thumbnail was put in place")
			}
		})
	}
}

// TestRegenerateThumbnail_WithoutGenerator: a service built without a
// generator reports that instead of crashing.
func TestRegenerateThumbnail_WithoutGenerator(t *testing.T) {
	f := newThumbFixture(t)
	id := f.add("video.mp4", ".thumbnails/video.jpg")
	svc := NewMediaService(f.store, newMockClock(), f.root, nil, &mockProber{})
	if err := svc.RegenerateThumbnail(context.Background(), id, f.admin); err == nil {
		t.Fatal("expected an error")
	}
	f.assertThumb(id, ".thumbnails/video.jpg", "old")
}

// writeThumbFile creates a file (and its directory) for the removal tests.
func writeThumbFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRemoveOwnThumbnail: purging a media item removes the thumbnail that is
// its own, and nothing that might belong to something else.
func TestRemoveOwnThumbnail(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		stored   string // relative to the temp dir
		wantGone bool
	}{
		{"own generated thumbnail", "a/clip.mp4", "a/.thumbnails/clip.mp4.jpg", true},
		{"old stem name, possibly shared", "a/clip.mp4", "a/.thumbnails/clip.jpg", false},
		{"another item's thumbnail", "a/clip.mp4", "a/.thumbnails/other.mp4.jpg", false},
		{"audio cover image", "a/song.mp3", "a/cover.jpg", false},
		{"image serving as its own thumbnail", "a/pic.png", "a/pic.png", false},
	}
	rm := newThumbnailRemover(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			stored := filepath.Join(dir, filepath.FromSlash(tt.stored))
			writeThumbFile(t, stored)
			removeOwnThumbnail(rm, &model.Media{AbsPath: filepath.Join(dir, filepath.FromSlash(tt.source)), ThumbnailPath: stored})
			if gone := !fileExists(stored); gone != tt.wantGone {
				t.Errorf("thumbnail gone = %v, want %v", gone, tt.wantGone)
			}
			if tt.wantGone && fileExists(filepath.Dir(stored)) {
				t.Error("emptied .thumbnails directory was left behind")
			}
		})
	}
}

// TestRemoveOwnThumbnail_EdgeCases: a row without paths is ignored, and a
// .thumbnails directory still holding other thumbnails stays.
func TestRemoveOwnThumbnail_EdgeCases(t *testing.T) {
	rm := newThumbnailRemover(nil)
	removeOwnThumbnail(rm, &model.Media{}) // must not panic or delete anything

	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	own, other := thumb.ThumbnailPathFor(src), filepath.Join(dir, thumb.DirName, "other.mp4.jpg")
	writeThumbFile(t, own)
	writeThumbFile(t, other)
	removeOwnThumbnail(rm, &model.Media{AbsPath: src, ThumbnailPath: own})
	if fileExists(own) || !fileExists(other) {
		t.Errorf("own gone = %v, other kept = %v; want both true", !fileExists(own), fileExists(other))
	}
}

// TestGCWorker_RemovesPurgedMediasThumbnail: garbage collection used to
// delete the file and the row but leave the generated thumbnail behind for
// good. A thumbnail under an old, possibly shared name is still kept.
func TestGCWorker_RemovesPurgedMediasThumbnail(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	root := t.TempDir()
	write := func(path string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	video := write(filepath.Join(root, "set", "a", "clip.mp4"))
	videoThumb := write(thumb.ThumbnailPathFor(video))
	legacy := write(filepath.Join(root, "set", "holiday.mp4"))
	legacyThumb := write(filepath.Join(root, "set", thumb.DirName, "holiday.jpg"))
	deletedAt := now.Add(-8 * 24 * time.Hour)
	store := &repository.MockStore{MediaRepo: repository.MockMediaRepo{
		ListDeletedMediaFunc: func(context.Context) ([]model.Media, error) {
			return []model.Media{
				{ID: 1, RelPath: "a/clip.mp4", AbsPath: video, ThumbnailPath: videoThumb, DeletedAt: &deletedAt},
				{ID: 2, RelPath: "holiday.mp4", AbsPath: legacy, ThumbnailPath: legacyThumb, DeletedAt: &deletedAt},
			}, nil
		},
	}}
	w := NewGCWorker(store, &clock.MockClock{T: now}, root, time.Minute, nil).WithAge(7 * 24 * time.Hour)
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	if fileExists(video) || fileExists(legacy) {
		t.Fatal("purged media files remain")
	}
	if fileExists(videoThumb) || fileExists(filepath.Dir(videoThumb)) {
		t.Error("thumbnail of the purged video, or its emptied directory, was left behind")
	}
	if !fileExists(legacyThumb) {
		t.Error("a thumbnail under an old, possibly shared name was deleted")
	}
}

// TestGCWorker_KeepsThumbnailWhenPurgeFails: if the row cannot be deleted
// the item still exists, and so must its thumbnail.
func TestGCWorker_KeepsThumbnailWhenPurgeFails(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	root := t.TempDir()
	video := filepath.Join(root, "set", "clip.mp4")
	videoThumb := thumb.ThumbnailPathFor(video)
	if err := os.MkdirAll(filepath.Dir(videoThumb), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoThumb, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deletedAt := now.Add(-8 * 24 * time.Hour)
	store := &repository.MockStore{MediaRepo: repository.MockMediaRepo{
		ListDeletedMediaFunc: func(context.Context) ([]model.Media, error) {
			return []model.Media{{ID: 1, RelPath: "clip.mp4", AbsPath: video, ThumbnailPath: videoThumb, DeletedAt: &deletedAt}}, nil
		},
		HardDeleteMediaFunc: func(context.Context, int64) error { return errors.New("database is locked") },
	}}
	w := NewGCWorker(store, &clock.MockClock{T: now}, root, time.Minute, nil).WithAge(7 * 24 * time.Hour)
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	if !fileExists(videoThumb) {
		t.Error("thumbnail deleted although the row could not be purged")
	}
}

// TestUnsubscribeFeed_RemovesThumbnailsOfDeletedMedia: a video episode and
// indexed feed artwork have generated thumbnails in the feed folder's
// .thumbnails directory. They go with their media, or the directory would
// keep the folder in place after unsubscribing.
func TestUnsubscribeFeed_RemovesThumbnailsOfDeletedMedia(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	writeThumb := func(src string) string {
		t.Helper()
		p := thumb.ThumbnailPathFor(src)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("jpg"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The episode, as a video podcast's would be after its download.
	if err := f.store.UpdateMediaThumbnail(ctx, f.otherMedia, writeThumb(f.otherFile)); err != nil {
		t.Fatal(err)
	}
	// The cover, as a rescan indexes it.
	if _, err := f.store.CreateMedia(ctx, &model.Media{
		SetID: feed.SetID, RelPath: "Other/cover.jpg", FileName: "cover.jpg", AbsPath: f.otherCoverImg,
		Type: model.MediaTypeImage, ThumbnailPath: writeThumb(f.otherCoverImg), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnsubscribeFeed(ctx, f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Dir(f.otherFile)) {
		t.Fatal("feed folder was left behind because of its media's thumbnails")
	}
}

// TestUnsubscribeFeed_KeepsOtherThumbnails: a thumbnail that belongs to none
// of the deleted media is not feed-owned and keeps the folder in place.
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
		t.Fatal("a thumbnail of something else was deleted")
	}
}
