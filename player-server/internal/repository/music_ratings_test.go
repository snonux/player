package repository

import (
	"github.com/snonux/player/internal/model"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMusicRatingsPersistenceIsolationAndCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "stars.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, &model.User{Username: "one", PasswordHash: "x", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, &model.User{Username: "two", PasswordHash: "x", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"album", "artist"} {
		for _, value := range []int{1, 5, 3} {
			if err := store.SetMusicRating(ctx, user, kind, "key", value); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.SetMusicRating(ctx, other, kind, "key", 2); err != nil {
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
	for _, kind := range []string{"album", "artist"} {
		values, err := store.ListMusicRatings(ctx, user, kind)
		if err != nil || values["key"] != 3 {
			t.Fatalf("restart %v %v", values, err)
		}
		if err := store.SetMusicRating(ctx, user, kind, "key", 0); err != nil {
			t.Fatal(err)
		}
		values, err = store.ListMusicRatings(ctx, user, kind)
		if err != nil || len(values) != 0 {
			t.Fatalf("clear %v %v", values, err)
		}
		values, err = store.ListMusicRatings(ctx, other, kind)
		if err != nil || values["key"] != 2 {
			t.Fatalf("isolation %v %v", values, err)
		}
	}
	for _, value := range []int{-1, 6} {
		if err := store.SetMusicRating(ctx, user, "album", "key", value); err == nil {
			t.Fatal("invalid accepted")
		}
	}
	if err := store.DeleteUser(ctx, other); err != nil {
		t.Fatal(err)
	}
	values, err := store.ListMusicRatings(ctx, other, "album")
	if err != nil || len(values) != 0 {
		t.Fatalf("user cleanup %v %v", values, err)
	}
}
