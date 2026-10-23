package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func TestMusicLibraryService_CRUDAccessAndFilter(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lib.db")
	store, err := repository.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	clk := &clock.MockClock{T: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	rootA := filepath.Join(dir, "a")
	rootB := filepath.Join(dir, "b")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(filepath.Join(root, "Music"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	roots := NewPathRoots(rootA)
	browse := &albumSource{sets: nil}
	svc := NewMusicLibraryService(store, clk, roots, browse)

	def, err := svc.EnsureDefault(ctx, rootA)
	if err != nil || def == nil || def.RootPath != rootA {
		t.Fatalf("default library: %#v err=%v", def, err)
	}
	if got := roots.All(); len(got) != 1 || got[0] != rootA {
		t.Fatalf("roots after default: %v", roots.All())
	}

	libB, err := svc.Create(ctx, "Second", rootB)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots.All()) != 2 {
		t.Fatalf("expected 2 roots, got %v", roots.All())
	}

	setA := &model.Set{LibraryID: def.ID, Name: "Music", RootPath: "Music", CreatedAt: clk.T}
	setBID, err := store.CreateSet(ctx, &model.Set{LibraryID: libB.ID, Name: "Music", RootPath: "Music", CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}
	setAID, err := store.CreateSet(ctx, setA)
	if err != nil {
		t.Fatal(err)
	}

	adminID, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "x", IsAdmin: true, CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}
	userID, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "x", CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.GrantAccess(ctx, libB.ID, userID, model.RoleViewer); err != nil {
		t.Fatal(err)
	}
	perm, err := store.GetPermission(ctx, setBID, userID)
	if err != nil || perm == nil || perm.Role != model.RoleViewer {
		t.Fatalf("expected viewer on set B, got %#v err=%v", perm, err)
	}
	if p, _ := store.GetPermission(ctx, setAID, userID); p != nil {
		t.Fatalf("set A should stay ungated for viewer, got %#v", p)
	}

	browse.sets = []model.Set{{ID: setBID, LibraryID: libB.ID, Name: "Music"}}
	visible, err := svc.ListForUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].ID != libB.ID {
		t.Fatalf("viewer libraries: %#v", visible)
	}
	adminLibs, err := svc.ListForUser(ctx, adminID)
	if err != nil || len(adminLibs) != 2 {
		t.Fatalf("admin libraries: %#v err=%v", adminLibs, err)
	}

	filtered := FilterSetsByLibraries([]model.Set{
		{ID: setAID, LibraryID: def.ID},
		{ID: setBID, LibraryID: libB.ID},
	}, []int64{libB.ID})
	if len(filtered) != 1 || filtered[0].ID != setBID {
		t.Fatalf("filter: %#v", filtered)
	}

	if err := svc.RevokeAccess(ctx, libB.ID, userID); err != nil {
		t.Fatal(err)
	}
	if p, _ := store.GetPermission(ctx, setBID, userID); p != nil {
		t.Fatalf("revoked permission still present: %#v", p)
	}

	if err := store.DeleteSet(ctx, setBID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, libB.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, def.ID); err != ErrLastLibrary {
		t.Fatalf("delete last: %v", err)
	}
}

func TestPathRoots_Contains(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	roots := NewPathRoots(a, b)
	inside := filepath.Join(a, "Music", "song.mp3")
	if !roots.ContainsLexical(inside) {
		t.Fatalf("expected %s inside roots", inside)
	}
	outside := filepath.Join(t.TempDir(), "secret.mp3")
	if roots.ContainsLexical(outside) {
		t.Fatalf("expected %s outside roots", outside)
	}
}

func TestParseLibraryIDs(t *testing.T) {
	ids, err := ParseLibraryIDs("1, 2")
	if err != nil || len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("got %v err=%v", ids, err)
	}
	if _, err := ParseLibraryIDs("nope"); err == nil {
		t.Fatal("expected error")
	}
}
