package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/scanner"
	"github.com/snonux/player/internal/thumb"
)

func TestInventoryUpdatesActualMusicCatalog(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "Music", "song.mp3")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := repository.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	user, err := store.CreateUser(ctx, &model.User{Username: "admin", IsAdmin: true, PasswordHash: "hash", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	album, artist := "First album", "First artist"
	var probes int
	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		probes++
		return &model.Metadata{Music: &model.MusicMetadata{Title: "Song", Album: album, Artist: artist, AlbumArtist: artist, Artists: []string{artist}, AlbumArtists: []string{artist}}}, nil
	}}
	scan := scanner.NewFSScanner(store, prober, &thumb.MockGenerator{}, clock.RealClock{}, root)
	source := NewMediaService(store, clock.RealClock{}, root, &thumb.MockGenerator{}, prober)
	var id int64
	for _, title := range []string{"First album", "Retagged album"} {
		album = title
		if title == "Retagged album" {
			artist = "Retagged artist"
			if err := os.WriteFile(path, []byte(title), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := scan.ScanInventory(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		albums, err := MusicAlbums(ctx, source, user)
		if err != nil || len(albums) != 1 {
			t.Fatalf("catalog=%+v err=%v", albums, err)
		}
		if albums[0].Title != album || albums[0].Artist != artist || len(albums[0].Songs) != 1 {
			t.Fatalf("stale album catalog %+v", albums)
		}
		people := MusicArtists(albums)
		if len(people) != 1 || people[0].Name != artist {
			t.Fatalf("stale artist catalog %+v", people)
		}
		if id == 0 {
			id = albums[0].Songs[0].ID
			if err := store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: user, MediaID: id, PositionSeconds: 12, UpdatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
		} else if albums[0].Songs[0].ID != id {
			t.Fatal("retag changed stable song identity")
		}
		count := probes
		if err := scan.ScanInventory(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if probes != count {
			t.Fatalf("unchanged audio reprobed: %d → %d", count, probes)
		}
	}
	progress, err := store.GetProgress(ctx, user, id)
	if err != nil || progress == nil || progress.PositionSeconds != 12 {
		t.Fatalf("saved progress lost: %+v %v", progress, err)
	}
	if _, err := store.ToggleFavorite(ctx, user, id); err != nil {
		t.Fatal(err)
	}
	if err := store.IncrementPlayCount(ctx, id); err != nil {
		t.Fatal(err)
	}
	playlist := model.Playlist{ID: "inventory-history", Name: "Keep"}
	if err := store.CreatePlaylist(ctx, user, playlist); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.AppendPlaylist(ctx, user, playlist.ID, []model.PlaylistEntry{{ID: "original-occurrence", MediaID: id}}); err != nil || !ok {
		t.Fatalf("append playlist: %v %v", ok, err)
	}
	if err := os.Rename(path, filepath.Join(filepath.Dir(path), "renamed.mp3")); err != nil {
		t.Fatal(err)
	}
	if err := scan.ScanInventory(ctx, root, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListMedia(ctx, repository.MediaFilter{})
	if err != nil || len(rows) != 1 || rows[0].ID != id || rows[0].FileName != "renamed.mp3" || rows[0].PlayCount != 1 {
		t.Fatalf("rename lost identity/history: %+v %v", rows, err)
	}
	progress, err = store.GetProgress(ctx, user, id)
	if err != nil || progress == nil || progress.PositionSeconds != 12 {
		t.Fatalf("rename lost resume: %+v %v", progress, err)
	}
	favorites, err := store.ListFavoritesByUser(ctx, user)
	if err != nil || len(favorites) != 1 || favorites[0].MediaID != id {
		t.Fatalf("rename lost favorites: %+v %v", favorites, err)
	}
	saved, err := store.GetPlaylist(ctx, user, playlist.ID)
	if err != nil || len(saved.Entries) != 1 || saved.Entries[0].ID != "original-occurrence" || saved.Entries[0].MediaID != id {
		t.Fatalf("rename lost playlist occurrence: %+v %v", saved, err)
	}
}
