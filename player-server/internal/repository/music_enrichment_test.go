package repository

import (
	"codeberg.org/snonux/player/internal/model"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMusicEnrichmentPersistenceNamespace(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "metadata.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveMusicEnrichment(ctx, "provider-one", "artist", "key", model.MusicEnrichment{Description: "Biography", Source: "Fixture", FetchedAt: &now}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	item, err := store.GetMusicEnrichment(ctx, "provider-one", "artist", "key")
	if err != nil || item == nil || item.Description != "Biography" || item.FetchedAt == nil || !item.FetchedAt.Equal(now) {
		t.Fatalf("restart: %+v %v", item, err)
	}
	for _, scope := range []struct{ provider, kind, key string }{{"other", "artist", "key"}, {"provider-one", "album", "key"}, {"provider-one", "artist", "hidden"}} {
		item, err := store.GetMusicEnrichment(ctx, scope.provider, scope.kind, scope.key)
		if err != nil || item != nil {
			t.Fatalf("namespace leaked: %+v %v", item, err)
		}
	}
}
