package repository

import (
	"context"
	"database/sql"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// latestProgressTime returns the latest edit time of ordinary media's
// progress register: the stored progress or a later reset.
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

// ProgressEditTime returns the time a server-side status edit (finish,
// reset) of the media must not tie with: the latest progress or reset
// time, or for an episode copy the later of its registers' clocks.
func (s *SQLite) ProgressEditTime(ctx context.Context, userID, mediaID int64) (time.Time, error) {
	return editTime(ctx, s.db, userID, mediaID, false)
}

// ProgressEditTime implements the same on the progress transaction.
func (s *progressTxStore) ProgressEditTime(ctx context.Context, userID, mediaID int64) (time.Time, error) {
	return editTime(ctx, s.tx, userID, mediaID, false)
}

// PositionEditTime returns the time a position save of the media must be
// later than: like ProgressEditTime for ordinary media, the position
// register's clock for an episode copy (a later completion does not block
// a position save, contract §4 rule 5).
func (s *SQLite) PositionEditTime(ctx context.Context, userID, mediaID int64) (time.Time, error) {
	return editTime(ctx, s.db, userID, mediaID, true)
}

// PositionEditTime implements the same on the progress transaction.
func (s *progressTxStore) PositionEditTime(ctx context.Context, userID, mediaID int64) (time.Time, error) {
	return editTime(ctx, s.tx, userID, mediaID, true)
}

// SyncPlayback merges one item's edits and reads the result in one transaction.
// No mutation is stamped with reconnect time, including tombstones. The
// progress of an episode copy merges into the episode registers, and the
// answer is their projection (contract §7.1); bookmarks stay media-scoped.
func (s *SQLite) SyncPlayback(ctx context.Context, userID, mediaID int64, incoming model.PlaybackSync) (*model.PlaybackSync, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	k, linked, err := linkedEpisode(ctx, tx, userID, mediaID)
	if err != nil {
		return nil, err
	}
	if p := incoming.Progress; p != nil {
		if linked {
			err = syncLinkedProgress(ctx, tx, k, mediaID, p, s.now())
		} else {
			err = syncMediaProgress(ctx, tx, userID, mediaID, p)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := mergeSyncedBookmarks(ctx, tx, userID, mediaID, incoming.Bookmarks); err != nil {
		return nil, err
	}
	result := &model.PlaybackSync{}
	if linked {
		result.Progress, err = linkedSyncResult(ctx, tx, k, mediaID)
	} else {
		result.Progress, err = mediaSyncResult(ctx, tx, userID, mediaID)
	}
	if err != nil {
		return nil, err
	}
	if result.Bookmarks, err = listSyncedBookmarks(ctx, tx, userID, mediaID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// syncMediaProgress merges the progress register of ordinary media: the
// edit wins only when it is later than every stored progress and reset.
func syncMediaProgress(ctx context.Context, tx *sql.Tx, userID, mediaID int64, p *model.SyncedProgress) error {
	latest, err := latestProgressTime(ctx, tx, userID, mediaID)
	if err != nil {
		return err
	}
	observed := time.UnixMilli(p.ObservedMS).UTC()
	if !observed.After(latest) {
		return nil
	}
	old, err := getProgress(ctx, tx, userID, mediaID)
	if err != nil {
		return err
	}
	var accumulated float64
	if old != nil {
		accumulated = old.AccumulatedSeconds
	}
	if err := upsertProgress(ctx, tx, &model.PlaybackProgress{UserID: userID, MediaID: mediaID, PositionSeconds: p.PositionSeconds, Finished: p.Finished, AccumulatedSeconds: accumulated, UpdatedAt: observed}); err != nil {
		return err
	}
	if p.Finished {
		return deleteUserAccumulators(ctx, tx, userID, mediaID)
	}
	return nil
}

// mergeSyncedBookmarks merges bookmark registers by their edit clocks.
func mergeSyncedBookmarks(ctx context.Context, tx *sql.Tx, userID, mediaID int64, bookmarks []model.SyncedBookmark) error {
	for _, b := range bookmarks {
		_, err := tx.ExecContext(ctx, `INSERT INTO media_bookmarks(user_id,media_id,position_seconds,label,created_at,sync_key,updated_ms,deleted)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,media_id,sync_key) DO UPDATE SET position_seconds=excluded.position_seconds,label=excluded.label,updated_ms=excluded.updated_ms,deleted=excluded.deleted WHERE excluded.updated_ms > media_bookmarks.updated_ms`, userID, mediaID, b.PositionSeconds, b.Label, time.UnixMilli(b.ObservedMS).UTC(), b.Key, b.ObservedMS, b.Deleted)
		if err != nil {
			return err
		}
	}
	return nil
}

// mediaSyncResult is the stored progress register of ordinary media. A
// reset later than the stored progress answers its clock with position 0.
func mediaSyncResult(ctx context.Context, tx *sql.Tx, userID, mediaID int64) (*model.SyncedProgress, error) {
	p, err := getProgress(ctx, tx, userID, mediaID)
	if err != nil {
		return nil, err
	}
	latest, err := latestProgressTime(ctx, tx, userID, mediaID)
	if err != nil || latest.IsZero() {
		return nil, err
	}
	result := &model.SyncedProgress{ObservedMS: latest.UnixMilli()}
	if p != nil && !p.UpdatedAt.Before(latest) {
		result.PositionSeconds = p.PositionSeconds
		result.Finished = p.Finished
	}
	return result, nil
}

// listSyncedBookmarks returns every bookmark register of the media,
// tombstones included.
func listSyncedBookmarks(ctx context.Context, tx *sql.Tx, userID, mediaID int64) ([]model.SyncedBookmark, error) {
	rows, err := tx.QueryContext(ctx, `SELECT sync_key,position_seconds,label,created_at,updated_ms,deleted FROM media_bookmarks WHERE user_id=? AND media_id=? ORDER BY id`, userID, mediaID)
	if err != nil {
		return nil, err
	}
	bookmarks := []model.SyncedBookmark{}
	for rows.Next() {
		var b model.SyncedBookmark
		var created time.Time
		if err := rows.Scan(&b.Key, &b.PositionSeconds, &b.Label, &created, &b.ObservedMS, &b.Deleted); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if b.ObservedMS == 0 {
			b.ObservedMS = created.UnixMilli()
		}
		bookmarks = append(bookmarks, b)
	}
	return bookmarks, closeRows(rows)
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
