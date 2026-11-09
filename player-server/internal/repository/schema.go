package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// tablesSchema defines all CREATE TABLE statements for a fresh database.
const tablesSchema = `
CREATE TABLE IF NOT EXISTS radio_stations (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL,
 stream_url TEXT NOT NULL,
 homepage_url TEXT NOT NULL DEFAULT '',
 artwork_url TEXT NOT NULL DEFAULT '',
 local_network INTEGER NOT NULL DEFAULT 0
);

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

CREATE TABLE IF NOT EXISTS music_libraries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    root_path TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id INTEGER REFERENCES music_libraries(id),
    name TEXT NOT NULL,
    root_path TEXT NOT NULL,
    cover_thumbnail_path TEXT,
    is_podcast INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(library_id, root_path)
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

CREATE TABLE IF NOT EXISTS playlists (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    comment TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS playlist_entries (
    id TEXT PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    UNIQUE(playlist_id, position)
);
CREATE INDEX IF NOT EXISTS idx_playlists_user ON playlists(user_id);
CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS media_tags (
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (media_id, tag_id)
);

CREATE TABLE IF NOT EXISTS music_listening_sessions (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL,
 media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 last_position REAL NOT NULL,
 accumulated_seconds REAL NOT NULL,
 observed_ms INTEGER NOT NULL,
 qualified_ms INTEGER,
 PRIMARY KEY(user_id,session_id,media_id)
);

CREATE TABLE IF NOT EXISTS music_artwork (
 kind TEXT NOT NULL CHECK(kind IN ('artist','playlist')),
 entity_key TEXT NOT NULL,
 data BLOB NOT NULL,
 revision TEXT NOT NULL,
 PRIMARY KEY(kind,entity_key)
);
CREATE TRIGGER IF NOT EXISTS playlist_artwork_delete AFTER DELETE ON playlists BEGIN
 DELETE FROM music_artwork WHERE kind='playlist' AND entity_key=old.id;
END;
CREATE TABLE IF NOT EXISTS music_enrichment (
    provider TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('artist','album')),
    entity_key TEXT NOT NULL,
    payload TEXT NOT NULL,
    PRIMARY KEY(provider,kind,entity_key)
);
CREATE TABLE IF NOT EXISTS provider_lyrics (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 identity TEXT NOT NULL, payload TEXT NOT NULL, fetched_at DATETIME NOT NULL,
 PRIMARY KEY(user_id,media_id)
);

CREATE TABLE IF NOT EXISTS music_ratings (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('song','album','artist')),
 entity_key TEXT NOT NULL,
 media_id INTEGER REFERENCES media(id) ON DELETE CASCADE,
 rating INTEGER NOT NULL CHECK(rating BETWEEN 1 AND 5),
 CHECK((kind='song' AND media_id IS NOT NULL AND entity_key=CAST(media_id AS TEXT)) OR (kind!='song' AND media_id IS NULL)),
 PRIMARY KEY(user_id,kind,entity_key)
);

CREATE TABLE IF NOT EXISTS music_entity_favorites (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('album','artist','playlist')),
    entity_key TEXT NOT NULL,
    PRIMARY KEY (user_id, kind, entity_key)
);

CREATE TABLE IF NOT EXISTS favorites (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE IF NOT EXISTS media_bookmarks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    position_seconds REAL NOT NULL CHECK(position_seconds >= 0),
    label TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_media_bookmarks_user_media_position
    ON media_bookmarks(user_id, media_id, position_seconds, id);

CREATE TABLE IF NOT EXISTS playback_progress (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    position_seconds REAL NOT NULL,
    accumulated_seconds REAL NOT NULL DEFAULT 0,
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
    session_id TEXT NOT NULL,
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

-- Secrets the server generates for itself and must keep across restarts
-- (currently only the key that signs share viewing credentials). Existing
-- databases get the table from this same statement: it runs on every start.
CREATE TABLE IF NOT EXISTS server_secrets (
    name TEXT PRIMARY KEY,
    value BLOB NOT NULL
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

-- podcast_feeds and podcast_episodes are created in their pre-sync shape
-- and upgraded by migratePodcastSync (podcast_sync_migration.go) on every
-- database, fresh or old, so the final shape is defined in one place.
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

CREATE TABLE IF NOT EXISTS radio_bookmarks (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    station_uuid TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    stream_url TEXT NOT NULL,
    homepage TEXT NOT NULL DEFAULT '',
    favicon_url TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    country_code TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '',
    codec TEXT NOT NULL DEFAULT '',
    bitrate INTEGER NOT NULL DEFAULT 0,
    votes INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, station_uuid)
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
CREATE UNIQUE INDEX IF NOT EXISTS idx_podcast_feeds_key ON podcast_feeds(feed_key) WHERE merged_into IS NULL;
CREATE INDEX IF NOT EXISTS idx_podcast_feeds_created_by ON podcast_feeds(created_by, created_at);
-- Every user's state of one episode: seeding a new server copy's progress
-- mirrors and joining catalog episodes with state look it up by key.
CREATE INDEX IF NOT EXISTS idx_podcast_state_episode ON podcast_episode_state(feed_key, episode_key);
`

