package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestRadioBookmarkPersistenceAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "radio.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := store.CreateUser(ctx, &model.User{Username: "radio-user", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	station := model.RadioStation{
		UUID: "11111111-2222-3333-4444-555555555555", Name: "Local Jazz",
		StreamURL: "http://127.0.0.1/live", Homepage: "https://example.com",
		FaviconURL: "http://127.0.0.1/art.png", Country: "Germany", CountryCode: "DE",
		Language: "en", Tags: []string{"jazz", "live"}, Codec: "mp3", Bitrate: 128, Votes: 3,
	}
	saved, err := store.UpsertRadioBookmark(ctx, &model.RadioBookmark{
		UserID: userID, Station: station, CreatedAt: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Station.Name != station.Name || saved.Station.UUID != station.UUID {
		t.Fatalf("upsert %+v", saved)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	items, err := store.ListRadioBookmarks(ctx, userID)
	if err != nil || len(items) != 1 || items[0].Station.Name != "Local Jazz" {
		t.Fatalf("restart list %+v %v", items, err)
	}
	got, err := store.GetRadioBookmark(ctx, userID, station.UUID)
	if err != nil || got == nil || got.Station.Codec != "mp3" {
		t.Fatalf("get %+v %v", got, err)
	}

	station.Name = "Edited Jazz"
	station.Bitrate = 256
	updated, err := store.UpsertRadioBookmark(ctx, &model.RadioBookmark{
		UserID: userID, Station: station, CreatedAt: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || updated.Station.Name != "Edited Jazz" || updated.Station.Bitrate != 256 {
		t.Fatalf("refresh %+v %v", updated, err)
	}
	// First bookmark time is kept on conflict.
	if !updated.CreatedAt.Equal(time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("created_at changed to %v", updated.CreatedAt)
	}

	if err := store.DeleteRadioBookmark(ctx, userID, station.UUID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteRadioBookmark(ctx, userID, station.UUID); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
	gone, err := store.GetRadioBookmark(ctx, userID, station.UUID)
	if err != nil || gone != nil {
		t.Fatalf("removed %+v %v", gone, err)
	}
}

func TestRadioBookmarkMissingUserReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "radio-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	items, err := store.ListRadioBookmarks(ctx, 999)
	if err != nil || len(items) != 0 {
		t.Fatalf("empty list %+v %v", items, err)
	}
	got, err := store.GetRadioBookmark(ctx, 999, "11111111-2222-3333-4444-555555555555")
	if err != nil || got != nil {
		t.Fatalf("missing get %+v %v", got, err)
	}
}
