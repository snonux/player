package repository

import (
	"context"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// MusicListeningRepo reads personal counters; callers must apply current ACLs.
type MusicListeningRepo interface {
	ListMusicListening(context.Context, int64) (map[int64]model.ListeningStats, error)
}

// MusicProgressStore updates personal qualification inside the progress transaction.
type MusicProgressStore interface {
	ApplyMusicProgress(context.Context, int64, string, int64, float64, float64, time.Time) error
}

// ApplyMusicProgress retains independent per-account/session qualification, using
// the existing 12s per-save cap and 60s threshold. Retained qualified rows make
// offline retries idempotent even after logout or a manual progress reset.
func (s *progressTxStore) ApplyMusicProgress(ctx context.Context, user int64, session string, media int64, position, initial float64, observed time.Time) error {
	_, err := s.tx.ExecContext(ctx, `INSERT INTO music_listening_sessions
 (user_id,session_id,media_id,last_position,accumulated_seconds,observed_ms,qualified_ms)
 SELECT ?,?,?,?,MIN(MAX(?-?,0),12),?,NULL
 FROM media m JOIN sets s ON s.id=m.set_id
 WHERE m.id=? AND m.type='audio' AND s.is_podcast=0 AND LOWER(m.file_name) NOT LIKE '%.m4b'
 ON CONFLICT(user_id,session_id,media_id) DO UPDATE SET
 accumulated_seconds=music_listening_sessions.accumulated_seconds+MIN(MAX(excluded.last_position-music_listening_sessions.last_position,0),12),
 last_position=excluded.last_position, observed_ms=excluded.observed_ms
 WHERE excluded.observed_ms>music_listening_sessions.observed_ms`, user, session, media, position, position, initial, observed.UnixMilli(), media)
	if err != nil {
		return fmt.Errorf("update listening session: %w", err)
	}
	_, err = s.tx.ExecContext(ctx, `UPDATE music_listening_sessions SET qualified_ms=observed_ms
 WHERE user_id=? AND session_id=? AND media_id=? AND accumulated_seconds>=60 AND qualified_ms IS NULL`, user, session, media)
	if err != nil {
		return fmt.Errorf("qualify listening session: %w", err)
	}
	return nil
}

// ListMusicListening returns qualified plays without projecting resume timestamps.
func (s *SQLite) ListMusicListening(ctx context.Context, user int64) (map[int64]model.ListeningStats, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT media_id,COUNT(*),MAX(qualified_ms) FROM music_listening_sessions WHERE user_id=? AND qualified_ms IS NOT NULL GROUP BY media_id`, user)
	if err != nil {
		return nil, fmt.Errorf("list music listening: %w", err)
	}
	defer rows.Close()
	result := map[int64]model.ListeningStats{}
	for rows.Next() {
		var id, millis int64
		var stats model.ListeningStats
		if err := rows.Scan(&id, &stats.PersonalPlayCount, &millis); err != nil {
			return nil, err
		}
		date := time.UnixMilli(millis).UTC()
		stats.LastPlayedAt = &date
		result[id] = stats
	}
	return result, rows.Err()
}

var _ MusicListeningRepo = (*SQLite)(nil)
var _ MusicProgressStore = (*progressTxStore)(nil)

// mergeMusicPlay never derives a play from a resume position. Client event IDs
// survive request retries; user/media keys prevent cross-account collisions.
func mergeMusicPlay(ctx context.Context, db sqlExecer, user, media int64, play model.SyncedMusicPlay) error {
	_, err := db.ExecContext(ctx, `INSERT INTO music_listening_sessions(user_id,session_id,media_id,last_position,accumulated_seconds,observed_ms,qualified_ms)
    SELECT ?,?,?,0,60,?,? FROM media m JOIN sets s ON s.id=m.set_id
    WHERE m.id=? AND m.type='audio' AND s.is_podcast=0 AND LOWER(m.file_name) NOT LIKE '%.m4b'
    ON CONFLICT(user_id,session_id,media_id) DO NOTHING`, user, "offline:"+play.ID, media, play.ObservedMS, play.ObservedMS, media)
	return err
}