// execSchema executes a raw SQL schema block against the given database.
func execSchema(db *sql.DB, name, schema string) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("execute %s schema: %w", name, err)
	}
	return nil
}

// enableForeignKeys turns on SQLite foreign key enforcement for the
// connection that runs the schema setup. It covers stores built with New
// from a caller's *sql.DB; Open also sets it in the DSN, which is what makes
// it hold for every later connection (see connection.go).
func enableForeignKeys(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	return nil
}

// schemaStep is one named step of the schema bootstrap. A step whose
// everyStart is false is a migration: it runs once and is recorded in
// schema_migrations. Steps that create missing tables or indexes, purge by
// retention or repair invariants run on every start.
//
// Databases from before schema_migrations existed have applied some
// migrations without a record. Every migration therefore keeps its own
// guard (a column probe, CREATE ... IF NOT EXISTS, a "duplicate column"
// error) and is recorded the first time it runs here, applied or not.
// Downgrades are not supported: several migrations rebuild tables one way.
type schemaStep struct {
	name       string
	run        func(*sql.DB) error
	everyStart bool
}

// schemaSteps is the one ordered list the bootstrap runs. Order matters
// where noted; append new migrations at the end of their group, and give
// every entry a new, never reused name.
var schemaSteps = func() []schemaStep {
	steps := []schemaStep{
		{name: "tables", run: func(db *sql.DB) error { return execSchema(db, "tables", tablesSchema) }, everyStart: true},
	}
	for _, m := range migrations {
		steps = append(steps, schemaStep{name: "sql:" + m.name, run: m.apply})
	}
	return append(steps,
		schemaStep{name: "collection_shares", run: migrateCollectionShares},
		schemaStep{name: "playback_progress_accumulated", run: migratePlaybackProgress},
		schemaStep{name: "playback_accumulator", run: migratePlaybackAccumulator},
		// Before the indexes: the episode rebuild drops the old table's
		// indexes, which indexesSchema recreates on the new one. The deletion
		// columns are needed before and after the podcast sync rebuild.
		schemaStep{name: "podcast_episode_deletion_before_sync", run: migratePodcastEpisodeDeletion},
		schemaStep{name: "podcast_sync", run: migratePodcastSync},
		schemaStep{name: "podcast_episode_deletion", run: migratePodcastEpisodeDeletion},
		schemaStep{name: "podcast_retention", run: migratePodcastRetention},
		schemaStep{name: "podcast_episode_artwork", run: migratePodcastEpisodeArtwork},
		schemaStep{name: "indexes", run: createIndexes, everyStart: true},
		schemaStep{name: "podcast_catalog_creations", run: migratePodcastCatalogCreations},
		schemaStep{name: "music_libraries", run: migrateMusicLibraries},
		schemaStep{name: "music_identity_conflicts", run: migrateMusicIdentityConflicts},
		// The sets rebuild of the music library migration drops the sets
		// indexes; recreate them in the same start.
		schemaStep{name: "indexes_after_rebuilds", run: createIndexes, everyStart: true},
		// Needs the final podcast tables and the episode state index.
		schemaStep{name: "repair_podcast_identities", run: repairPodcastIdentities, everyStart: true},
		schemaStep{name: "saved_queues", run: initializeSavedQueues},
		// Creates its tables and applies the history retention.
		schemaStep{name: "music_history", run: initializeMusicHistory, everyStart: true},
		// Last, when every table has its final shape.
		schemaStep{name: "normalize_stored_times", run: normalizeStoredTimes},
		schemaStep{name: "bookmark_sync_times", run: backfillBookmarkSyncTimes, everyStart: true},
		schemaStep{name: "remove_orphan_rows", run: removeOrphanRows, everyStart: true},
	)
}()

