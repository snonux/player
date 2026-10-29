package repository

import "database/sql"

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
