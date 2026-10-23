package repository

import (
	"context"
	"testing"

	"github.com/snonux/player/internal/model"
)

func TestMusicArtworkPersistence(t *testing.T) {
	path := t.TempDir() + "/library.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	art := &model.MusicArtwork{Data: []byte("normalized"), Revision: "one"}
	if err := s.SaveMusicArtwork(ctx, "artist", "ar_one", art); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.GetMusicArtwork(ctx, "artist", "ar_one")
	if err != nil || got == nil || string(got.Data) != "normalized" {
		t.Fatalf("restart: %+v %v", got, err)
	}
	art.Revision = "two"
	if err := s.SaveMusicArtwork(ctx, "artist", "ar_one", art); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMusicArtwork(ctx, "artist", "ar_one")
	if err != nil || got.Revision != "two" {
		t.Fatal("replacement failed", err)
	}
	if err := s.SaveMusicArtwork(ctx, "artist", "ar_one", nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMusicArtwork(ctx, "artist", "ar_one")
	if err != nil || got != nil {
		t.Fatal("remove failed", err)
	}
}
