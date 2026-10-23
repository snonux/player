package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
)

type readonlyFixture struct {
	store               *repository.SQLite
	root, cache, setDir string
	user, set, media    int64
	svc                 *mediaService
}

func newReadonlyFixture(t *testing.T) readonlyFixture {
	t.Helper()
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, cache := t.TempDir(), t.TempDir()
	setDir := filepath.Join(root, "library")
	if err := os.MkdirAll(filepath.Join(setDir, "album"), 0o755); err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "hash", IsAdmin: true, CreatedAt: newMockClock().Now()})
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.CreateSet(ctx, &model.Set{Name: "library", RootPath: "library", CreatedAt: newMockClock().Now()})
	if err != nil {
		t.Fatal(err)
	}
	var first int64
	for _, name := range []string{"album/photo.jpg", "album/second.jpg"} {
		path := filepath.Join(setDir, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte("source"), 0o644); err != nil {
			t.Fatal(err)
		}
		id, err := store.CreateMedia(ctx, &model.Media{SetID: set, RelPath: name, AbsPath: path, FileName: filepath.Base(name), Type: model.MediaTypeImage, CreatedAt: newMockClock().Now()})
		if err != nil {
			t.Fatal(err)
		}
		if first == 0 {
			first = id
		}
	}
	svc := NewMediaService(store, newMockClock(), root, writingThumbGen(), &mockProber{}).WithThumbnailLayout(thumb.NewLayout(root, cache))
	return readonlyFixture{store, root, cache, setDir, user, set, first, svc}
}

func makeReadonly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	f, err := os.CreateTemp(dir, "write-check-*")
	if err == nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Skip("process can write chmod 0555 directory")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
}

func TestReadonlySetRefusesMutations(t *testing.T) {
	f := newReadonlyFixture(t)
	makeReadonly(t, f.setDir)
	makeReadonly(t, filepath.Join(f.setDir, "album"))
	ctx := context.Background()
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != ErrReadOnlySet {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.svc.UploadMedia(ctx, f.set, f.user, "new.jpg", strings.NewReader("data"), 4); err != ErrReadOnlySet {
		t.Fatalf("upload: %v", err)
	}
	row, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil || row == nil || row.DeletedAt != nil {
		t.Fatalf("row changed: %+v, %v", row, err)
	}
	if fileExists(filepath.Join(f.setDir, "new.jpg")) {
		t.Fatal("upload file left behind")
	}
}

func TestReadonlySetGeneratedCoversUseCache(t *testing.T) {
	f := newReadonlyFixture(t)
	makeReadonly(t, f.setDir)
	makeReadonly(t, filepath.Join(f.setDir, "album"))
	ctx := context.Background()
	for _, folder := range []string{"", "album"} {
		if err := f.svc.RegenerateSetCover(ctx, f.set, folder, f.user); err != nil {
			t.Fatal(err)
		}
		got, err := f.svc.GetSetCover(ctx, f.set, folder, f.user)
		want := filepath.Join(f.cache, "library", folder, ".cover.jpg")
		if err != nil || got == nil || got.Path != want {
			t.Fatalf("cover: %+v, %v; want %s", got, err, want)
		}
		if fileExists(filepath.Join(f.setDir, folder, ".cover.jpg")) {
			t.Fatal("cover written into source library")
		}
	}
	result, err := f.svc.BrowseSet(ctx, f.set, f.user, "")
	if err != nil || len(result.Folders) != 1 || !result.Folders[0].HasCover {
		t.Fatalf("folder cover absent: %+v, %v", result, err)
	}
	// Covers must not depend on a media row's thumbnail or indexed artwork.
	files, err := os.ReadDir(filepath.Join(f.cache, "library", "album"))
	if err != nil || len(files) != 1 || files[0].Name() != ".cover.jpg" {
		t.Fatalf("temporary files left behind: %v, %v", files, err)
	}
}

func TestReadonlySetLegacyTrashPurgesOnce(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	if err := f.store.SoftDeleteMedia(ctx, f.media); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.store.ListDeletedMedia(ctx)
	if err != nil || len(deleted) != 1 {
		t.Fatalf("deleted rows: %v %v", deleted, err)
	}
	row := deleted[0]
	makeReadonly(t, filepath.Join(f.setDir, "album"))
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	w := NewGCWorker(f.store, &clock.MockClock{T: row.DeletedAt.Add(8 * 24 * time.Hour)}, f.root, time.Minute, logger).WithThumbnailLayout(thumb.NewLayout(f.root, f.cache))
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil || got != nil {
		t.Fatalf("trash row remains: %+v %v", got, err)
	}
	if !fileExists(row.AbsPath) {
		t.Fatal("read-only source removed")
	}
	firstLogs := logs.String()
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	if logs.String() != firstLogs {
		t.Fatal("GC retried read-only source")
	}
}

func TestReadonlyErrorClassification(t *testing.T) {
	for _, err := range []error{syscall.EACCES, syscall.EPERM, syscall.EROFS} {
		if !readonlyError(&os.PathError{Op: "remove", Path: "file", Err: err}) {
			t.Fatalf("not classified: %v", err)
		}
	}
	for _, err := range []error{syscall.EIO, syscall.ENOSPC, syscall.ENOTEMPTY, os.ErrNotExist} {
		if readonlyError(err) {
			t.Fatalf("wrongly classified: %v", err)
		}
	}
}

func TestSetCoverRefusesPathEscapes(t *testing.T) {
	for _, kind := range []string{"traversal", "source symlink", "cache symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newReadonlyFixture(t)
			outside := t.TempDir()
			folder := "../sibling"
			switch kind {
			case "source symlink":
				folder = "escape"
				if err := os.Symlink(outside, filepath.Join(f.setDir, folder)); err != nil {
					t.Fatal(err)
				}
			case "cache symlink":
				folder = "album"
				if err := os.Symlink(outside, filepath.Join(f.cache, "library")); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if err := f.svc.RegenerateSetCover(ctx, f.set, folder, f.user); err != ErrForbidden {
				t.Fatalf("generate: %v", err)
			}
			if _, err := f.svc.GetSetCover(ctx, f.set, folder, f.user); err != ErrForbidden {
				t.Fatalf("read: %v", err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("outside files: %v %v", entries, err)
			}
		})
	}
}

func TestSetCoverFailureKeepsPreviousCover(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	if err := f.svc.RegenerateSetCover(ctx, f.set, "album", f.user); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(f.cache, "library", "album", ".cover.jpg")
	before, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	ws := f.svc.MediaWriteService.(*writeService)
	ws.thumbGen = &mockThumbGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
		if err := os.WriteFile(out, []byte("partial"), 0o644); err != nil {
			return err
		}
		return errors.New("generation failed")
	}}
	if err := f.svc.RegenerateSetCover(ctx, f.set, "album", f.user); err == nil {
		t.Fatal("generation unexpectedly succeeded")
	}
	after, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("previous cover changed: %q %v", after, err)
	}
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files left: %v %v", entries, err)
	}
	makeReadonly(t, filepath.Dir(dst))
	if err := f.svc.RegenerateSetCover(ctx, f.set, "album", f.user); err != ErrReadOnlySet {
		t.Fatalf("cache failure: %v", err)
	}
	if fileExists(filepath.Join(f.setDir, "album", ".cover.jpg")) {
		t.Fatal("fell back to media directory")
	}
}

