package repository

import (
	"context"
	"database/sql"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func latestProgressTime(ctx context.Context, db sqlQueryRower, userID, mediaID int64) (time.Time, error) {
	p, err := getProgress(ctx, db, userID, mediaID)
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	if p != nil {
		latest = p.UpdatedAt.Truncate(time.Millisecond)
	}
	var ms int64
	err = db.QueryRowContext(ctx, `SELECT observed_ms FROM playback_resets WHERE user_id=? AND media_id=?`, userID, mediaID).Scan(&ms)
	if err != nil && err != sql.ErrNoRows {
		return latest, err
	}
	if err == nil && time.UnixMilli(ms).After(latest) {
		latest = time.UnixMilli(ms)
	}
	return latest, nil
}
func (s *SQLite) ProgressEditTime(ctx context.Context, userID, mediaID int64) (time.Time, error) {
	return latestProgressTime(ctx, s.db, userID, mediaID)
}
func (s *progressTxStore) ProgressEditTime(ctx context.Context, userID, mediaID int64) (time.Time, error) {
	return latestProgressTime(ctx, s.tx, userID, mediaID)
}

// SyncPlayback merges one item's edits and reads the result in one transaction.
// No mutation is stamped with reconnect time, including tombstones.
func (s *SQLite) SyncPlayback(ctx context.Context, userID, mediaID int64, incoming model.PlaybackSync) (*model.PlaybackSync, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if p := incoming.Progress; p != nil {
		latest, err := latestProgressTime(ctx, tx, userID, mediaID)
		if err != nil {
			return nil, err
		}
		observed := time.UnixMilli(p.ObservedMS).UTC()
		if observed.After(latest) {
			old, err := getProgress(ctx, tx, userID, mediaID)
			if err != nil {
				return nil, err
			}
			var accumulated float64
			if old != nil {
				accumulated = old.AccumulatedSeconds
			}
			if err := upsertProgress(ctx, tx, &model.PlaybackProgress{UserID: userID, MediaID: mediaID, PositionSeconds: p.PositionSeconds, Finished: p.Finished, AccumulatedSeconds: accumulated, UpdatedAt: observed}); err != nil {
				return nil, err
			}
			if p.Finished {
				if err := deleteUserAccumulators(ctx, tx, userID, mediaID); err != nil {
					return nil, err
				}
			}
			// Dual-write into the episode registers of a podcast copy: a
			// natural end is an automatic completion together with the
			// position of the same edit, a save the position only.
			edit := positionEdit(p.PositionSeconds, p.ObservedMS)
			if p.Finished {
				edit.completed, edit.source = &p.Finished, completionAuto
			}
			if err := recordLinkedEpisodeEdit(ctx, tx, userID, mediaID, edit); err != nil {
				return nil, err
			}
		}
	}
	for _, b := range incoming.Bookmarks {
		_, err = tx.ExecContext(ctx, `INSERT INTO media_bookmarks(user_id,media_id,position_seconds,label,created_at,sync_key,updated_ms,deleted)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,media_id,sync_key) DO UPDATE SET position_seconds=excluded.position_seconds,label=excluded.label,updated_ms=excluded.updated_ms,deleted=excluded.deleted WHERE excluded.updated_ms > media_bookmarks.updated_ms`, userID, mediaID, b.PositionSeconds, b.Label, time.UnixMilli(b.ObservedMS).UTC(), b.Key, b.ObservedMS, b.Deleted)
		if err != nil {
			return nil, err
		}
	}
	result := &model.PlaybackSync{Bookmarks: []model.SyncedBookmark{}}
	p, err := getProgress(ctx, tx, userID, mediaID)
	if err != nil {
		return nil, err
	}
	latest, err := latestProgressTime(ctx, tx, userID, mediaID)
	if err != nil {
		return nil, err
	}
	if !latest.IsZero() {
		result.Progress = &model.SyncedProgress{ObservedMS: latest.UnixMilli()}
		if p != nil && !p.UpdatedAt.Before(latest) {
			result.Progress.PositionSeconds = p.PositionSeconds
			result.Progress.Finished = p.Finished
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT sync_key,position_seconds,label,created_at,updated_ms,deleted FROM media_bookmarks WHERE user_id=? AND media_id=? ORDER BY id`, userID, mediaID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var b model.SyncedBookmark
		var created time.Time
		if err := rows.Scan(&b.Key, &b.PositionSeconds, &b.Label, &created, &b.ObservedMS, &b.Deleted); err != nil {
			rows.Close()
			return nil, err
		}
		if b.ObservedMS == 0 {
			b.ObservedMS = created.UnixMilli()
		}
		result.Bookmarks = append(result.Bookmarks, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Legacy bookmarks predate the integer edit clock. Read normalized UTC times
// with the driver so conversion never depends on SQLite's date string parser.
func backfillBookmarkSyncTimes(db *sql.DB) error {
	rows, err := db.Query(`SELECT id,created_at FROM media_bookmarks WHERE updated_ms=0`)
	if err != nil {
		return err
	}
	type stamp struct{ id, ms int64 }
	var values []stamp
	for rows.Next() {
		var id int64
		var created time.Time
		if err := rows.Scan(&id, &created); err != nil {
			rows.Close()
			return err
		}
		values = append(values, stamp{id, created.UnixMilli()})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range values {
		if _, err := db.Exec(`UPDATE media_bookmarks SET updated_ms=? WHERE id=? AND updated_ms=0`, v.ms, v.id); err != nil {
			return err
		}
	}
	return nil
}
