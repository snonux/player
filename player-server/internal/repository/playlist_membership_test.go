package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestPlaylistMultiAddAtomicDuplicates(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	mustID := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	now := time.Now().UTC()
	user := mustID(s.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: now}))
	other := mustID(s.CreateUser(ctx, &model.User{Username: "other", PasswordHash: "x", CreatedAt: now}))
	set := mustID(s.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	media := mustID(s.CreateMedia(ctx, &model.Media{SetID: set, FileName: "song.mp3", RelPath: "song.mp3", Type: model.MediaTypeAudio, CreatedAt: now}))
	for _, id := range []string{"first", "second", "concurrent"} {
		if err := s.CreatePlaylist(ctx, user, model.Playlist{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	entries := func(prefix string) []model.PlaylistEntry {
		return []model.PlaylistEntry{{ID: prefix + "a", MediaID: media}, {ID: prefix + "b", MediaID: media}}
	}
	if ok, err := s.AddPlaylistSongs(ctx, user, map[string][]model.PlaylistEntry{"first": entries("one"), "second": entries("two")}, true); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, id := range []string{"first", "second"} {
		p, err := s.GetPlaylist(ctx, user, id)
		if err != nil || len(p.Entries) != 1 {
			t.Fatalf("skip batch %+v %v", p, err)
		}
	}
	if ok, err := s.AddPlaylistSongs(ctx, user, map[string][]model.PlaylistEntry{"first": entries("three"), "missing": entries("four")}, false); err != nil || ok {
		t.Fatal(ok, err)
	}
	p, err := s.GetPlaylist(ctx, user, "first")
	if err != nil || len(p.Entries) != 1 {
		t.Fatalf("partial commit %+v %v", p, err)
	}
	if ok, err := s.AddPlaylistSongs(ctx, other, map[string][]model.PlaylistEntry{"first": entries("five")}, false); err != nil || ok {
		t.Fatal(ok, err)
	}
	if ok, err := s.AddPlaylistSongs(ctx, user, map[string][]model.PlaylistEntry{"first": entries("six")}, false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	p, err = s.GetPlaylist(ctx, user, "first")
	if err != nil || len(p.Entries) != 3 || p.Entries[1].ID != "sixa" || p.Entries[2].ID != "sixb" {
		t.Fatalf("add order %+v %v", p, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if ok, err := s.AddPlaylistSongs(ctx, user, map[string][]model.PlaylistEntry{"concurrent": entries(fmt.Sprint("parallel", i))}, true); err != nil || !ok {
				t.Errorf("parallel %v %v", ok, err)
			}
		}(i)
	}
	wg.Wait()
	p, err = s.GetPlaylist(ctx, user, "concurrent")
	if err != nil || len(p.Entries) != 1 {
		t.Fatalf("concurrent skip %+v %v", p, err)
	}
}
