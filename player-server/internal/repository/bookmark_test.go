package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestBookmarksPersistAndIsolate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bookmarks.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	owner, other := seedOwned(t, s, "owner"), seedOwned(t, s, "other")
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.FixedZone("EEST", 10800))
	create := func(userID, mediaID int64, position float64) int64 {
		t.Helper()
		id, err := s.CreateBookmark(ctx, &model.Bookmark{UserID: userID, MediaID: mediaID, PositionSeconds: position, Label: "chapter", CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	last, first, second := create(owner.userID, owner.mediaID, 30), create(owner.userID, owner.mediaID, 10), create(owner.userID, owner.mediaID, 10)
	create(other.userID, owner.mediaID, 5)
	create(owner.userID, other.mediaID, 2)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListBookmarks(ctx, owner.mediaID, owner.userID)
	if err != nil || len(items) != 3 {
		t.Fatalf("persisted bookmarks = %+v, %v", items, err)
	}
	for i, id := range []int64{first, second, last} {
		if items[i].ID != id || !items[i].CreatedAt.Equal(now) || items[i].CreatedAt.Location() != time.UTC || items[i].Label != "chapter" {
			t.Fatalf("bookmark %d = %+v", i, items[i])
		}
	}
	for _, ids := range [][3]int64{{first, owner.mediaID, other.userID}, {first, other.mediaID, owner.userID}, {999999, owner.mediaID, owner.userID}} {
		if deleted, err := s.DeleteBookmark(ctx, ids[0], ids[1], ids[2]); err != nil || deleted {
			t.Fatalf("unauthorized/missing delete = %v, %v", deleted, err)
		}
	}
	if deleted, err := s.DeleteBookmark(ctx, first, owner.mediaID, owner.userID); err != nil || !deleted {
		t.Fatalf("delete own = %v, %v", deleted, err)
	}
	if items, err := s.ListBookmarks(ctx, 999999, owner.userID); err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty list = %+v, %v", items, err)
	}
	for _, b := range []*model.Bookmark{
		{UserID: 999999, MediaID: owner.mediaID, CreatedAt: now},
		{UserID: owner.userID, MediaID: 999999, CreatedAt: now},
		{UserID: owner.userID, MediaID: owner.mediaID, PositionSeconds: -1, CreatedAt: now},
	} {
		if _, err := s.CreateBookmark(ctx, b); err == nil {
			t.Fatalf("invalid foreign key/position accepted: %+v", b)
		}
	}
}

func TestBookmarkCleanup(t *testing.T) {
	for _, mode := range []string{"user cascade", "delete user without cascade", "media cascade", "orphaned user on reopen", "orphaned media on reopen"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "cleanup.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if s != nil {
					_ = s.Close()
				}
			}()
			gone, kept := seedOwned(t, s, "gone"), seedOwned(t, s, "kept")
			for _, o := range []owned{gone, kept} {
				if _, err := s.CreateBookmark(ctx, &model.Bookmark{UserID: o.userID, MediaID: o.mediaID, CreatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "user cascade":
				_, err = s.db.Exec(`DELETE FROM users WHERE id = ?`, gone.userID)
			case "media cascade":
				err = s.HardDeleteMedia(ctx, gone.mediaID)
			default:
				if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "delete user without cascade":
					err = s.DeleteUser(ctx, gone.userID)
				case "orphaned user on reopen":
					_, err = s.db.Exec(`DELETE FROM users WHERE id = ?`, gone.userID)
				case "orphaned media on reopen":
					_, err = s.db.Exec(`DELETE FROM media WHERE id = ?`, gone.mediaID)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if items, err := s.ListBookmarks(ctx, gone.mediaID, gone.userID); err != nil || len(items) != 0 {
				t.Fatalf("gone bookmarks = %+v, %v", items, err)
			}
			if items, err := s.ListBookmarks(ctx, kept.mediaID, kept.userID); err != nil || len(items) != 1 {
				t.Fatalf("kept bookmarks = %+v, %v", items, err)
			}
		})
	}
}

func TestBookmarkStoreErrors(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.CreateBookmark(ctx, &model.Bookmark{}); err == nil {
		t.Fatal("create on closed database succeeded")
	}
	if _, err := s.ListBookmarks(ctx, 1, 1); err == nil {
		t.Fatal("list on closed database succeeded")
	}
	if _, err := s.DeleteBookmark(ctx, 1, 1, 1); err == nil {
		t.Fatal("delete on closed database succeeded")
	}
}
