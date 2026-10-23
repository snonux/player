package service

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestCreateFromQueueAtomicPrivateOccurrences(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	must := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	user := must(store.CreateUser(ctx, &model.User{Username: "queue-owner", PasswordHash: "x", CreatedAt: now}))
	other := must(store.CreateUser(ctx, &model.User{Username: "queue-other", PasswordHash: "x", CreatedAt: now}))
	set := must(store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	if err := store.GrantPermission(ctx, &model.SetPermission{UserID: user, SetID: set, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	add := func(name string) int64 {
		return must(store.CreateMedia(ctx, &model.Media{SetID: set, FileName: name, RelPath: name, Type: model.MediaTypeAudio, Music: &model.MusicMetadata{Title: name, Album: "Album"}, CreatedAt: now}))
	}
	a, b := add("a.mp3"), add("b.mp3")
	svc := NewPlaylistService(store, NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil))
	for _, ids := range [][]int64{{}, {a, 999999}, make([]int64, 10001)} {
		if _, err := svc.CreateFromQueue(ctx, user, "queue", "", nil, ids); err == nil {
			t.Fatalf("accepted invalid selection %d", len(ids))
		}
	}
	if docs, err := store.ListPlaylists(ctx, user); err != nil || len(docs) != 0 {
		t.Fatalf("partial documents: %v %v", docs, err)
	}
	shared := "shared"
	if _, err := svc.CreateFromQueue(ctx, user, "shared", "", &shared, []int64{a}); err == nil {
		t.Fatal("queue copy shared")
	}
	if _, err := svc.CreateFromQueue(ctx, other, "foreign", "", nil, []int64{a}); err == nil {
		t.Fatal("inaccessible song accepted")
	}
	p, err := svc.CreateFromQueue(ctx, user, "  Night drive 🌙  ", "", nil, []int64{b, a, b})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Night drive 🌙" || p.OwnerID != user || p.Visibility != "private" || p.SongCount != 3 {
		t.Fatalf("bad metadata %+v", p)
	}
	if !reflect.DeepEqual([]int64{p.Entries[0].MediaID, p.Entries[1].MediaID, p.Entries[2].MediaID}, []int64{b, a, b}) || p.Entries[0].ID == p.Entries[2].ID {
		t.Fatal("lost ordered occurrences")
	}
	if _, err := svc.Get(ctx, other, p.ID); err == nil {
		t.Fatal("foreign playlist visible")
	}
	// Force a repository insert failure after the document insert: transaction rolls back.
	if _, err := store.ImportPlaylist(ctx, user, model.Playlist{ID: "broken", Name: "broken", Entries: []model.PlaylistEntry{{ID: "bad-song", MediaID: 999999}}}, true); err == nil {
		t.Fatal("foreign key accepted")
	}
	if got, err := store.GetPlaylist(ctx, user, "broken"); err != nil || got != nil {
		t.Fatal("partial playlist survived", err)
	}
	// Later missing songs disappear without merging or reordering survivors.
	if err := store.HardDeleteMedia(ctx, a); err != nil {
		t.Fatal(err)
	}
	p, err = svc.Get(ctx, user, p.ID)
	if err != nil || p.SongCount != 2 || p.Entries[0].MediaID != b || p.Entries[1].MediaID != b {
		t.Fatalf("deleted handling %+v %v", p, err)
	}
}
