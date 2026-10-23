package repository

import (
	"codeberg.org/snonux/player/internal/model"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMusicEntityFavoritesPersistenceAndUserCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "favorites.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, &model.User{Username: "one", PasswordHash: "x", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"album", "artist", "playlist"} {
		if err := store.SetMusicFavorite(ctx, user, kind, "saved-key", true); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	for _, kind := range []string{"album", "artist", "playlist"} {
		keys, err := store.ListMusicFavorites(ctx, user, kind)
		if err != nil || !keys["saved-key"] {
			t.Fatalf("restart %s %v %v", kind, keys, err)
		}
	}
	if err := store.DeleteUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	keys, err := store.ListMusicFavorites(ctx, user, "album")
	if err != nil || len(keys) != 0 {
		t.Fatalf("user cleanup %v %v", keys, err)
	}
}
