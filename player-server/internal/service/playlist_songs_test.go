package service

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestPlaylistSongsOccurrenceEditingPreservesHiddenAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "songs.db")
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
	owner := must(store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: now}))
	other := must(store.CreateUser(ctx, &model.User{Username: "other", PasswordHash: "x", IsAdmin: false, CreatedAt: now}))
	set := must(store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	hidden := must(store.CreateSet(ctx, &model.Set{Name: "Hidden", RootPath: "Hidden", CreatedAt: now}))
	if err := store.GrantPermission(ctx, &model.SetPermission{UserID: owner, SetID: set, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	add := func(set int64, name string, kind model.MediaType) int64 {
		return must(store.CreateMedia(ctx, &model.Media{SetID: set, FileName: name, RelPath: name, Type: kind, Music: &model.MusicMetadata{Title: name, Album: "Album"}, CreatedAt: now}))
	}
	a, b, h := add(set, "A.mp3", model.MediaTypeAudio), add(set, "B.mp3", model.MediaTypeAudio), add(hidden, "Hidden.mp3", model.MediaTypeAudio)
	image := add(set, "cover.jpg", model.MediaTypeImage)
	svc := NewPlaylistService(store, NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil))
	p, err := svc.Create(ctx, owner, "Edit", "notes")
	if err != nil {
		t.Fatal(err)
	}
	edit := func(action, entry, before string, ids ...int64) *PlaylistView {
		t.Helper()
		got, err := svc.EditSongs(ctx, owner, p.ID, action, entry, before, ids)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	p = edit("add", "", "", a, b, a)
	original := append([]model.PlaylistEntry(nil), p.Entries...)
	if original[0].ID == original[2].ID {
		t.Fatal("duplicate identity collapsed")
	}
	if ok, err := store.EditPlaylist(ctx, owner, p.ID, repository.PlaylistChange{BeforeID: original[1].ID, Entries: []model.PlaylistEntry{{ID: "hidden", MediaID: h}}}); err != nil || !ok {
		t.Fatal(err)
	}
	p = edit("up", original[2].ID, "")
	if p.Entries[1].ID != original[2].ID || p.Entries[2].ID != original[1].ID {
		t.Fatalf("later duplicate move: %+v", p.Entries)
	}
	p = edit("remove", original[2].ID, "")
	if len(p.Entries) != 2 || p.Entries[0].ID != original[0].ID {
		t.Fatalf("wrong duplicate removed: %+v", p.Entries)
	}
	// Undo re-adds at the saved next occurrence rather than altering a same-media occurrence.
	p = edit("add", "", original[1].ID, a)
	if p.Songs[1].ID != a || p.Entries[1].ID == original[2].ID {
		t.Fatalf("undo %+v", p)
	}
	p = edit("down", original[0].ID, "")
	if p.Entries[1].ID != original[0].ID {
		t.Fatalf("move across hidden occurrence %+v", p.Entries)
	}
	saved, err := store.GetPlaylist(ctx, owner, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Entries) != 4 || saved.Entries[1].ID != "hidden" {
		t.Fatalf("hidden lost %+v", saved)
	}
	for _, tc := range []struct {
		name, action, entry, before string
		user                        int64
		ids                         []int64
	}{
		{name: "foreign", action: "remove", entry: original[0].ID, user: other},
		{name: "foreign add", action: "add", user: other, ids: []int64{a}},
		{name: "missing occurrence", action: "down", entry: "missing", user: owner},
		{name: "hidden occurrence", action: "remove", entry: "hidden", user: owner},
		{name: "mixed inaccessible add", action: "add", user: owner, ids: []int64{a, h}},
		{name: "nonmusic", action: "add", user: owner, ids: []int64{image}},
		{name: "hidden anchor", action: "add", before: "hidden", user: owner, ids: []int64{a}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.EditSongs(ctx, tc.user, p.ID, tc.action, tc.entry, tc.before, tc.ids)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("got %v", err)
			}
			unchanged, err := store.GetPlaylist(ctx, owner, p.ID)
			if err != nil || !reflect.DeepEqual(unchanged, saved) {
				t.Fatalf("partial mutation %+v %v", unchanged, err)
			}
		})
	}
	for _, action := range []string{"unknown", "add"} {
		if _, err := svc.EditSongs(ctx, owner, p.ID, action, "", "", nil); err == nil {
			t.Fatalf("accepted %s", action)
		}
	}
	// Missing undo anchor appends and preserves full membership.
	p = edit("add", "", "gone", b)
	if p.Songs[len(p.Songs)-1].ID != b {
		t.Fatal("missing anchor did not append")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = repository.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc = NewPlaylistService(store, NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil))
	restored, err := svc.Get(ctx, owner, p.ID)
	if err != nil || !reflect.DeepEqual(p, restored) {
		t.Fatalf("restart %+v %v", restored, err)
	}
}
