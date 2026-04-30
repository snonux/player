package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/paul/kiss-media-player/internal/model"
)

// UpsertProgress inserts or replaces playback progress.
func (s *SQLite) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO playback_progress (user_id, media_id, position_seconds, updated_at) VALUES (?, ?, ?, ?)`,
		progress.UserID, progress.MediaID, progress.PositionSeconds, progress.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}
	return nil
}

// GetProgress retrieves playback progress for a user and media.
func (s *SQLite) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT user_id, media_id, position_seconds, updated_at FROM playback_progress WHERE user_id = ? AND media_id = ?`,
		userID, mediaID,
	)
	var p model.PlaybackProgress
	if err := row.Scan(&p.UserID, &p.MediaID, &p.PositionSeconds, &p.UpdatedAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProgressByUser returns all progress records for a user.
func (s *SQLite) ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, media_id, position_seconds, updated_at FROM playback_progress WHERE user_id = ? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list progress: %w", err)
	}
	defer rows.Close()
	var pp []model.PlaybackProgress
	for rows.Next() {
		var p model.PlaybackProgress
		if err := rows.Scan(&p.UserID, &p.MediaID, &p.PositionSeconds, &p.UpdatedAt); err != nil {
			return nil, err
		}
		pp = append(pp, p)
	}
	return pp, rows.Err()
}
