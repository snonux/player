package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// migratePodcastCatalogCreations keeps the quota independent of catalog
// deletion. Seed existing rows exactly once, together with table creation.
func migratePodcastCatalogCreations(db *sql.DB) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'podcast_catalog_creations'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE podcast_catalog_creations (
 id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at DATETIME NOT NULL);
CREATE INDEX idx_podcast_catalog_creations_user ON podcast_catalog_creations(user_id, created_at);
INSERT INTO podcast_catalog_creations (user_id, created_at)
 SELECT created_by, created_at FROM podcast_feeds WHERE created_by IN (SELECT id FROM users);`); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateDeferredPodcastFeeds implements PodcastCatalogRepo. Subscriptions
// themselves are the durable backlog: tombstones cancel pending creation,
// and a restart requires no queue reconstruction. One transaction serializes
// quota accounting with subscription edits and administrative removal.
func (s *SQLite) CreateDeferredPodcastFeeds(ctx context.Context, now time.Time, checkInterval int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create deferred feeds: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Entries older than the rolling hour cannot affect future decisions.
	if _, err := tx.ExecContext(ctx, `DELETE FROM podcast_catalog_creations WHERE created_at <= ?`, now.Add(-time.Hour)); err != nil {
		return fmt.Errorf("expire catalog quota entries: %w", err)
	}
	pending, err := queryPairs(ctx, tx, `SELECT CAST(s.user_id AS TEXT), s.feed_key FROM podcast_subscriptions s
WHERE s.subscribed = 1 AND s.feed_key LIKE 'feed:%'
 AND NOT EXISTS (SELECT 1 FROM podcast_feeds f WHERE f.feed_key = s.feed_key AND f.merged_into IS NULL)
 AND NOT EXISTS (SELECT 1 FROM podcast_feed_aliases a WHERE a.alias_key = s.feed_key)
ORDER BY s.subscription_ms, s.user_id, s.feed_key`)
	if err != nil {
		return err
	}
	for _, p := range pending {
		// Read the register through its normal decoder to keep metadata and
		// fetch URL validation identical to immediate catalog creation.
		var userID int64
		if _, err := fmt.Sscan(p[0], &userID); err != nil {
			return fmt.Errorf("decode deferred feed user: %w", err)
		}
		sub, err := getSubscription(ctx, tx, userID, p[1])
		if err != nil {
			return err
		}
		if err := ensureCatalogFeed(ctx, tx, userID, sub.PodcastSubscription,
			model.PodcastSyncBatch{Now: now, CheckIntervalMinutes: checkInterval}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
