package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// UpsertProgress inserts or replaces playback progress.
func (s *SQLite) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	return upsertProgress(ctx, s.db, progress)
}

func upsertProgress(ctx context.Context, db sqlExecer, progress *model.PlaybackProgress) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO playback_progress (user_id, media_id, position_seconds, accumulated_seconds, finished, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, media_id) DO UPDATE SET
		 position_seconds = excluded.position_seconds,
		 accumulated_seconds = CASE WHEN excluded.finished THEN 0 ELSE MAX(playback_progress.accumulated_seconds, excluded.accumulated_seconds) END,
		 finished = excluded.finished,
		 updated_at = excluded.updated_at`,
		progress.UserID, progress.MediaID, progress.PositionSeconds, progress.AccumulatedSeconds, progress.Finished, progress.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}
	return nil
}

// GetProgress retrieves playback progress for a user and media.
func (s *SQLite) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	return getProgress(ctx, s.db, userID, mediaID)
}

func getProgress(ctx context.Context, db sqlQueryRower, userID, mediaID int64) (*model.PlaybackProgress, error) {
	row := db.QueryRowContext(ctx,
		`SELECT user_id, media_id, position_seconds, accumulated_seconds, finished, updated_at FROM playback_progress WHERE user_id = ? AND media_id = ?`,
		userID, mediaID,
	)
	var p model.PlaybackProgress
	if err := row.Scan(&p.UserID, &p.MediaID, &p.PositionSeconds, &p.AccumulatedSeconds, &p.Finished, &p.UpdatedAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteProgress removes playback progress for a user and media.
func (s *SQLite) DeleteProgress(ctx context.Context, userID, mediaID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM playback_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID)
	if err != nil {
		return fmt.Errorf("delete progress: %w", err)
	}
	return nil
}

// ResetProgress removes one user's progress and transient playback counters.
func (s *SQLite) ResetProgress(ctx context.Context, userID, mediaID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reset progress: %w", err)
	}
	if err := deleteUserAccumulators(ctx, tx, userID, mediaID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playback_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete user progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reset progress: %w", err)
	}
	return nil
}

// FinishProgress saves a finished position and starts a fresh playback cycle.
func (s *SQLite) FinishProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish progress: %w", err)
	}
	if err := deleteUserAccumulators(ctx, tx, progress.UserID, progress.MediaID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := upsertProgress(ctx, tx, progress); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("finish progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finish progress: %w", err)
	}
	return nil
}

func deleteUserAccumulators(ctx context.Context, db sqlExecer, userID, mediaID int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM playback_accumulator
WHERE media_id = ? AND (
    session_id IN (SELECT id FROM sessions WHERE user_id = ?)
    OR session_id IN (SELECT 'api-token:' || id FROM api_tokens WHERE user_id = ?)
)`, mediaID, userID, userID)
	if err != nil {
		return fmt.Errorf("delete user accumulators: %w", err)
	}
	return nil
}

// MarkFinished marks playback progress finished for a user and media.
func (s *SQLite) MarkFinished(ctx context.Context, userID, mediaID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE playback_progress SET finished = 1, accumulated_seconds = 0, updated_at = CURRENT_TIMESTAMP WHERE user_id = ? AND media_id = ?`,
		userID, mediaID,
	)
	if err != nil {
		return fmt.Errorf("mark finished: %w", err)
	}
	return nil
}

// ListProgressByUser returns all progress records for a user.
func (s *SQLite) ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, media_id, position_seconds, accumulated_seconds, finished, updated_at FROM playback_progress WHERE user_id = ? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list progress: %w", err)
	}
	defer rows.Close()
	var pp []model.PlaybackProgress
	for rows.Next() {
		var p model.PlaybackProgress
		if err := rows.Scan(&p.UserID, &p.MediaID, &p.PositionSeconds, &p.AccumulatedSeconds, &p.Finished, &p.UpdatedAt); err != nil {
			return nil, err
		}
		pp = append(pp, p)
	}
	return pp, rows.Err()
}

// ListInProgressMedia returns unfinished, non-deleted media with at least 60s
// accumulated playback, most recently played first. Each item carries the
// user's saved position so clients can show progress and resume directly.
func (s *SQLite) ListInProgressMedia(ctx context.Context, userID int64, filter MediaFilter) ([]model.Media, error) {
	args := []any{userID}
	conds := []string{
		`pp.user_id = ?`,
		`pp.finished = 0`,
		`media.deleted_at IS NULL`,
		`pp.accumulated_seconds >= 60`,
	}
	query := `SELECT media.id, media.set_id, media.rel_path, media.file_name, media.abs_path, media.type, media.duration, media.codec, media.resolution, media.bitrate, media.file_size_bytes, media.width, media.height, media.exif_camera, media.exif_lens, media.exif_date, media.exif_iso, media.exif_f_number, media.exif_exposure, media.exif_focal_length, media.thumbnail_path, media.play_count, media.deleted_at, media.created_at, pp.position_seconds FROM playback_progress pp INNER JOIN media ON media.id = pp.media_id`

	if len(filter.AllowedSetIDs) > 0 {
		conds = append(conds, "media.set_id IN ("+placeholders(len(filter.AllowedSetIDs))+")")
		for _, id := range filter.AllowedSetIDs {
			args = append(args, id)
		}
	}

	query += " WHERE " + strings.Join(conds, " AND ")
	query += " ORDER BY pp.updated_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list in-progress media: %w", err)
	}
	defer rows.Close()

	var media []model.Media
	for rows.Next() {
		var position float64
		m, err := scanMedia(rows, &position)
		if err != nil {
			return nil, err
		}
		m.PositionSeconds = &position
		media = append(media, *m)
	}
	return media, rows.Err()
}
