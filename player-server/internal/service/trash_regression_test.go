package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/scanner"
	"codeberg.org/snonux/player/internal/thumb"
)

func TestTrashStartupRetentionBoundaryRealStore(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name            string
		age             time.Duration
		deleted, purged bool
	}{
		{"active", 8 * 24 * time.Hour, false, false},
		{"fresh", 0, true, false},
		{"future", -time.Hour, true, false},
		{"one nanosecond before expiry", 7*24*time.Hour - time.Nanosecond, true, false},
		{"exact expiry", 7 * 24 * time.Hour, true, true},
		{"one nanosecond past expiry", 7*24*time.Hour + time.Nanosecond, true, true},
		{"old", 30 * 24 * time.Hour, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReadonlyFixture(t)
			ctx := context.Background()
			item, err := f.store.GetMediaByID(ctx, f.media)
			if err != nil {
				t.Fatal(err)
			}
			if tc.deleted {
				deletedAt := now.Add(-tc.age).In(time.FixedZone("EEST", 3*3600))
				item.DeletedAt = &deletedAt
			}
			layout := thumb.NewLayout(f.root, f.cache)
			thumbnail, ok := layout.PathFor(item.AbsPath)
			if !ok {
				t.Fatal("thumbnail path refused")
			}
			if err := os.MkdirAll(filepath.Dir(thumbnail), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(thumbnail, []byte("thumbnail"), 0644); err != nil {
				t.Fatal(err)
			}
			item.ThumbnailPath = thumbnail
			if err := f.store.UpdateMedia(ctx, item); err != nil {
				t.Fatal(err)
			}
			cache := &fakeRenditionGC{}
			worker := NewGCWorker(f.store, &clock.MockClock{T: now}, f.root, 24*time.Hour, nil).WithThumbnailLayout(layout).WithRenditionCache(cache)
			worker.Start() // Must complete expiry without waiting for the first tick.
			worker.Stop()
			rows, err := f.store.ListMedia(ctx, repository.MediaFilter{IncludeDeleted: true})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.ID == f.media {
					found = true
				}
			}
			if found == tc.purged {
				t.Fatalf("row present=%v purged=%v", found, tc.purged)
			}
			for _, path := range []string{item.AbsPath, thumbnail} {
				_, err := os.Stat(path)
				if tc.purged && !os.IsNotExist(err) {
					t.Fatalf("expired path remains: %s (%v)", path, err)
				}
				if !tc.purged && err != nil {
					t.Fatalf("recoverable path removed: %s (%v)", path, err)
				}
			}
			if tc.purged && (len(cache.removed) != 1 || cache.removed[0] != item.ID) {
				t.Fatalf("rendition removals=%v", cache.removed)
			}
			if !tc.purged && len(cache.removed) != 0 {
				t.Fatalf("recoverable renditions removed=%v", cache.removed)
			}
			if cache.pruned != 1 {
				t.Fatalf("startup cache prune count=%d", cache.pruned)
			}
		})
	}
}

