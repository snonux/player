package repository

import (
	"database/sql"
	"fmt"
	"strings"
)

// tablesSchema defines all CREATE TABLE statements for a fresh database.
const tablesSchema = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    last_used_at DATETIME,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    root_path TEXT UNIQUE NOT NULL,
    cover_thumbnail_path TEXT,
    is_podcast INTEGER NOT NULL DEFAULT 0,
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
    finished BOOLEAN NOT NULL DEFAULT 0,
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

CREATE TABLE IF NOT EXISTS podcast_feeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    set_id INTEGER NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    feed_url TEXT NOT NULL,
    title TEXT,
    description TEXT,
    image_url TEXT,
    last_checked_at DATETIME,
    last_etag TEXT,
    check_interval_minutes INTEGER NOT NULL DEFAULT 60,
    auto_download INTEGER NOT NULL DEFAULT 0,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    next_check_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS podcast_episodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL REFERENCES podcast_feeds(id) ON DELETE CASCADE,
    media_id INTEGER UNIQUE REFERENCES media(id) ON DELETE SET NULL,
    guid TEXT NOT NULL,
    title TEXT,
    description TEXT,
    published_at DATETIME,
    episode_url TEXT NOT NULL,
    duration_seconds REAL,
    file_size INTEGER,
    file_name TEXT,
    is_downloaded INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid)
);

CREATE TABLE IF NOT EXISTS podcast_status (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    episode_id INTEGER NOT NULL REFERENCES podcast_episodes(id) ON DELETE CASCADE,
    is_completed INTEGER NOT NULL DEFAULT 0,
    position_seconds REAL NOT NULL DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, episode_id)
);
`

// indexesSchema defines all CREATE INDEX statements.
const indexesSchema = `
CREATE INDEX IF NOT EXISTS idx_media_set_id ON media(set_id);
CREATE INDEX IF NOT EXISTS idx_media_rel_path ON media(set_id, rel_path);
CREATE INDEX IF NOT EXISTS idx_media_deleted_at ON media(deleted_at);
CREATE INDEX IF NOT EXISTS idx_media_type ON media(type);
CREATE INDEX IF NOT EXISTS idx_media_filename ON media(file_name);
CREATE INDEX IF NOT EXISTS idx_api_tokens_token_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_permissions_user ON set_permissions(user_id);
CREATE INDEX IF NOT EXISTS idx_permissions_set ON set_permissions(set_id);
CREATE INDEX IF NOT EXISTS idx_shares_expires ON shares(expires_at);
CREATE INDEX IF NOT EXISTS idx_podcast_episodes_feed ON podcast_episodes(feed_id);
CREATE INDEX IF NOT EXISTS idx_podcast_episodes_media ON podcast_episodes(media_id);
CREATE INDEX IF NOT EXISTS idx_podcast_status_episode ON podcast_status(episode_id);
CREATE INDEX IF NOT EXISTS idx_sets_is_podcast ON sets(is_podcast);
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

// initializeSchema creates the database schema for a fresh database and applies pending migrations.
func initializeSchema(db *sql.DB) error {
	if err := enableForeignKeys(db); err != nil {
		return err
	}
	if err := execSchema(db, "tables", tablesSchema); err != nil {
		return err
	}
	if err := runMigrations(db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if err := execSchema(db, "indexes", indexesSchema); err != nil {
		return err
	}
	return nil
}

// migration is a idempotent schema change applied after the base schema.
type migration struct {
	name string
	sql  string
}

var migrations = []migration{
	{
		name: "create_api_tokens",
		sql: `
CREATE TABLE IF NOT EXISTS api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    last_used_at DATETIME,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`,
	},
	{
		name: "add_sets_is_podcast",
		sql:  `ALTER TABLE sets ADD COLUMN is_podcast INTEGER NOT NULL DEFAULT 0;`,
	},
	{
		name: "add_podcast_feed_backoff_columns",
		sql: `
ALTER TABLE podcast_feeds ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE podcast_feeds ADD COLUMN next_check_at DATETIME;
`,
	},
	{
		// Adds the "finished" flag used by GetProgress / SetProgress on the
		// playback_progress table.  Databases created before this column was
		// introduced still satisfy the CREATE TABLE IF NOT EXISTS in the base
		// schema, so the column must be added via migration to avoid a 500 on
		// GET /api/v1/media/{id} reading from progress.
		name: "add_playback_progress_finished",
		sql:  `ALTER TABLE playback_progress ADD COLUMN finished BOOLEAN NOT NULL DEFAULT 0;`,
	},
}

// runMigrations applies each migration in order, skipping ones whose SQL
// has already been partially applied (e.g. column already exists).
func runMigrations(db *sql.DB) error {
	for _, m := range migrations {
		if _, err := db.Exec(m.sql); err != nil {
			if isDuplicateColumnError(err) {
				continue
			}
			return fmt.Errorf("migration %q: %w", m.name, err)
		}
	}
	return nil
}

// isDuplicateColumnError returns true for SQLite errors raised when adding an
// existing column.
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name") || strings.Contains(msg, "already exists")
}
