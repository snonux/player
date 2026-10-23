package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestPlaybackSyncMigrationResumesPartialUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := seedOwned(t, s, "sync")
	created := time.Date(2026, 10, 15, 12, 0, 0, 123000000, time.FixedZone("east", 10800))
	id, err := s.CreateBookmark(context.Background(), &model.Bookmark{UserID: owner.userID, MediaID: owner.mediaID, PositionSeconds: 3, CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a killed upgrade after adding sync_key but before later ALTERs.
	for _, query := range []string{`DROP INDEX idx_bookmark_sync_key`, `ALTER TABLE media_bookmarks DROP COLUMN updated_ms`, `ALTER TABLE media_bookmarks DROP COLUMN deleted`, `UPDATE media_bookmarks SET sync_key=''`, `DROP TABLE playback_resets`} {
		if _, err := s.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var key string
		var ms, deleted int64
		if err := s.db.QueryRow(`SELECT sync_key,updated_ms,deleted FROM media_bookmarks WHERE id=?`, id).Scan(&key, &ms, &deleted); err != nil {
			t.Fatal(err)
		}
		if key == "" || ms != created.UnixMilli() || deleted != 0 {
			t.Fatalf("legacy bookmark after upgrade: key=%q ms=%d deleted=%d", key, ms, deleted)
		}
		snapshot, err := s.SyncPlayback(context.Background(), owner.userID, owner.mediaID, model.PlaybackSync{Bookmarks: []model.SyncedBookmark{{Key: key, PositionSeconds: 1, ObservedMS: created.UnixMilli() - 1}}})
		if err != nil || len(snapshot.Bookmarks) != 1 || snapshot.Bookmarks[0].PositionSeconds != 3 {
			t.Fatalf("stale edit survived migration: %+v err=%v", snapshot, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBookmarkDeleteSameMillisecondAndFutureEdit(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owner := seedOwned(t, s, "delete")
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 12, 0, 0, 123000000, time.UTC)
	id, err := s.CreateBookmark(ctx, &model.Bookmark{UserID: owner.userID, MediaID: owner.mediaID, PositionSeconds: 3, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteBookmarkAt(ctx, id, owner.mediaID, owner.userID, now)
	if err != nil || !deleted {
		t.Fatalf("same-ms deletion=%v,%v", deleted, err)
	}
	got, err := s.SyncPlayback(ctx, owner.userID, owner.mediaID, model.PlaybackSync{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bookmarks) != 1 || !got.Bookmarks[0].Deleted || got.Bookmarks[0].ObservedMS != now.UnixMilli()+1 {
		t.Fatalf("same-ms tombstone: %+v", got)
	}
	future := now.Add(time.Hour)
	id, err = s.CreateBookmark(ctx, &model.Bookmark{UserID: owner.userID, MediaID: owner.mediaID, PositionSeconds: 4, CreatedAt: future})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err = s.DeleteBookmarkAt(ctx, id, owner.mediaID, owner.userID, now)
	if err != nil || !deleted {
		t.Fatalf("owned older deletion=%v,%v", deleted, err)
	}
	live, err := s.ListBookmarks(ctx, owner.mediaID, owner.userID)
	if err != nil || len(live) != 1 || live[0].ID != id {
		t.Fatalf("newer future edit should remain: %+v err=%v", live, err)
	}
}
