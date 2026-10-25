package repository

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestShareQualityM62UpgradeAndOmittedEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m62.db")
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
	mid, err := store.CreateMedia(ctx, &model.Media{SetID: sid, FileName: "song.mp3", RelPath: "song.mp3", AbsPath: "/set/song.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	max := 3
	shares := []model.Share{{Token: "single", MediaID: mid, CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(time.Hour), MaxUses: &max, UsedCount: 2}, {Token: "collection", CollectionTitle: "Album", Description: "Keep", MediaIDs: []int64{mid, mid}, CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(time.Hour), MaxUses: &max, UsedCount: 1}}
	for i := range shares {
		if err := store.CreateShare(ctx, &shares[i]); err != nil {
			t.Fatal(err)
		}
	}
	key, err := store.ShareViewingKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The live M62 schema already has nullable media_id and ordered media_ids;
	// remove only M65's column to reproduce a real deployed upgrade boundary.
	if _, err := store.db.Exec(`ALTER TABLE shares DROP COLUMN audio_quality`); err != nil {
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
	for _, want := range shares {
		got, err := store.GetShareByToken(ctx, want.Token)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, &want) {
			t.Fatalf("upgrade changed share: got %+v want %+v", got, want)
		}
	}
	gotKey, err := store.ShareViewingKey(ctx)
	if err != nil || !reflect.DeepEqual(key, gotKey) {
		t.Fatal("viewing signing key changed", err)
	}
	quality := "opus96"
	if _, err := store.UpdateShare(ctx, "collection", nil, nil, &quality); err != nil {
		t.Fatal(err)
	}
	description := "Updated by older client"
	if _, err := store.UpdateShare(ctx, "collection", &description, nil); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetShareByToken(ctx, "collection")
	if err != nil {
		t.Fatal(err)
	}
	if got.AudioQuality != quality || got.Description != description || got.UsedCount != 1 || !reflect.DeepEqual(got.MediaIDs, shares[1].MediaIDs) {
		t.Fatalf("omitted policy lost: %+v", got)
	}
}
