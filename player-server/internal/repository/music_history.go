package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/snonux/player/internal/model"
)

// MusicHistoryRepo owns the erasable projection; qualification rows remain the
// idempotency authority and personal counters even after history is cleared.
type MusicHistoryRepo interface {
	MusicHistorySettings(context.Context, int64) (model.MusicHistorySettings, error)
	SetMusicHistorySettings(context.Context, int64, model.MusicHistorySettings) error
	ClearMusicHistory(context.Context, int64) error
	ListMusicHistory(context.Context, int64, int64, int64, int) ([]model.MusicHistoryEvent, error)
}

const musicHistorySchema = `
CREATE TABLE IF NOT EXISTS music_history_settings (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 recording INTEGER NOT NULL DEFAULT 1,
 retention_days INTEGER NOT NULL DEFAULT 365,
 cutoff_ms INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS music_history_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 played_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_music_history_user ON music_history_events(user_id,played_ms DESC,id DESC);
CREATE TRIGGER IF NOT EXISTS music_history_insert AFTER INSERT ON music_listening_sessions
 WHEN NEW.qualified_ms IS NOT NULL BEGIN
 INSERT INTO music_history_events(user_id,media_id,played_ms)
 SELECT NEW.user_id,NEW.media_id,NEW.qualified_ms
 WHERE COALESCE((SELECT recording FROM music_history_settings WHERE user_id=NEW.user_id),1)=1
 AND NEW.qualified_ms>COALESCE((SELECT cutoff_ms FROM music_history_settings WHERE user_id=NEW.user_id),0);
END;
CREATE TRIGGER IF NOT EXISTS music_history_qualify AFTER UPDATE OF qualified_ms ON music_listening_sessions
 WHEN OLD.qualified_ms IS NULL AND NEW.qualified_ms IS NOT NULL BEGIN
 INSERT INTO music_history_events(user_id,media_id,played_ms)
 SELECT NEW.user_id,NEW.media_id,NEW.qualified_ms
 WHERE COALESCE((SELECT recording FROM music_history_settings WHERE user_id=NEW.user_id),1)=1
 AND NEW.qualified_ms>COALESCE((SELECT cutoff_ms FROM music_history_settings WHERE user_id=NEW.user_id),0);
END;
CREATE TRIGGER IF NOT EXISTS music_history_retention AFTER INSERT ON music_history_events BEGIN
 DELETE FROM music_history_events WHERE user_id=NEW.user_id
 AND COALESCE((SELECT retention_days FROM music_history_settings WHERE user_id=NEW.user_id),365)>0
 AND played_ms<=CAST(strftime('%s','now') AS INTEGER)*1000-COALESCE((SELECT retention_days FROM music_history_settings WHERE user_id=NEW.user_id),365)*86400000;
END;
`

// initializeMusicHistory installs a projection for future qualifications. Existing
// counters are deliberately not backfilled, so restart never recreates erased plays.
func initializeMusicHistory(db *sql.DB) error {
	if _, err := db.Exec(musicHistorySchema); err != nil {
		return fmt.Errorf("initialize music history: %w", err)
	}
	_, err := db.Exec(`DELETE FROM music_history_events WHERE played_ms<=CAST(strftime('%s','now') AS INTEGER)*1000-COALESCE((SELECT retention_days FROM music_history_settings s WHERE s.user_id=music_history_events.user_id),365)*86400000 AND COALESCE((SELECT retention_days FROM music_history_settings s WHERE s.user_id=music_history_events.user_id),365)>0`)
	return err
}

