package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// CreateBookmark saves an additional position without replacing resume progress.
func (s *SQLite) CreateBookmark(ctx context.Context, b *model.Bookmark) (int64, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO media_bookmarks
		(user_id, media_id, position_seconds, label, created_at, sync_key, updated_ms) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		b.UserID, b.MediaID, b.PositionSeconds, b.Label, b.CreatedAt, hex.EncodeToString(key), b.CreatedAt.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("create bookmark: %w", err)
	}
	return res.LastInsertId()
}

// ListBookmarks returns only this user's positions in stable playback order.
func (s *SQLite) ListBookmarks(ctx context.Context, mediaID, userID int64) ([]model.Bookmark, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, media_id, user_id, position_seconds, label, created_at
		FROM media_bookmarks WHERE media_id = ? AND user_id = ? AND deleted = 0 ORDER BY position_seconds, id`, mediaID, userID)
	if err != nil {
		return nil, fmt.Errorf("list bookmarks: %w", err)
	}
	defer rows.Close()
	bookmarks := []model.Bookmark{}
	for rows.Next() {
		var b model.Bookmark
		if err := rows.Scan(&b.ID, &b.MediaID, &b.UserID, &b.PositionSeconds, &b.Label, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan bookmark: %w", err)
		}
		bookmarks = append(bookmarks, b)
	}
	return bookmarks, rows.Err()
}

// DeleteBookmark requires both ownership and the matching media item.
func (s *SQLite) DeleteBookmark(ctx context.Context, id, mediaID, userID int64) (bool, error) {
	return s.DeleteBookmarkAt(ctx, id, mediaID, userID, time.Now())
}

func (s *SQLite) DeleteBookmarkAt(ctx context.Context, id, mediaID, userID int64, observed time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE media_bookmarks SET
 deleted = CASE WHEN updated_ms <= ? THEN 1 ELSE deleted END,
 updated_ms = CASE WHEN updated_ms <= ? THEN MAX(updated_ms+1,?) ELSE updated_ms END
 WHERE id = ? AND media_id = ? AND user_id = ? AND deleted = 0`, observed.UnixMilli(), observed.UnixMilli(), observed.UnixMilli(), id, mediaID, userID)
	if err != nil {
		return false, fmt.Errorf("delete bookmark: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
