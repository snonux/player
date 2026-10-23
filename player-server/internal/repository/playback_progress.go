package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/snonux/player/internal/model"
)

// UpsertProgress inserts or replaces playback progress. For the server copy
// of a podcast episode the edit goes to the user's episode registers
// instead, and playback_progress gets their projection (see
// podcast_episode_bridge.go).
func (s *SQLite) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertProgressOrEpisode(ctx, tx, progress, s.now()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}
	return nil
}

// upsertProgressOrEpisode writes progress of ordinary media, or routes the
// edit of an episode copy through its registers: a position save is a
// position edit, a finished row a manual completion that keeps the
// position register (contract §7.1). now is the server time clamping uses.
func upsertProgressOrEpisode(ctx context.Context, tx sqlTx, progress *model.PlaybackProgress, now time.Time) error {
	k, linked, err := linkedEpisode(ctx, tx, progress.UserID, progress.MediaID)
	if err != nil {
		return err
	}
	if linked {
		return saveLinkedProgress(ctx, tx, k, progress, now)
	}
	return upsertProgress(ctx, tx, progress)
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
	return s.ResetProgressAt(ctx, userID, mediaID, s.now().UTC())
}

// ResetProgressAt clears one user's progress of a media item as of
// observed, unless a later edit exists. For an episode copy it is position
// 0 plus a manual uncompletion of the episode registers.
func (s *SQLite) ResetProgressAt(ctx context.Context, userID, mediaID int64, observed time.Time) error {
	observed = observed.Truncate(time.Millisecond)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reset progress: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	k, linked, err := linkedEpisode(ctx, tx, userID, mediaID)
	if err != nil {
		return err
	}
	if linked {
		err = resetLinkedProgress(ctx, tx, k, mediaID, observed, s.now())
	} else {
		err = resetMediaProgress(ctx, tx, userID, mediaID, observed)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reset progress: %w", err)
	}
	return nil
}

// resetMediaProgress resets ordinary media: the reset time becomes an edit
// barrier (playback_resets), and progress and session counters are removed.
func resetMediaProgress(ctx context.Context, tx *sql.Tx, userID, mediaID int64, observed time.Time) error {
	latest, err := latestProgressTime(ctx, tx, userID, mediaID)
	if err != nil || !observed.After(latest) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO playback_resets(user_id,media_id,observed_ms) VALUES (?,?,?) ON CONFLICT(user_id,media_id) DO UPDATE SET observed_ms=excluded.observed_ms`, userID, mediaID, observed.UnixMilli()); err != nil {
		return err
	}
	if err := deleteUserAccumulators(ctx, tx, userID, mediaID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playback_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID); err != nil {
		return fmt.Errorf("delete user progress: %w", err)
	}
	return nil
}

// FinishProgress saves a finished position and starts a fresh playback
// cycle. For an episode copy it is a manual completion of the episode
// registers that keeps the position register.
func (s *SQLite) FinishProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish progress: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	k, linked, err := linkedEpisode(ctx, tx, progress.UserID, progress.MediaID)
	if err != nil {
		return err
	}
	if linked {
		err = finishLinkedProgress(ctx, tx, k, progress.MediaID, progress.UpdatedAt.Truncate(time.Millisecond), s.now())
	} else {
		err = finishMediaProgress(ctx, tx, progress)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finish progress: %w", err)
	}
	return nil
}

// finishMediaProgress finishes ordinary media unless a later edit exists.
func finishMediaProgress(ctx context.Context, tx *sql.Tx, progress *model.PlaybackProgress) error {
	latest, err := latestProgressTime(ctx, tx, progress.UserID, progress.MediaID)
	if err != nil || !progress.UpdatedAt.Truncate(time.Millisecond).After(latest) {
		return err
	}
	if err := deleteUserAccumulators(ctx, tx, progress.UserID, progress.MediaID); err != nil {
		return err
	}
	if err := upsertProgress(ctx, tx, progress); err != nil {
		return fmt.Errorf("finish progress: %w", err)
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
	query := `SELECT media.id, media.set_id, media.rel_path, media.file_name, media.abs_path, media.type, media.duration, media.codec, media.resolution, media.bitrate, media.file_size_bytes, media.width, media.height, media.exif_camera, media.exif_lens, media.exif_date, media.exif_iso, media.exif_f_number, media.exif_exposure, media.exif_focal_length, media.thumbnail_path, media.play_count, media.deleted_at, media.created_at, media.music, pp.position_seconds FROM playback_progress pp INNER JOIN media ON media.id = pp.media_id`

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
