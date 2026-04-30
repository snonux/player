package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/paul/kiss-media-player/internal/model"
)

// UpsertAccumulator inserts or replaces a playback accumulator.
func (s *SQLite) UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO playback_accumulator (session_id, media_id, last_position, accumulated_seconds, counted, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		acc.SessionID, acc.MediaID, acc.LastPosition, acc.AccumulatedSeconds, boolToInt(acc.Counted), acc.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert accumulator: %w", err)
	}
	return nil
}

// GetAccumulator retrieves a playback accumulator by session and media.
func (s *SQLite) GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, media_id, last_position, accumulated_seconds, counted, updated_at FROM playback_accumulator WHERE session_id = ? AND media_id = ?`,
		sessionID, mediaID,
	)
	var a model.PlaybackAccumulator
	var counted int
	if err := row.Scan(&a.SessionID, &a.MediaID, &a.LastPosition, &a.AccumulatedSeconds, &counted, &a.UpdatedAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	a.Counted = counted != 0
	return &a, nil
}