func createIndexes(db *sql.DB) error { return execSchema(db, "indexes", indexesSchema) }

// initializeSchema creates the database schema for a fresh database and
// applies pending migrations (schemaSteps).
func initializeSchema(db *sql.DB) error {
	if err := enableForeignKeys(db); err != nil {
		return err
	}
	return runSchemaSteps(db, schemaSteps)
}

// runSchemaSteps runs steps in order: every-start steps always, migrations
// only when schema_migrations has no record of them, recording each after
// it succeeded.
func runSchemaSteps(db *sql.DB, steps []schemaStep) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
 name TEXT PRIMARY KEY,
 applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		applied[name] = true
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, step := range steps {
		if seen[step.name] {
			return fmt.Errorf("schema step %q listed twice", step.name)
		}
		seen[step.name] = true
		if !step.everyStart && applied[step.name] {
			continue
		}
		if err := step.run(db); err != nil {
			return fmt.Errorf("schema step %s: %w", step.name, err)
		}
		if step.everyStart {
			continue
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations (name) VALUES (?)`, step.name); err != nil {
			return fmt.Errorf("record schema step %s: %w", step.name, err)
		}
	}
	return nil
}

// migration is a single-statement schema change applied after the base
// schema, inside a transaction. One statement per entry: a multi-statement
// migration could stop half applied, and its guard would then skip the rest.
type migration struct {
	name string
	sql  string
}

// apply runs the migration in a transaction. A column or table that
// already exists means the change was applied before schema_migrations
// recorded it.
func (m migration) apply(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(m.sql); err != nil {
		if isDuplicateColumnError(err) {
			return nil
		}
		return fmt.Errorf("migration %q: %w", m.name, err)
	}
	return tx.Commit()
}

var migrations = []migration{
	{name: "share_collection_title", sql: `ALTER TABLE shares ADD COLUMN collection_title TEXT NOT NULL DEFAULT '';`},
	{name: "share_audio_quality", sql: `ALTER TABLE shares ADD COLUMN audio_quality TEXT NOT NULL DEFAULT '';`},
	{name: "share_description", sql: `ALTER TABLE shares ADD COLUMN description TEXT NOT NULL DEFAULT '';`},
	{name: "share_collection_ids", sql: `ALTER TABLE shares ADD COLUMN media_ids TEXT NOT NULL DEFAULT '[]';`},
	{name: "playlist_rules", sql: `ALTER TABLE playlists ADD COLUMN rules TEXT NOT NULL DEFAULT '';`},
	{name: "playlist_source", sql: `ALTER TABLE playlists ADD COLUMN source TEXT NOT NULL DEFAULT '';`},
	{name: "playlist_visibility", sql: `ALTER TABLE playlists ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private' CHECK(visibility IN ('private','shared'));`},
	{name: "media_music", sql: `ALTER TABLE media ADD COLUMN music TEXT;`},
	{name: "bookmark_sync_key", sql: `ALTER TABLE media_bookmarks ADD COLUMN sync_key TEXT NOT NULL DEFAULT '';`},
	{name: "bookmark_sync_time", sql: `ALTER TABLE media_bookmarks ADD COLUMN updated_ms INTEGER NOT NULL DEFAULT 0;`},
	{name: "bookmark_sync_deleted", sql: `ALTER TABLE media_bookmarks ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0;`},
	{name: "bookmark_sync_legacy_keys", sql: `UPDATE media_bookmarks SET sync_key='server:' || id WHERE sync_key='';`},
	{name: "bookmark_sync_index", sql: `CREATE UNIQUE INDEX IF NOT EXISTS idx_bookmark_sync_key ON media_bookmarks(user_id,media_id,sync_key);`},
	{name: "playback_reset_clock", sql: `CREATE TABLE IF NOT EXISTS playback_resets (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 observed_ms INTEGER NOT NULL, PRIMARY KEY(user_id, media_id));`},
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
	// The two backoff columns were once one two-statement migration, which a
	// crash could leave half applied; one statement each heals that case.
	{
		name: "add_podcast_feed_backoff_failures",
		sql:  `ALTER TABLE podcast_feeds ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;`,
	},
	{
		name: "add_podcast_feed_backoff_next_check",
		sql:  `ALTER TABLE podcast_feeds ADD COLUMN next_check_at DATETIME;`,
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
	{
		// Heals existing databases that ingested macOS AppleDouble sidecars
		// (file names starting with "._") before the scanner learned to skip
		// them. The underscore in LIKE is a wildcard matching any single
		// character, so we escape it via ESCAPE '\' to anchor the literal
		// "._" prefix and avoid sweeping up unrelated dot-prefixed names
		// such as ".gitignore" or ".hidden.mp3".
		name: "delete_appledouble_media",
		sql:  `DELETE FROM media WHERE file_name LIKE '.\_%' ESCAPE '\';`,
	},
}

// runMigrations applies every single-statement migration without
// recording it (test fixtures that build older database shapes use it).
func runMigrations(db *sql.DB) error {
	for _, m := range migrations {
		if err := m.apply(db); err != nil {
			return err
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

// migratePlaybackProgress backfills live session playback only when adding the
// column. Repeating the backfill would restore eligibility after finishing.
func migratePlaybackProgress(db *sql.DB) error {
	var column string
	err := db.QueryRow(`SELECT name FROM pragma_table_info('playback_progress') WHERE name = 'accumulated_seconds'`).Scan(&column)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE playback_progress ADD COLUMN accumulated_seconds REAL NOT NULL DEFAULT 0;`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE playback_progress
SET accumulated_seconds = COALESCE((
    SELECT SUM(pa.accumulated_seconds)
    FROM playback_accumulator pa
    JOIN sessions s ON s.id = pa.session_id
    WHERE s.user_id = playback_progress.user_id AND pa.media_id = playback_progress.media_id
), 0)
WHERE finished = 0;`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// migratePlaybackAccumulator removes the session foreign key so Bearer tokens
// can use their synthetic session IDs for per-token delta accounting.
func migratePlaybackAccumulator(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA foreign_key_list(playback_accumulator)`)
	if err != nil {
		return err
	}
	needsMigration := false
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			_ = rows.Close()
			return err
		}
		if table == "sessions" {
			needsMigration = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !needsMigration {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE TABLE playback_accumulator_new (
    session_id TEXT NOT NULL,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    last_position REAL NOT NULL DEFAULT 0,
    accumulated_seconds REAL NOT NULL DEFAULT 0,
    counted INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, media_id)
);
INSERT INTO playback_accumulator_new SELECT * FROM playback_accumulator;
DROP TABLE playback_accumulator;
ALTER TABLE playback_accumulator_new RENAME TO playback_accumulator;`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
