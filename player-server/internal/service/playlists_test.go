package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestPlaylistsPersistencePermissionsAndLifecycle(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "playlists.db")
	store, err := repository.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	mustID := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner := mustID(store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: now}))
	other := mustID(store.CreateUser(ctx, &model.User{Username: "other", IsAdmin: false, PasswordHash: "x", CreatedAt: now}))
	set := mustID(store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	hidden := mustID(store.CreateSet(ctx, &model.Set{Name: "Hidden", RootPath: "Hidden", CreatedAt: now}))
	if err := store.GrantPermission(ctx, &model.SetPermission{UserID: owner, SetID: set, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for i, setID := range []int64{set, set, hidden} {
		ids = append(ids, mustID(store.CreateMedia(ctx, &model.Media{SetID: setID, FileName: []string{"b.mp3", "a.mp3", "hidden.mp3"}[i], RelPath: []string{"b.mp3", "a.mp3", "hidden.mp3"}[i], Type: model.MediaTypeAudio, Duration: float64(i+1) * 10, ThumbnailPath: "cover", Music: &model.MusicMetadata{Album: "Album", AlbumArtist: "Artist", Track: 3 - i}, CreatedAt: now})))
	}
	source := NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	svc := NewPlaylistService(store, source)
	p, err := svc.Create(ctx, owner, "  🌙 東京  ", "line one\n<plain> & comment")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "🌙 東京" || p.SongCount != 0 || len(p.Songs) != 0 {
		t.Fatalf("empty %+v", p)
	}
	albums, err := MusicAlbums(ctx, source, owner)
	if err != nil || len(albums) != 1 {
		t.Fatalf("albums %v %v", albums, err)
	}
	p, err = svc.AddAlbum(ctx, owner, p.ID, albums[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	if p.SongCount != 2 || p.Duration != 30 || p.CoverMediaID != ids[1] || p.Songs[0].ID != ids[1] {
		t.Fatalf("aggregate/order %+v", p)
	}
	p, err = svc.AddAlbum(ctx, owner, p.ID, albums[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	if p.SongCount != 4 || p.Duration != 60 || p.Entries[0].ID == p.Entries[2].ID {
		t.Fatalf("duplicates %+v", p)
	}
	for _, action := range []func() error{
		func() error { _, e := svc.Get(ctx, other, p.ID); return e },
		func() error { _, e := svc.Update(ctx, other, p.ID, "stolen", ""); return e },
		func() error { _, e := svc.AddAlbum(ctx, other, p.ID, albums[0].Key); return e },
		func() error { return svc.Delete(ctx, other, p.ID) },
	} {
		if err := action(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign owner %v", err)
		}
	}
	if list, err := svc.List(ctx, other); err != nil || len(list) != 0 {
		t.Fatalf("foreign list %+v %v", list, err)
	}
	if _, err := svc.AddAlbum(ctx, owner, p.ID, "not-an-album"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid album %v", err)
	}
	renamed, err := svc.Update(ctx, owner, p.ID, "Road Trip", "saved comment")
	if err != nil || renamed.SongCount != 4 {
		t.Fatalf("rename %+v %v", renamed, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = repository.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	source = NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	svc = NewPlaylistService(store, source)
	restored, err := svc.Get(ctx, owner, p.ID)
	if err != nil || restored.Name != "Road Trip" || restored.Comment != "saved comment" || restored.SongCount != 4 {
		t.Fatalf("restart %+v %v", restored, err)
	}
	// Revocation immediately hides saved membership and aggregates, retaining it durably.
	if err := store.RevokePermission(ctx, set, owner); err != nil {
		t.Fatal(err)
	}
	restored, err = svc.Get(ctx, owner, p.ID)
	if err != nil || restored.SongCount != 0 || restored.CoverMediaID != 0 || len(restored.Entries) != 0 {
		t.Fatalf("revoked %+v %v", restored, err)
	}
	saved, err := store.GetPlaylist(ctx, owner, p.ID)
	if err != nil || len(saved.Entries) != 4 {
		t.Fatalf("durable membership %+v %v", saved, err)
	}
	if err := svc.Delete(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, owner, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted %v", err)
	}
	if m, err := store.GetMediaByID(ctx, ids[0]); err != nil || m == nil {
		t.Fatalf("source removed %+v %v", m, err)
	}
}

func TestPlaylistMetadataValidation(t *testing.T) {
	for _, test := range []struct {
		name, comment string
		valid         bool
	}{
		{"", "", false}, {" \n ", "", false}, {strings.Repeat("🌙", 200), strings.Repeat("界", 2000), true}, {strings.Repeat("🌙", 201), "", false}, {"n", strings.Repeat("界", 2001), false}, {"good", "\n<not html>", true},
	} {
		t.Run(test.name[:min(len(test.name), 12)], func(t *testing.T) {
			_, err := validatePlaylist(test.name, test.comment)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
