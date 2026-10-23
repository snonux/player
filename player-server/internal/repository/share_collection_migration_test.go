package repository

import (
	"github.com/snonux/player/internal/model"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestShareCollectionMigrationPreservesLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	uid, err := store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "h", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	sid, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mid, err := store.CreateMedia(ctx, &model.Media{SetID: sid, FileName: "a.mp3", RelPath: "a.mp3", AbsPath: "/set/a.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	max := 3
	legacy := &model.Share{Token: "legacy", MediaID: mid, CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(time.Hour), MaxUses: &max, UsedCount: 2}
	if err := store.CreateShare(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`CREATE TABLE shares_old(token TEXT PRIMARY KEY,media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,created_at DATETIME DEFAULT CURRENT_TIMESTAMP,expires_at DATETIME NOT NULL,max_uses INTEGER,used_count INTEGER NOT NULL DEFAULT 0); INSERT INTO shares_old SELECT token,media_id,created_by,created_at,expires_at,max_uses,used_count FROM shares; DROP TABLE shares; ALTER TABLE shares_old RENAME TO shares;`)
	if err != nil {
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
	got, err := store.GetShareByToken(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.MediaID != mid || got.CreatedBy != uid || got.UsedCount != 2 || got.MaxUses == nil || *got.MaxUses != max || !got.ExpiresAt.Equal(legacy.ExpiresAt) || !got.CreatedAt.Equal(now) {
		t.Fatalf("legacy changed: %+v", got)
	}
	coll := &model.Share{Token: "collection", CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(time.Hour), CollectionTitle: "Album", Description: "Description stays", MediaIDs: []int64{mid, mid}}
	if err := store.CreateShare(ctx, coll); err != nil {
		t.Fatal(err)
	}
	if err := store.HardDeleteMedia(ctx, mid); err != nil {
		t.Fatal(err)
	}
	if err := removeOrphanRows(store.db); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetShareByToken(ctx, coll.Token)
	if err != nil || got == nil || len(got.MediaIDs) != 2 || got.Description != "Description stays" {
		t.Fatalf("collection anchored: %+v %v", got, err)
	}
	got, err = store.GetShareByToken(ctx, legacy.Token)
	if err != nil || got != nil {
		t.Fatalf("legacy cascade changed: %+v %v", got, err)
	}
}
