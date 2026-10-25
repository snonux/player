package repository

import (
	"database/sql"
	"fmt"
)

// migrateCollectionShares removes the required single-media anchor while
// retaining every legacy token, owner, timestamp and viewing count atomically.
func migrateCollectionShares(db *sql.DB) error {
	var required int
	if err := db.QueryRow(`SELECT "notnull" FROM pragma_table_info('shares') WHERE name='media_id'`).Scan(&required); err != nil {
		return err
	}
	if required == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`CREATE TABLE shares_collection_new (
 token TEXT PRIMARY KEY,
 media_id INTEGER REFERENCES media(id) ON DELETE CASCADE,
 created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
 expires_at DATETIME NOT NULL,
 max_uses INTEGER,
 used_count INTEGER NOT NULL DEFAULT 0,
 collection_title TEXT NOT NULL DEFAULT '',
 media_ids TEXT NOT NULL DEFAULT '[]',
 description TEXT NOT NULL DEFAULT '',
 audio_quality TEXT NOT NULL DEFAULT ''
 );
 INSERT INTO shares_collection_new SELECT token,media_id,created_by,created_at,expires_at,max_uses,used_count,collection_title,media_ids,description,audio_quality FROM shares;
 DROP TABLE shares;
 ALTER TABLE shares_collection_new RENAME TO shares;`)
	if err != nil {
		return fmt.Errorf("rebuild shares: %w", err)
	}
	return tx.Commit()
}
