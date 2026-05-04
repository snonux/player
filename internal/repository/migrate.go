package repository

import (
	"database/sql"
	"fmt"
)

// tablesSchema defines all CREATE TABLE statements.
const tablesSchema = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    root_path TEXT UNIQUE NOT NULL,
    cover_thumbnail_path TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS set_permissions (
    set_id INTEGER NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT CHECK(role IN ('owner','viewer')) NOT NULL DEFAULT 'viewer',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (set_id, user_id)
);

CREATE TABLE IF NOT EXISTS media (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    set_id INTEGER NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    rel_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    abs_path TEXT NOT NULL,
    type TEXT CHECK(type IN ('video','audio','image')) NOT NULL,
    duration REAL,
    codec TEXT,
    resolution TEXT,
    bitrate INTEGER,
    file_size_bytes INTEGER,
    width INTEGER,
    height INTEGER,
    exif_camera TEXT,
    exif_lens TEXT,
    exif_date TEXT,
    exif_iso TEXT,
    exif_f_number TEXT,
    exif_exposure TEXT,
    exif_focal_length TEXT,
    thumbnail_path TEXT,
    play_count INTEGER NOT NULL DEFAULT 0,
    deleted_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(set_id, rel_path)
);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS media_tags (
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (media_id, tag_id)
);

CREATE TABLE IF NOT EXISTS favorites (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE IF NOT EXISTS playback_progress (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    position_seconds REAL NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS playback_accumulator (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    last_position REAL NOT NULL DEFAULT 0,
    accumulated_seconds REAL NOT NULL DEFAULT 0,
    counted INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, media_id)
);

CREATE TABLE IF NOT EXISTS shares (
    token TEXT PRIMARY KEY,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    max_uses INTEGER,
    used_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS media_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(media_id, user_id)
);
`

// indexesSchema defines all CREATE INDEX statements.
const indexesSchema = `
CREATE INDEX IF NOT EXISTS idx_media_set_id ON media(set_id);
CREATE INDEX IF NOT EXISTS idx_media_rel_path ON media(set_id, rel_path);
CREATE INDEX IF NOT EXISTS idx_media_deleted_at ON media(deleted_at);
CREATE INDEX IF NOT EXISTS idx_media_type ON media(type);
CREATE INDEX IF NOT EXISTS idx_media_filename ON media(file_name);
CREATE INDEX IF NOT EXISTS idx_permissions_user ON set_permissions(user_id);
CREATE INDEX IF NOT EXISTS idx_permissions_set ON set_permissions(set_id);
CREATE INDEX IF NOT EXISTS idx_shares_expires ON shares(expires_at);
`

// execSchema executes a raw SQL schema block against the given database.
func execSchema(db *sql.DB, name, schema string) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("execute %s schema: %w", name, err)
	}
	return nil
}

// enableForeignKeys turns on SQLite foreign key enforcement.
func enableForeignKeys(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	return nil
}

// Migrate creates the database schema if it does not exist.
func Migrate(db *sql.DB) error {
	if err := enableForeignKeys(db); err != nil {
		return err
	}
	if err := execSchema(db, "tables", tablesSchema); err != nil {
		return err
	}
	if err := execSchema(db, "indexes", indexesSchema); err != nil {
		return err
	}
	return nil
}
