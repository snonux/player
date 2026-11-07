package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PodcastRetentionAge is the grace period before completed episodes leave
// their account's lists. History, catalog rows and media files are preserved.
const PodcastRetentionAge = 7 * 24 * time.Hour

func migratePodcastRetention(db *sql.DB) error {
	for _, name := range []string{"retention_completion_ms", "retention_seen_ms"} {
		exists, err := hasColumn(db, "podcast_episode_state", name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec("ALTER TABLE podcast_episode_state ADD COLUMN " + name + " INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
		}
	}
	return nil
}

// RetainPodcastEpisodes sweeps all accounts at startup and on maintenance ticks.
// It creates ordinary synchronized tombstones, never shared-media deletions.
func (s *SQLite) RetainPodcastEpisodes(ctx context.Context) error {
	return s.retainPodcastEpisodes(ctx, 0)
}

func (s *SQLite) retainPodcastEpisodes(ctx context.Context, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := retainPodcastEpisodes(ctx, tx, userID, s.now()); err != nil {
		return err
	}
	return tx.Commit()
}

// Observe new completion clocks on server time rather than trusting a device's
// wall clock for age. Old offline reports and upgrade backfills receive a full
// grace period. Replay and explicit restore prevent deletion until recompletion.
func retainPodcastEpisodes(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE podcast_episode_state SET retention_completion_ms=completion_ms, retention_seen_ms=?
WHERE (?=0 OR user_id=?) AND completed=1 AND completion_ms>0 AND deleted=0 AND retention_completion_ms!=completion_ms`, now.UnixMilli(), userID, userID)
	if err != nil {
		return fmt.Errorf("observe podcast retention: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id,feed_key,episode_key FROM podcast_episode_state
WHERE (?=0 OR user_id=?) AND completed=1 AND completion_ms>0 AND deleted=0
 AND position_ms<=completion_ms AND deletion_ms<completion_ms
 AND retention_seen_ms>0 AND retention_seen_ms<=?`, userID, userID, now.Add(-PodcastRetentionAge).UnixMilli())
	if err != nil {
		return fmt.Errorf("list retained podcasts: %w", err)
	}
	var keys []episodeStateKey
	for rows.Next() {
		var k episodeStateKey
		if err := rows.Scan(&k.userID, &k.feedKey, &k.episodeKey); err != nil {
			_ = rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, k := range keys {
		r, _, err := loadRegisters(ctx, tx, k)
		if err != nil {
			return err
		}
		if _, err := mergeEpisodeEdit(ctx, tx, k, episodeEdit{}.withDeletion(true, max(now.UnixMilli(), r.deletionMS+1)), ""); err != nil {
			return err
		}
	}
	return nil
}
