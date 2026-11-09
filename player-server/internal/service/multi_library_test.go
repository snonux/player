package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
)

// twoLibraryFixture has a default library A and a second library B, each
// with a set called "Rock" (equal RootPath). Bob may only use B/Rock.
type twoLibraryFixture struct {
	store                  *repository.SQLite
	rootA, rootB, cache    string
	libA, libB             int64
	setA, setB, admin, bob int64
}

func newTwoLibraryFixture(t *testing.T) twoLibraryFixture {
	t.Helper()
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f := twoLibraryFixture{store: store, rootA: t.TempDir(), rootB: t.TempDir(), cache: t.TempDir()}
	now := newMockClock().Now()
	libA, err := store.EnsureDefaultMusicLibrary(ctx, f.rootA, now)
	if err != nil {
		t.Fatal(err)
	}
	f.libA = libA.ID
	if f.libB, err = store.CreateMusicLibrary(ctx, &model.MusicLibrary{Name: "B", RootPath: f.rootB, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{f.rootA, f.rootB} {
		if err := os.MkdirAll(filepath.Join(root, "Rock"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if f.setA, err = store.CreateSet(ctx, &model.Set{LibraryID: f.libA, Name: "Rock", RootPath: "Rock", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if f.setB, err = store.CreateSet(ctx, &model.Set{LibraryID: f.libB, Name: "Rock", RootPath: "Rock", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if f.admin, err = store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "h", IsAdmin: true, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if f.bob, err = store.CreateUser(ctx, &model.User{Username: "bob", PasswordHash: "h", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(ctx, &model.SetPermission{SetID: f.setB, UserID: f.bob, Role: model.RoleOwner, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f twoLibraryFixture) addMedia(t *testing.T, root string, set int64, rel string, typ model.MediaType) (int64, string) {
	t.Helper()
	path := filepath.Join(root, "Rock", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := f.store.CreateMedia(context.Background(), &model.Media{SetID: set, RelPath: rel, AbsPath: path,
		FileName: filepath.Base(path), Type: typ, CreatedAt: newMockClock().Now()})
	if err != nil {
		t.Fatal(err)
	}
	return id, path
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// B2: a set cover is read from the set's own library, never from the
// same-named set of the default library.
func TestSetCover_UsesOwnLibrary(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(map[bool]string{false: "beside", true: "cache"}[cache], func(t *testing.T) {
			f := newTwoLibraryFixture(t)
			ctx := context.Background()
			cacheDir := ""
			if cache {
				cacheDir = f.cache
			}
			svc := NewMediaService(f.store, newMockClock(), f.rootA, writingThumbGen(), &mockProber{}).
				WithThumbnailLayout(thumb.NewLayout(f.rootA, cacheDir))
			writeFile(t, filepath.Join(f.rootA, "Rock", "cover.jpg"), "A cover")

			if got, err := svc.GetSetCover(ctx, f.setB, "", f.bob); !errors.Is(err, ErrNotFound) {
				t.Fatalf("B cover without artwork = %+v, %v; want ErrNotFound", got, err)
			}
			res, err := svc.BrowseSet(ctx, f.setB, f.bob, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Folders) != 0 {
				t.Fatalf("unexpected folders %+v", res.Folders)
			}

			// B's own artwork is found.
			_, own := f.addMedia(t, f.rootB, f.setB, "cover.jpg", model.MediaTypeImage)
			got, err := svc.GetSetCover(ctx, f.setB, "", f.bob)
			if err != nil || got.Path != own {
				t.Fatalf("B cover = %+v, %v; want %s", got, err, own)
			}

			// Regenerating B's cover writes inside B and never touches A.
			_, _ = f.addMedia(t, f.rootB, f.setB, "Album/clip.mp4", model.MediaTypeVideo)
			if err := svc.RegenerateSetCover(ctx, f.setB, "Album", f.bob); err != nil {
				t.Fatalf("regenerate B cover: %v", err)
			}
			got, err = svc.GetSetCover(ctx, f.setB, "Album", f.bob)
			if err != nil || !pathWithin(f.rootB, got.Path) {
				t.Fatalf("regenerated B cover = %+v, %v; want below %s", got, err, f.rootB)
			}
			for _, dir := range []string{filepath.Join(f.rootA, "Rock", "Album"), filepath.Join(f.cache, "Rock")} {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("regenerating B's cover touched %s: %v", dir, err)
				}
			}
		})
	}
}

// B8: expired trash in a non-default library is purged, and a legacy row
// without AbsPath resolves inside its own library, never the default one.
func TestGC_PurgesTrashInEveryLibrary(t *testing.T) {
	f := newTwoLibraryFixture(t)
	ctx := context.Background()
	idB, pathB := f.addMedia(t, f.rootB, f.setB, "song.mp3", model.MediaTypeAudio)
	_, pathA := f.addMedia(t, f.rootA, f.setA, "legacy.mp3", model.MediaTypeAudio)
	legacyPathB := filepath.Join(f.rootB, "Rock", "legacy.mp3")
	writeFile(t, legacyPathB, "B legacy")
	legacyB, err := f.store.CreateMedia(ctx, &model.Media{SetID: f.setB, RelPath: "legacy.mp3", FileName: "legacy.mp3",
		Type: model.MediaTypeAudio, CreatedAt: newMockClock().Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{idB, legacyB} {
		if err := f.store.SoftDeleteMedia(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	later := &clock.MockClock{T: time.Now().Add(8 * 24 * time.Hour)}
	w := NewGCWorker(f.store, later, f.rootA, time.Minute, nil).WithAge(7 * 24 * time.Hour)
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{pathB, legacyPathB} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expired trash %s still on disk: %v", p, err)
		}
	}
	for _, id := range []int64{idB, legacyB} {
		if m, _ := f.store.GetMediaByID(ctx, id); m != nil {
			t.Fatalf("expired row %d not purged", id)
		}
	}
	if _, err := os.Stat(pathA); err != nil {
		t.Fatalf("default library file with the same relative path touched: %v", err)
	}
}

func musicMedia(f twoLibraryFixture, t *testing.T, root string, set int64, rel string) int64 {
	t.Helper()
	path := filepath.Join(root, "Rock", filepath.FromSlash(rel))
	writeFile(t, path, "audio")
	id, err := f.store.CreateMedia(context.Background(), &model.Media{SetID: set, RelPath: rel, AbsPath: path, FileName: filepath.Base(rel),
		Type: model.MediaTypeAudio, Music: &model.MusicMetadata{Title: rel, Album: "Album", Artist: "Artist"}, CreatedAt: newMockClock().Now()})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// B9: M3U import reads only from the caller's set in its own library and
// matches entries against songs of that library.
func TestM3UImport_StaysInOwnLibrary(t *testing.T) {
	f := newTwoLibraryFixture(t)
	ctx := context.Background()
	songA := musicMedia(f, t, f.rootA, f.setA, "song.mp3")
	songB := musicMedia(f, t, f.rootB, f.setB, "song.mp3")
	if err := f.store.GrantPermission(ctx, &model.SetPermission{SetID: f.setA, UserID: f.admin, Role: model.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	svc := NewPlaylistService(f.store, NewMediaService(f.store, newMockClock(), f.rootA, nil, nil)).WithImportRoot(f.rootA)
	writeFile(t, filepath.Join(f.rootA, "Rock", "a.m3u"), "song.mp3\n")
	req := PlaylistImportRequest{Path: "Rock/a.m3u", Base: "folder", Preview: true}
	if got, err := svc.ImportM3U(ctx, f.bob, req); err == nil {
		t.Fatalf("bob read library A's playlist file: %+v", got)
	}

	writeFile(t, filepath.Join(f.rootB, "Rock", "b.m3u"), "song.mp3\n")
	req.Path = "Rock/b.m3u"
	for _, user := range []int64{f.bob, f.admin} {
		preview, err := svc.ImportM3U(ctx, user, req)
		if err != nil || preview.Available != 1 {
			t.Fatalf("user %d preview: %+v, %v", user, preview, err)
		}
		req.Preview, req.Fingerprint = false, preview.Fingerprint
		imported, err := svc.ImportM3U(ctx, user, req)
		req.Preview, req.Fingerprint = true, ""
		if err != nil || len(imported.Playlist.Entries) != 1 {
			t.Fatalf("user %d import: %+v, %v", user, imported, err)
		}
		if got := imported.Playlist.Entries[0].MediaID; got != songB {
			t.Fatalf("user %d imported media %d, want B's %d (A's is %d)", user, got, songB, songA)
		}
	}
}

// B9: sidecar lyrics of a song in a non-default library are found.
func TestLyrics_NonDefaultLibrary(t *testing.T) {
	f := newTwoLibraryFixture(t)
	ctx := context.Background()
	id := musicMedia(f, t, f.rootB, f.setB, "song.mp3")
	writeFile(t, filepath.Join(f.rootB, "Rock", "song.lrc"), "[00:01.00]hello from B\n")
	svc := NewMediaService(f.store, newMockClock(), f.rootA, nil, nil)
	lyrics, err := svc.GetLyrics(ctx, id, f.bob)
	if err != nil || len(lyrics.Lines) != 1 || lyrics.Lines[0].Text != "hello from B" {
		t.Fatalf("lyrics = %+v, %v", lyrics, err)
	}
}

// B9: regenerating a thumbnail in a non-default library removes the one it
// replaced.
func TestReplacedThumbnail_RemovedInNonDefaultLibrary(t *testing.T) {
	f := newTwoLibraryFixture(t)
	ctx := context.Background()
	id, src := f.addMedia(t, f.rootB, f.setB, "clip.mp4", model.MediaTypeVideo)
	old := filepath.Join(f.rootB, "Rock", thumb.DirName, "old-clip.jpg")
	writeFile(t, old, "old")
	m, err := f.store.GetMediaByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	m.ThumbnailPath = old
	if err := f.store.UpdateMedia(ctx, m); err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(f.store, newMockClock(), f.rootA, writingThumbGen(), &mockProber{}).
		WithThumbnailLayout(thumb.NewLayout(f.rootA, ""))
	if err := svc.RegenerateThumbnail(ctx, id, f.bob); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("replaced thumbnail of %s kept: %v", src, err)
	}
}
