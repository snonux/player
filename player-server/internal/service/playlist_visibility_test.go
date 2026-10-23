package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestPlaylistVisibilityOwnershipAndMediaACL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "visibility.db")
	store, err := repository.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	must := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	now := time.Now().UTC()
	alice := must(store.CreateUser(ctx, &model.User{Username: "alice", PasswordHash: "x", CreatedAt: now}))
	bob := must(store.CreateUser(ctx, &model.User{Username: "bob", PasswordHash: "x", CreatedAt: now}))
	admin := must(store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "x", IsAdmin: true, CreatedAt: now}))
	set := must(store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	if err := store.GrantPermission(ctx, &model.SetPermission{UserID: alice, SetID: set, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	song := must(store.CreateMedia(ctx, &model.Media{SetID: set, FileName: "song.mp3", RelPath: "song.mp3", Type: model.MediaTypeAudio, Duration: 30, ThumbnailPath: "cover", Music: &model.MusicMetadata{Album: "Album"}, CreatedAt: now}))
	svc := NewPlaylistService(store, NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil))
	p, err := svc.Create(ctx, alice, "Private name", "comment")
	if err != nil {
		t.Fatal(err)
	}
	if p.Visibility != "private" || p.OwnerID != alice || !p.CanEdit || p.CanTransfer {
		t.Fatalf("default %+v", p)
	}
	if _, err := svc.Get(ctx, bob, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private read %v", err)
	}
	if list, err := svc.List(ctx, bob); err != nil || len(list) != 0 {
		t.Fatalf("private metadata %+v %v", list, err)
	}
	if _, err := svc.EditSongs(ctx, alice, p.ID, "add", "", "", []int64{song}); err != nil {
		t.Fatal(err)
	}
	if docs, err := svc.Membership(ctx, bob); err != nil || len(docs) != 0 {
		t.Fatalf("private membership leak %+v %v", docs, err)
	}
	if err := svc.AddToPlaylists(ctx, alice, []string{p.ID}, []int64{song, song}, "skip"); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Get(ctx, alice, p.ID); err != nil || len(got.Entries) != 1 {
		t.Fatalf("skip %+v %v", got, err)
	}
	shared := "shared"
	p, err = svc.UpdateDetails(ctx, alice, p.ID, p.Name, p.Comment, &shared, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Get(ctx, bob, p.ID)
	if err != nil || b.CanEdit || b.CanTransfer || b.SongCount != 0 || b.CoverMediaID != 0 || len(b.Entries) != 0 || len(b.Songs) != 0 {
		t.Fatalf("shared ACL %+v %v", b, err)
	}
	if list, err := svc.List(ctx, bob); err != nil || len(list) != 1 || list[0].SongCount != 0 {
		t.Fatalf("shared list %+v %v", list, err)
	}
	if docs, err := svc.Membership(ctx, bob); err != nil || len(docs) != 1 || len(docs[0].Entries) != 0 {
		t.Fatalf("hidden membership leak %+v %v", docs, err)
	}
	if err := svc.AddToPlaylists(ctx, bob, []string{p.ID}, []int64{song}, "skip"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden song add %v", err)
	}
	for name, action := range map[string]func() error{
		"metadata":       func() error { _, e := svc.Update(ctx, bob, p.ID, "stolen", ""); return e },
		"delete":         func() error { return svc.Delete(ctx, bob, p.ID) },
		"album":          func() error { _, e := svc.AddAlbum(ctx, bob, p.ID, "missing"); return e },
		"songs":          func() error { _, e := svc.EditSongs(ctx, bob, p.ID, "add", "", "", []int64{song}); return e },
		"owner transfer": func() error { _, e := svc.UpdateDetails(ctx, alice, p.ID, "stolen", "", nil, &bob); return e },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); !errors.Is(err, ErrForbidden) {
				t.Fatalf("got %v", err)
			}
		})
	}
	missing := int64(99999)
	if _, err := svc.UpdateDetails(ctx, admin, p.ID, "bad", "", nil, &missing); err == nil {
		t.Fatal("accepted missing owner")
	}
	original, err := svc.Get(ctx, alice, p.ID)
	if err != nil || original.Name != "Private name" {
		t.Fatalf("partial update %+v %v", original, err)
	}
	// Legacy metadata clients leave visibility and ownership unchanged.
	p, err = svc.Update(ctx, alice, p.ID, "Renamed", "legacy")
	if err != nil || p.Visibility != "shared" || p.OwnerID != alice {
		t.Fatalf("legacy %+v %v", p, err)
	}
	private := "private"
	p, err = svc.UpdateDetails(ctx, admin, p.ID, p.Name, p.Comment, &private, &bob)
	if err != nil || p.OwnerID != bob || !p.CanTransfer {
		t.Fatalf("transfer %+v %v", p, err)
	}
	if _, err := svc.Get(ctx, alice, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old owner read %v", err)
	}
	if _, err := svc.Update(ctx, alice, p.ID, "stolen", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old owner write %v", err)
	}
	p, err = svc.Get(ctx, bob, p.ID)
	if err != nil || !p.CanEdit || p.CanTransfer || p.SongCount != 0 {
		t.Fatalf("new owner %+v %v", p, err)
	}
	// Transfers preserve all saved membership without granting media access.
	if err := store.GrantPermission(ctx, &model.SetPermission{UserID: bob, SetID: set, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = repository.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc = NewPlaylistService(store, NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil))
	p, err = svc.Get(ctx, bob, p.ID)
	if err != nil || p.OwnerName != "bob" || p.Visibility != "private" || p.SongCount != 1 || p.Songs[0].ID != song {
		t.Fatalf("restart %+v %v", p, err)
	}
}
