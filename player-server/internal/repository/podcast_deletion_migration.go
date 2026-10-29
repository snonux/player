package repository

import "database/sql"

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
