package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