func TestTrashRealDeleteRestoreRescanPreservesTimestamp(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	before, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != nil {
		t.Fatal(err)
	}
	trash, err := f.store.ListDeletedMedia(ctx)
	if err != nil || len(trash) != 1 || trash[0].DeletedAt == nil {
		t.Fatalf("trash=%v err=%v", trash, err)
	}
	first := *trash[0].DeletedAt
	if _, err := os.Stat(before.AbsPath); err != nil {
		t.Fatalf("source changed on delete: %v", err)
	}
	// Repository retries must not extend the recovery window.
	old := first.Add(-time.Hour)
	trash[0].DeletedAt = &old
	if err := f.store.UpdateMedia(ctx, &trash[0]); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SoftDeleteMedia(ctx, f.media); err != nil {
		t.Fatal(err)
	}
	scan := scanner.NewFSScanner(f.store, &mockProber{}, writingThumbGen(), clock.RealClock{}, f.root)
	if err := scan.Scan(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	trash, err = f.store.ListDeletedMedia(ctx)
	if err != nil || len(trash) != 1 || !trash[0].DeletedAt.Equal(old) {
		t.Fatalf("rescan/retry altered trash=%v err=%v", trash, err)
	}
	if err := f.svc.RestoreMedia(ctx, f.media, f.user); err != nil {
		t.Fatal(err)
	}
	restored, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil || restored == nil || restored.DeletedAt != nil || restored.ID != before.ID || restored.AbsPath != before.AbsPath {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != nil {
		t.Fatal(err)
	}
	trash, err = f.store.ListDeletedMedia(ctx)
	if err != nil || len(trash) != 1 || !trash[0].DeletedAt.After(old) {
		t.Fatalf("new deletion timestamp=%v err=%v", trash, err)
	}
}

// Barrier hooks pause the real store at the operation whose lock must own the
// decision; no sleeps or scheduling assumptions are used to order operations.
type trashBarrierStore struct {
	repository.Store
	listOnce                       sync.Once
	listEntered, listRelease       chan struct{}
	restoreEntered, restoreRelease chan struct{}
}

func (s *trashBarrierStore) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	if s.listEntered != nil {
		s.listOnce.Do(func() { close(s.listEntered); <-s.listRelease })
	}
	return s.Store.ListDeletedMedia(ctx)
}
func (s *trashBarrierStore) RestoreMedia(ctx context.Context, id int64) error {
	if s.restoreEntered != nil {
		close(s.restoreEntered)
		<-s.restoreRelease
	}
	return s.Store.RestoreMedia(ctx, id)
}

func TestTrashRestoreAndExpiryAreSerialized(t *testing.T) {
	for _, purgeFirst := range []bool{false, true} {
		name := "restore first"
		if purgeFirst {
			name = "purge first"
		}
		t.Run(name, func(t *testing.T) {
			f := newReadonlyFixture(t)
			ctx := context.Background()
			item, err := f.store.GetMediaByID(ctx, f.media)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			old := now.Add(-8 * 24 * time.Hour)
			item.DeletedAt = &old
			if err := f.store.UpdateMedia(ctx, item); err != nil {
				t.Fatal(err)
			}
			store := &trashBarrierStore{Store: f.store}
			lock := new(sync.Mutex)
			svc := NewMediaService(store, clock.RealClock{}, f.root, nil, nil).WithTrashLock(lock)
			worker := NewGCWorker(store, &clock.MockClock{T: now}, f.root, time.Hour, nil).WithTrashLock(lock)
			restoreDone := make(chan error, 1)
			gcDone := make(chan error, 1)
			if purgeFirst {
				store.listEntered = make(chan struct{})
				store.listRelease = make(chan struct{})
				go func() { gcDone <- worker.RunOnce() }()
				<-store.listEntered
				if lock.TryLock() {
					lock.Unlock()
					t.Fatal("GC did not hold the injected trash lock")
				}
				go func() { restoreDone <- svc.RestoreMedia(ctx, f.media, f.user) }()
				close(store.listRelease)
			} else {
				store.restoreEntered = make(chan struct{})
				store.restoreRelease = make(chan struct{})
				go func() { restoreDone <- svc.RestoreMedia(ctx, f.media, f.user) }()
				<-store.restoreEntered
				if lock.TryLock() {
					lock.Unlock()
					t.Fatal("restore did not hold the injected trash lock")
				}
				go func() { gcDone <- worker.RunOnce() }()
				close(store.restoreRelease)
			}
			if err := <-gcDone; err != nil {
				t.Fatal(err)
			}
			err = <-restoreDone
			if purgeFirst && !errors.Is(err, ErrNotFound) {
				t.Fatalf("purge-first restore=%v", err)
			}
			if !purgeFirst && err != nil {
				t.Fatalf("restore-first=%v", err)
			}
			row, err := f.store.GetMediaByID(ctx, f.media)
			if err != nil {
				t.Fatal(err)
			}
			if purgeFirst && row != nil {
				t.Fatal("purged row survived")
			}
			if !purgeFirst && row == nil {
				t.Fatal("restored row purged")
			}
			_, err = os.Stat(item.AbsPath)
			if purgeFirst && !os.IsNotExist(err) {
				t.Fatalf("purged file survived: %v", err)
			}
			if !purgeFirst && err != nil {
				t.Fatalf("restored file removed: %v", err)
			}
		})
	}
}

func TestUserTrashRealPermissions(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != nil {
		t.Fatal(err)
	}
	svc := NewUserTrashService(f.store)
	for _, role := range []model.Role{"", model.RoleViewer, model.RoleOwner} {
		t.Run(string(role), func(t *testing.T) {
			id, err := f.store.CreateUser(ctx, &model.User{Username: "user-" + string(role), PasswordHash: "hash", CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			if role != "" {
				if err := f.store.GrantPermission(ctx, &model.SetPermission{SetID: f.set, UserID: id, Role: role, CreatedAt: now}); err != nil {
					t.Fatal(err)
				}
			}
			trash, err := svc.ListTrash(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if role == model.RoleOwner {
				expected = 1
			}
			if len(trash) != expected {
				t.Fatalf("role %q trash=%v", role, trash)
			}
			if role != model.RoleOwner {
				if err := f.svc.RestoreMedia(ctx, f.media, id); !errors.Is(err, ErrForbidden) {
					t.Fatalf("unauthorized restore=%v", err)
				}
			}
		})
	}
	if trash, err := svc.ListTrash(ctx, f.user); err != nil || len(trash) != 1 {
		t.Fatalf("admin trash=%v err=%v", trash, err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListTrash(ctx, f.user); err == nil {
		t.Fatal("DB failure hidden as empty trash")
	}
}

func TestTrashStartupStopWaitsAndCancels(t *testing.T) {
	entered, release, startDone, stopDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctxCancelled := make(chan struct{})
	root := t.TempDir()
	file := filepath.Join(root, "must-stay.jpg")
	if err := os.WriteFile(file, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	store := &repository.MockStore{MediaRepo: repository.MockMediaRepo{ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
		close(entered)
		<-ctx.Done()
		close(ctxCancelled)
		<-release
		return []model.Media{{ID: 1, AbsPath: file, DeletedAt: &old}}, nil
	}}}
	worker := NewGCWorker(store, clock.RealClock{}, root, time.Hour, nil)
	go func() { worker.Start(); close(startDone) }()
	<-entered
	go func() { worker.Stop(); close(stopDone) }()
	<-ctxCancelled // Stop has cancelled the active startup pass.
	select {
	case <-stopDone:
		t.Fatal("Stop returned before startup completed")
	default:
	}
	close(release)
	<-startDone
	<-stopDone
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("cancelled startup removed source: %v", err)
	}
}

func TestTrashStopBeforeStartNeverRunsStartup(t *testing.T) {
	calls := 0
	store := &repository.MockStore{MediaRepo: repository.MockMediaRepo{ListDeletedMediaFunc: func(context.Context) ([]model.Media, error) { calls++; return nil, nil }}}
	worker := NewGCWorker(store, clock.RealClock{}, t.TempDir(), time.Hour, nil)
	worker.Stop()
	worker.Start()
	worker.Stop()
	if calls != 0 {
		t.Fatalf("stopped worker ran startup %d times", calls)
	}
}

func TestTrashRestoreRefusesMissingUnsafeAndUnreadableSources(t *testing.T) {
	for _, name := range []string{"missing", "directory", "outside root", "escaping symlink", "unreadable file", "inaccessible directory"} {
		t.Run(name, func(t *testing.T) {
			f := newReadonlyFixture(t)
			ctx := context.Background()
			original, err := f.store.GetMediaByID(ctx, f.media)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != nil {
				t.Fatal(err)
			}
			rows, err := f.store.ListDeletedMedia(ctx)
			if err != nil || len(rows) != 1 {
				t.Fatalf("trash=%v err=%v", rows, err)
			}
			row := rows[0]
			deletedAt := *row.DeletedAt
			expected := ErrForbidden
			switch name {
			case "missing":
				if err := os.Remove(row.AbsPath); err != nil {
					t.Fatal(err)
				}
				expected = ErrNotFound
			case "directory":
				if err := os.Remove(row.AbsPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(row.AbsPath, 0755); err != nil {
					t.Fatal(err)
				}
				expected = ErrNotFound
			case "outside root", "escaping symlink":
				outside := filepath.Join(t.TempDir(), "outside.jpg")
				if err := os.WriteFile(outside, []byte("outside-source"), 0644); err != nil {
					t.Fatal(err)
				}
				if name == "outside root" {
					row.AbsPath = outside
					if err := f.store.UpdateMedia(ctx, &row); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(row.AbsPath); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, row.AbsPath); err != nil {
						t.Fatal(err)
					}
				}
				t.Cleanup(func() {
					data, err := os.ReadFile(outside)
					if err != nil || string(data) != "outside-source" {
						t.Errorf("outside file changed: %v", err)
					}
				})
			case "unreadable file":
				if err := os.Chmod(row.AbsPath, 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(row.AbsPath, 0644) })
				if file, err := os.Open(row.AbsPath); err == nil {
					file.Close()
					t.Skip("process can read chmod000 file")
				}
			case "inaccessible directory":
				dir := filepath.Dir(row.AbsPath)
				if err := os.Chmod(dir, 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
				if _, err := os.Stat(row.AbsPath); err == nil {
					t.Skip("process can traverse chmod000 directory")
				}
			}
			if err := f.svc.RestoreMedia(ctx, f.media, f.user); !errors.Is(err, expected) {
				t.Fatalf("restore=%v expected=%v", err, expected)
			}
			active, err := f.store.GetMediaByID(ctx, f.media)
			if err != nil || active != nil {
				t.Fatalf("failed restore activated row=%v err=%v", active, err)
			}
			rows, err = f.store.ListDeletedMedia(ctx)
			if err != nil || len(rows) != 1 || !rows[0].DeletedAt.Equal(deletedAt) {
				t.Fatalf("failed restore changed trash=%v err=%v", rows, err)
			}
			if rows[0].ID != original.ID {
				t.Fatal("failed restore changed identity")
			}
		})
	}
}

func TestTrashRestoreExistingSourceInReadOnlySet(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != nil {
		t.Fatal(err)
	}
	makeReadonly(t, filepath.Join(f.setDir, "album"))
	makeReadonly(t, f.setDir)
	if err := f.svc.RestoreMedia(ctx, f.media, f.user); err != nil {
		t.Fatalf("read-only source restore=%v", err)
	}
	row, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil || row == nil {
		t.Fatalf("restore row=%v err=%v", row, err)
	}
	if _, err := os.Stat(row.AbsPath); err != nil {
		t.Fatalf("source changed=%v", err)
	}
}

func TestTrashRestoreEmptyAbsPathUsesSetRootNotConflictingSibling(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	item, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil {
		t.Fatal(err)
	}
	original := item.AbsPath
	sibling := filepath.Join(f.root, filepath.FromSlash(item.RelPath))
	if err := os.MkdirAll(filepath.Dir(sibling), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("unrelated sibling"), 0644); err != nil {
		t.Fatal(err)
	}
	item.AbsPath = ""
	if err := f.store.UpdateMedia(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RestoreMedia(ctx, f.media, f.user); err != nil {
		t.Fatalf("set-relative restore=%v", err)
	}
	if err := f.svc.SoftDeleteMedia(ctx, f.media, f.user); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RestoreMedia(ctx, f.media, f.user); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing set source accepted wrong root sibling: %v", err)
	}
	row, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil || row != nil {
		t.Fatalf("phantom active row=%v err=%v", row, err)
	}
	data, err := os.ReadFile(sibling)
	if err != nil || string(data) != "unrelated sibling" {
		t.Fatalf("sibling changed=%q err=%v", data, err)
	}
}

func TestTrashGCEmptyAbsPathUsesSetRootAndCleansOwnCache(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	item, err := f.store.GetMediaByID(ctx, f.media)
	if err != nil {
		t.Fatal(err)
	}
	source := item.AbsPath
	sibling := filepath.Join(f.root, filepath.FromSlash(item.RelPath))
	if err := os.MkdirAll(filepath.Dir(sibling), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("unrelated sibling"), 0644); err != nil {
		t.Fatal(err)
	}
	layout := thumb.NewLayout(f.root, f.cache)
	thumbnail, ok := layout.PathFor(source)
	if !ok {
		t.Fatal("thumbnail outside layout")
	}
	if err := os.MkdirAll(filepath.Dir(thumbnail), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumbnail, []byte("thumbnail"), 0644); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-8 * 24 * time.Hour)
	item.AbsPath = ""
	item.DeletedAt = &old
	item.ThumbnailPath = thumbnail
	if err := f.store.UpdateMedia(ctx, item); err != nil {
		t.Fatal(err)
	}
	cache := &fakeRenditionGC{}
	worker := NewGCWorker(f.store, &clock.MockClock{T: now}, f.root, time.Hour, nil).WithThumbnailLayout(layout).WithRenditionCache(cache)
	if err := worker.RunOnce(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{source, thumbnail} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("owned expired path remains=%s err=%v", path, err)
		}
	}
	data, err := os.ReadFile(sibling)
	if err != nil || string(data) != "unrelated sibling" {
		t.Fatalf("unrelated root sibling touched=%q err=%v", data, err)
	}
	trash, err := f.store.ListDeletedMedia(ctx)
	if err != nil || len(trash) != 0 {
		t.Fatalf("expired row remains=%v err=%v", trash, err)
	}
	if len(cache.removed) != 1 || cache.removed[0] != item.ID {
		t.Fatalf("renditions not cleaned=%v", cache.removed)
	}
}

func TestTrashGCUnknownLegacySetNeverGuessesSourcePath(t *testing.T) {
	for _, name := range []string{"missing set", "set lookup error", "empty relative path", "empty set root"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "file.jpg")
			if err := os.WriteFile(source, []byte("unrelated source"), 0644); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-8 * 24 * time.Hour)
			item := model.Media{ID: 1, SetID: 42, RelPath: "file.jpg", DeletedAt: &old}
			if name == "empty relative path" {
				item.RelPath = ""
			}
			deleted := false
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{ListDeletedMediaFunc: func(context.Context) ([]model.Media, error) { return []model.Media{item}, nil }, HardDeleteMediaFunc: func(context.Context, int64) error { deleted = true; return nil }},
				SetRepo: repository.MockSetRepo{GetSetByIDFunc: func(context.Context, int64) (*model.Set, error) {
					if name == "missing set" {
						return nil, nil
					}
					if name == "set lookup error" {
						return nil, errors.New("DB failure")
					}
					if name == "empty set root" {
						return &model.Set{ID: 42}, nil
					}
					return &model.Set{ID: 42, RootPath: "library"}, nil
				}},
			}
			worker := NewGCWorker(store, clock.RealClock{}, root, time.Hour, nil)
			if err := worker.RunOnce(); err != nil {
				t.Fatal(err)
			}
			if deleted {
				t.Fatal("unknown source row hard-deleted")
			}
			data, err := os.ReadFile(source)
			if err != nil || string(data) != "unrelated source" {
				t.Fatalf("guessed unrelated source changed=%q err=%v", data, err)
			}
		})
	}
}
