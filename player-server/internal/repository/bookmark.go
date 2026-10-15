package repository

import (
	"context"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// CreateBookmark saves an additional position without replacing resume progress.
func (s *SQLite) CreateBookmark(ctx context.Context, b *model.Bookmark) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO media_bookmarks
		(user_id, media_id, position_seconds, label, created_at) VALUES (?, ?, ?, ?, ?)`,
		b.UserID, b.MediaID, b.PositionSeconds, b.Label, b.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("create bookmark: %w", err)
	}
	return res.LastInsertId()
}

// ListBookmarks returns only this user's positions in stable playback order.
func (s *SQLite) ListBookmarks(ctx context.Context, mediaID, userID int64) ([]model.Bookmark, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, media_id, user_id, position_seconds, label, created_at
		FROM media_bookmarks WHERE media_id = ? AND user_id = ? ORDER BY position_seconds, id`, mediaID, userID)
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
	res, err := s.db.ExecContext(ctx, `DELETE FROM media_bookmarks WHERE id = ? AND media_id = ? AND user_id = ?`, id, mediaID, userID)
	if err != nil {
		return false, fmt.Errorf("delete bookmark: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
