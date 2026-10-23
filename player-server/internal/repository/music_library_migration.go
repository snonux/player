package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// migrateMusicLibraries adds music_libraries and sets.library_id, rebuilding
// the sets unique key as (library_id, root_path) when needed.
func migrateMusicLibraries(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS music_libraries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		root_path TEXT UNIQUE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create music_libraries: %w", err)
	}

	hasColumn := false
	rows, err := db.Query(`PRAGMA table_info(sets)`)
	if err != nil {
		return fmt.Errorf("pragma sets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "library_id" {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasColumn {
		if _, err := db.Exec(`ALTER TABLE sets ADD COLUMN library_id INTEGER REFERENCES music_libraries(id)`); err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("add sets.library_id: %w", err)
		}
	}

	var libCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_libraries`).Scan(&libCount); err != nil {
		return err
	}
	if libCount == 0 {
		if _, err := db.Exec(
			`INSERT INTO music_libraries (name, root_path, created_at) VALUES (?, ?, ?)`,
			"Library", "", time.Now().UTC(),
		); err != nil {
			return fmt.Errorf("seed default music library: %w", err)
		}
	}
	var defaultID int64
	if err := db.QueryRow(`SELECT id FROM music_libraries ORDER BY id LIMIT 1`).Scan(&defaultID); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE sets SET library_id = ? WHERE library_id IS NULL OR library_id = 0`, defaultID); err != nil {
		return fmt.Errorf("backfill sets.library_id: %w", err)
	}

	return rebuildSetsLibraryUnique(db)
}

func rebuildSetsLibraryUnique(db *sql.DB) error {
	var sqlText string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='sets'`).Scan(&sqlText)
	if err != nil {
		return fmt.Errorf("read sets ddl: %w", err)
	}
	upper := strings.ToUpper(sqlText)
	if strings.Contains(upper, "UNIQUE(LIBRARY_ID, ROOT_PATH)") || strings.Contains(upper, "UNIQUE (LIBRARY_ID, ROOT_PATH)") {
		return nil
	}
	// Old schema used UNIQUE(root_path) alone. Rebuild so two libraries may
	// share the same relative set name. PRAGMA foreign_keys cannot change
	// inside a multi-statement transaction, so disable it before BEGIN and
	// restore afterward — otherwise DROP TABLE sets would CASCADE media.
	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() { _, _ = db.Exec(`PRAGMA foreign_keys = ON`) }()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`CREATE TABLE sets_library_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		library_id INTEGER NOT NULL REFERENCES music_libraries(id),
		name TEXT NOT NULL,
		root_path TEXT NOT NULL,
		cover_thumbnail_path TEXT,
		is_podcast INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(library_id, root_path)
	)`); err != nil {
		return fmt.Errorf("create sets_library_new: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO sets_library_new (id, library_id, name, root_path, cover_thumbnail_path, is_podcast, created_at)
		SELECT id, library_id, name, root_path, cover_thumbnail_path, COALESCE(is_podcast, 0), created_at FROM sets`); err != nil {
		return fmt.Errorf("copy sets: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE sets`); err != nil {
		return fmt.Errorf("drop sets: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE sets_library_new RENAME TO sets`); err != nil {
		return fmt.Errorf("rename sets: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if fk != 0 {
		if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
			return err
		}
	}
	return nil
}