func TestUploadAndDeleteRefuseSymlinkSet(t *testing.T) {
	f := newReadonlyFixture(t)
	outside := t.TempDir()
	path := filepath.Join(outside, "source.jpg")
	if err := os.WriteFile(path, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.setDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, f.setDir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := f.svc.UploadMedia(ctx, f.set, f.user, "new.jpg", strings.NewReader("data"), 4); err != ErrForbidden {
		t.Fatalf("upload: %v", err)
	}
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != ErrForbidden {
		t.Fatalf("delete: %v", err)
	}
	if fileExists(filepath.Join(outside, "new.jpg")) {
		t.Fatal("upload outside root")
	}
}

func TestGCRefusesOutsideSource(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	outside := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	row, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil {
		t.Fatal(err)
	}
	row.AbsPath = outside
	if err := f.store.UpdateMedia(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SoftDeleteMedia(ctx, f.media); err != nil {
		t.Fatal(err)
	}
	w := NewGCWorker(f.store, &clock.MockClock{T: time.Now().Add(8 * 24 * time.Hour)}, f.root, time.Minute, nil)
	if err := w.RunOnce(); err != nil {
		t.Fatal(err)
	}
	if !fileExists(outside) {
		t.Fatal("outside source deleted")
	}
	deleted, err := f.store.ListDeletedMedia(ctx)
	if err != nil || len(deleted) != 1 {
		t.Fatalf("row removed: %v %v", deleted, err)
	}
}

func TestSetCoverCreatesMissingCache(t *testing.T) {
	f := newReadonlyFixture(t)
	if err := os.RemoveAll(f.cache); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RegenerateSetCover(context.Background(), f.set, "album", f.user); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(f.cache, "library", "album", ".cover.jpg")) {
		t.Fatal("cover absent from recreated cache")
	}
}

func TestReadonlySetInaccessibleTrashPurgesOnce(t *testing.T) {
	for _, paths := range []string{"absolute", "relative root", "relative source"} {
		t.Run(paths, func(t *testing.T) {
			f := newReadonlyFixture(t)
			ctx := context.Background()
			row, err := f.store.GetMediaByID(ctx, f.media)
			if err != nil {
				t.Fatal(err)
			}
			originalSource := row.AbsPath
			root := f.root
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if paths == "relative root" {
				root, err = filepath.Rel(cwd, root)
				if err != nil {
					t.Fatal(err)
				}
			}
			if paths == "relative source" {
				row.AbsPath, err = filepath.Rel(cwd, row.AbsPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.store.UpdateMedia(ctx, row); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.SoftDeleteMedia(ctx, f.media); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(f.setDir, "album")
			if err := os.Chmod(dir, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			if _, err := os.Lstat(row.AbsPath); !errors.Is(err, os.ErrPermission) {
				t.Skip("process can access chmod 000 directory")
			}
			var logs bytes.Buffer
			w := NewGCWorker(f.store, &clock.MockClock{T: time.Now().Add(8 * 24 * time.Hour)}, root, time.Minute, slog.New(slog.NewTextHandler(&logs, nil)))
			if err := w.RunOnce(); err != nil {
				t.Fatal(err)
			}
			deleted, err := f.store.ListDeletedMedia(ctx)
			if err != nil || len(deleted) != 0 {
				t.Fatalf("trash row remains: %v %v", deleted, err)
			}
			firstLogs := logs.String()
			if err := w.RunOnce(); err != nil {
				t.Fatal(err)
			}
			if logs.String() != firstLogs {
				t.Fatal("GC retried inaccessible source")
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if !fileExists(originalSource) {
				t.Fatal("inaccessible source deleted")
			}
		})
	}
}
