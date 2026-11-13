package repository

// Schema steps that bring an older database's podcast tables to their final
// shape. Podcasts are no longer a feature and nothing reads or writes these
// tables, but upgrades still run the steps so existing data stays intact and
// later steps (such as the indexes) find the columns they expect.

import (
	"context"
	"database/sql"
)

// Deletions are independent per-account tombstones, retained across feed refresh.
func migratePodcastEpisodeDeletion(db *sql.DB) error {
	exists, err := hasColumn(db, "podcast_episode_state", "user_id")
	if err != nil || !exists {
		return err
	}
	for _, c := range []struct{ name, definition string }{{"deleted", "INTEGER NOT NULL DEFAULT 0"}, {"deletion_ms", "INTEGER NOT NULL DEFAULT 0"}} {
		exists, err := hasColumn(db, "podcast_episode_state", c.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err = db.Exec("ALTER TABLE podcast_episode_state ADD COLUMN " + c.name + " " + c.definition); err != nil {
				return err
			}
		}
	}
	return nil
}

// migratePodcastRetention adds the retention columns the final schema has.
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

// Run after the legacy episode-table rebuild. Clearing conditional validators
// once lets ordinary refresh enrich existing episodes without changing identity.
func migratePodcastEpisodeArtwork(db *sql.DB) error {
	done, err := hasColumn(db, "podcast_episodes", "image_url")
	if err != nil || done {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`ALTER TABLE podcast_episodes ADD COLUMN image_url TEXT NOT NULL DEFAULT ''; UPDATE podcast_feeds SET last_etag=NULL;`); err != nil {
		return err
	}
	return tx.Commit()
}

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