// MusicHistorySettings returns account settings with a one-year default.
func (s *SQLite) MusicHistorySettings(ctx context.Context, user int64) (model.MusicHistorySettings, error) {
	result := model.MusicHistorySettings{Recording: true, RetentionDays: 365}
	err := s.db.QueryRowContext(ctx, `SELECT recording,retention_days FROM music_history_settings WHERE user_id=?`, user).Scan(&result.Recording, &result.RetentionDays)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

// SetMusicHistorySettings saves retention and recording atomically with pruning.
// A monotonic cutoff prevents old offline plays appearing after re-enabling.
func (s *SQLite) SetMusicHistorySettings(ctx context.Context, user int64, value model.MusicHistorySettings) error {
	if value.RetentionDays != 0 && value.RetentionDays != 30 && value.RetentionDays != 90 && value.RetentionDays != 365 {
		return fmt.Errorf("invalid history retention")
	}
	return s.editMusicHistory(ctx, user, &value, false)
}

// ClearMusicHistory erases history while keeping retry identities and counters.
func (s *SQLite) ClearMusicHistory(ctx context.Context, user int64) error {
	return s.editMusicHistory(ctx, user, nil, true)
}

func (s *SQLite) editMusicHistory(ctx context.Context, user int64, value *model.MusicHistorySettings, clear bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO music_history_settings(user_id) VALUES(?) ON CONFLICT DO NOTHING`, user); err != nil {
		return err
	}
	now := s.now().UnixMilli()
	if value != nil {
		// Advancing the barrier on either transition also rejects packets played
		// while recording was paused but delivered after it was resumed.
		_, err = tx.ExecContext(ctx, `UPDATE music_history_settings SET cutoff_ms=CASE WHEN recording!=? THEN MAX(cutoff_ms,?) ELSE cutoff_ms END, recording=?,retention_days=? WHERE user_id=?`, value.Recording, now, value.Recording, value.RetentionDays, user)
		if err != nil {
			return err
		}
	}
	if clear {
		if _, err = tx.ExecContext(ctx, `UPDATE music_history_settings SET cutoff_ms=MAX(cutoff_ms,?) WHERE user_id=?`, now, user); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM music_history_events WHERE user_id=?`, user)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM music_history_events WHERE user_id=? AND (SELECT retention_days FROM music_history_settings WHERE user_id=?)>0 AND played_ms<=?-(SELECT retention_days FROM music_history_settings WHERE user_id=?)*86400000`, user, user, now, user)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ListMusicHistory returns newest qualifications first. ACLs are enforced in
// SQL before pagination, so revoked songs cannot leak titles or exhaust pages.
func (s *SQLite) ListMusicHistory(ctx context.Context, user, beforeMS, beforeID int64, limit int) ([]model.MusicHistoryEvent, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM music_history_events WHERE user_id=? AND COALESCE((SELECT retention_days FROM music_history_settings WHERE user_id=?),365)>0 AND played_ms<=?-COALESCE((SELECT retention_days FROM music_history_settings WHERE user_id=?),365)*86400000`, user, user, s.now().UnixMilli(), user); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.media_id,e.played_ms FROM music_history_events e JOIN media m ON m.id=e.media_id JOIN users u ON u.id=e.user_id
 WHERE e.user_id=? AND m.deleted_at IS NULL AND (?=0 OR e.played_ms<? OR (e.played_ms=? AND e.id<?))
 AND (u.is_admin=1 OR EXISTS(SELECT 1 FROM set_permissions p WHERE p.user_id=e.user_id AND p.set_id=m.set_id))
 AND (COALESCE((SELECT retention_days FROM music_history_settings WHERE user_id=e.user_id),365)=0 OR e.played_ms>?-COALESCE((SELECT retention_days FROM music_history_settings WHERE user_id=e.user_id),365)*86400000)
 ORDER BY e.played_ms DESC,e.id DESC LIMIT ?`, user, beforeMS, beforeMS, beforeMS, beforeID, s.now().UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []model.MusicHistoryEvent{}
	for rows.Next() {
		var row model.MusicHistoryEvent
		var ms int64
		if err := rows.Scan(&row.ID, &row.MediaID, &ms); err != nil {
			return nil, err
		}
		row.PlayedAt = time.UnixMilli(ms).UTC()
		result = append(result, row)
	}
	return result, rows.Err()
}

var _ MusicHistoryRepo = (*SQLite)(nil)
