package repository

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// legacyPodcastDB creates a database the way releases before podcast sync
// left it: every schema step except migratePodcastSync, with the legacy
// episode indexes.
func legacyPodcastDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", connectionDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	steps := []func(*sql.DB) error{
		enableForeignKeys,
		func(db *sql.DB) error { return execSchema(db, "tables", tablesSchema) },
		runMigrations, migratePlaybackProgress, migratePlaybackAccumulator,
		func(db *sql.DB) error {
			return execSchema(db, "legacy indexes", `
CREATE INDEX idx_podcast_episodes_feed ON podcast_episodes(feed_id);
CREATE INDEX idx_podcast_episodes_media ON podcast_episodes(media_id);`)
		},
	}
	for _, step := range steps {
		if err := step(db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// migrationClock gives the legacy rows distinct, known edit times.
var migrationClock = struct{ created, t1, t2, t3, t4, t5 time.Time }{
	created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	t1:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	t2:      time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
	t3:      time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
	t4:      time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC),
	t5:      time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC),
}

// seedLegacyPodcasts fills a legacy database: an admin, a podcast viewer
// and a stranger; a feed, an http variant of it (same key) and a feed whose
// URL has no key; episodes with and without GUIDs and an unusable
// enclosure; a downloaded episode with progress, a reset, a share, a note
// and a bookmark; and media progress outside the podcast set.
func seedLegacyPodcasts(t *testing.T, db *sql.DB) {
	t.Helper()
	c := migrationClock
	stmts := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, username, password_hash, is_admin) VALUES (1, 'admin', 'h', 1), (2, 'viewer', 'h', 0), (3, 'stranger', 'h', 0)`, nil},
		{`INSERT INTO sets (id, name, root_path, is_podcast) VALUES (1, 'podcast', 'podcast', 1), (2, 'music', 'music', 0)`, nil},
		{`INSERT INTO set_permissions (set_id, user_id, role) VALUES (1, 1, 'owner'), (1, 2, 'viewer'), (2, 3, 'viewer')`, nil},
		{`INSERT INTO podcast_feeds (id, set_id, feed_url, title, created_at) VALUES
 (1, 1, 'https://Example.com/rss', 'Show', ?), (2, 1, 'http://example.com/rss', 'Show again', ?), (3, 1, 'ftp://bad/rss', 'Bad', ?)`, []any{c.created, c.created, c.created}},
		{`INSERT INTO media (id, set_id, rel_path, file_name, abs_path, type) VALUES
 (1, 1, 'Show/ep4.mp3', 'ep4.mp3', '/m/podcast/Show/ep4.mp3', 'audio'), (2, 2, 'song.mp3', 'song.mp3', '/m/music/song.mp3', 'audio')`, nil},
		{`INSERT INTO podcast_episodes (id, feed_id, media_id, guid, title, episode_url, is_downloaded) VALUES
 (1, 1, NULL, 'g1', 'One', 'https://cdn.example.com/1.mp3', 0),
 (2, 1, NULL, '', 'Two', 'https://cdn.example.com/2.mp3', 0),
 (3, 1, NULL, 'dup', 'Three', 'not a url', 0),
 (4, 1, 1, 'g4', 'Four', 'https://cdn.example.com/4.mp3', 1),
 (5, 2, NULL, 'g1', 'One again', 'https://cdn.example.com/1.mp3', 0)`, nil},
		{`INSERT INTO podcast_status (user_id, episode_id, is_completed, position_seconds, updated_at) VALUES
 (2, 1, 1, 0, ?), (1, 5, 0, 0, ?)`, []any{c.t1, c.t2}},
		{`INSERT INTO playback_progress (user_id, media_id, position_seconds, accumulated_seconds, finished, updated_at) VALUES
 (2, 1, 100, 10, 0, ?), (1, 1, 300, 50, 1, ?), (2, 2, 42, 7, 0, ?)`, []any{c.t3, c.t4, c.t1}},
		{`INSERT INTO playback_resets (user_id, media_id, observed_ms) VALUES (2, 1, ?)`, []any{c.t5.UnixMilli()}},
		{`INSERT INTO shares (token, media_id, created_by, expires_at) VALUES ('tok', 1, 1, ?)`, []any{c.t5}},
		{`INSERT INTO media_notes (media_id, user_id, content) VALUES (1, 2, 'note')`, nil},
		{`INSERT INTO media_bookmarks (user_id, media_id, position_seconds, label) VALUES (2, 1, 12, 'mark')`, nil},
	}
	for _, s := range stmts {
		if _, err := db.Exec(s.query, s.args...); err != nil {
			t.Fatalf("seed %q: %v", s.query, err)
		}
	}
}

// tableSnapshot renders every row of a table, in a stable order, as text.
func tableSnapshot(t *testing.T, q interface {
	Query(string, ...any) (*sql.Rows, error)
}, table string) string {
	t.Helper()
	query := fmt.Sprintf(`SELECT * FROM %s ORDER BY rowid`, table)
	if table == "bookmarkColumns" {
		query = `SELECT id, user_id, media_id, position_seconds, label, created_at FROM media_bookmarks ORDER BY id`
	}
	rows, err := q.Query(query)
	if err != nil {
		t.Fatalf("snapshot %s: %v", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&b, vals...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// untouchedTables must not change during the migration.
// media_bookmarks is compared through bookmarkColumns: its sync columns
// are backfilled by an older startup step on the first start.
// sets is omitted: migrateMusicLibraries (A05) attaches library_id on Open.
// shares is omitted: migrateCollectionShares may rebuild/reorder empty columns on Open.
var untouchedTables = []string{"users", "set_permissions", "media", "media_notes",
	"playback_resets", "favorites", "podcast_status", "bookmarkColumns"}

func TestPodcastSyncMigrationFromLegacySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := legacyPodcastDB(t, path)
	seedLegacyPodcasts(t, db)
	before := map[string]string{}
	for _, table := range untouchedTables {
		before[table] = tableSnapshot(t, db, table)
	}
	musicProgress := progressRow(t, db, 2, 2)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range untouchedTables {
		if got := tableSnapshot(t, s.db, table); got != before[table] {
			t.Errorf("%s changed:\nbefore %s\nafter  %s", table, before[table], got)
		}
	}
	if got := progressRow(t, s.db, 2, 2); got != musicProgress {
		t.Errorf("non-podcast progress changed: %s -> %s", musicProgress, got)
	}
	checkMigratedFeeds(t, s.db)
	checkMigratedEpisodes(t, s.db)
	checkMigratedSubscriptions(t, s.db)
	checkMigratedState(t, s.db)
	checkMigratedMirrors(t, s.db)
	var fk int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys after migration = %d, %v; want 1", fk, err)
	}
	snapshot := migratedSnapshot(t, s.db)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A second start finds everything migrated and changes nothing.
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if again := migratedSnapshot(t, s.db); again != snapshot {
		t.Fatalf("second start changed state:\n%s\nvs\n%s", snapshot, again)
	}
}

// migratedSnapshot renders every table the migration writes.
func migratedSnapshot(t *testing.T, db *sql.DB) string {
	var b strings.Builder
	for _, table := range []string{"podcast_feeds", "podcast_episodes", "podcast_subscriptions",
		"podcast_episode_state", "podcast_sync_seq", "playback_progress", "server_secrets"} {
		b.WriteString(table + "\n" + tableSnapshot(t, db, table))
	}
	return b.String()
}

func progressRow(t *testing.T, db *sql.DB, userID, mediaID int64) string {
	t.Helper()
	var pos, acc float64
	var finished bool
	var updated time.Time
	err := db.QueryRow(`SELECT position_seconds, accumulated_seconds, finished, updated_at FROM playback_progress
WHERE user_id = ? AND media_id = ?`, userID, mediaID).Scan(&pos, &acc, &finished, &updated)
	if err != nil {
		t.Fatalf("progress %d/%d: %v", userID, mediaID, err)
	}
	return fmt.Sprintf("pos=%v acc=%v finished=%v at=%d", pos, acc, finished, updated.UnixMilli())
}

func checkMigratedFeeds(t *testing.T, db *sql.DB) {
	t.Helper()
	want := map[int64]string{1: "feed:example.com/rss <nil>", 2: "feed:example.com/rss 1", 3: "legacy-feed:3 <nil>"}
	for id, w := range want {
		var key string
		var merged sql.NullInt64
		if err := db.QueryRow(`SELECT feed_key, merged_into FROM podcast_feeds WHERE id = ?`, id).Scan(&key, &merged); err != nil {
			t.Fatal(err)
		}
		got := key + " <nil>"
		if merged.Valid {
			got = fmt.Sprintf("%s %d", key, merged.Int64)
		}
		if got != w {
			t.Errorf("feed %d = %q, want %q", id, got, w)
		}
	}
}

func checkMigratedEpisodes(t *testing.T, db *sql.DB) {
	t.Helper()
	want := map[int64]string{
		1: "guid:g1 enc:cdn.example.com/1.mp3 g1 <nil>",
		2: "enc:cdn.example.com/2.mp3 enc:cdn.example.com/2.mp3 <nil> <nil>",
		3: "legacy-episode:3  dup <nil>",
		4: "guid:g4 enc:cdn.example.com/4.mp3 g4 1",
		5: "guid:g1 enc:cdn.example.com/1.mp3 g1 <nil>",
	}
	for id, w := range want {
		var key, enc string
		var guid sql.NullString
		var media sql.NullInt64
		if err := db.QueryRow(`SELECT episode_key, enclosure_key, guid, media_id FROM podcast_episodes WHERE id = ?`, id).
			Scan(&key, &enc, &guid, &media); err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprintf("%s %s %s %s", key, enc, nullText(guid.Valid, guid.String), nullText(media.Valid, fmt.Sprint(media.Int64)))
		if got != w {
			t.Errorf("episode %d = %q, want %q", id, got, w)
		}
	}
}

func nullText(valid bool, s string) string {
	if !valid {
		return "<nil>"
	}
	return s
}

func checkMigratedSubscriptions(t *testing.T, db *sql.DB) {
	t.Helper()
	got := tableSnapshot(t, db, "podcast_subscriptions")
	ms := migrationClock.created.UnixMilli()
	want := fmt.Sprintf(`1 feed:example.com/rss 1 %[1]d Show https://Example.com/rss ["https://Example.com/rss"] 1
2 feed:example.com/rss 1 %[1]d Show https://Example.com/rss ["https://Example.com/rss"] 2
1 legacy-feed:3 1 %[1]d Bad ftp://bad/rss [] 3
2 legacy-feed:3 1 %[1]d Bad ftp://bad/rss [] 4
`, ms)
	if got != want {
		t.Errorf("subscriptions:\n%s\nwant\n%s", got, want)
	}
}

func checkMigratedState(t *testing.T, db *sql.DB) {
	t.Helper()
	c := migrationClock
	got := tableSnapshot(t, db, "podcast_episode_state")
	want := fmt.Sprintf(`1 feed:example.com/rss guid:g1 0 0 0 manual %d  5
1 feed:example.com/rss guid:g4 300 %d 1 auto %d  6
2 feed:example.com/rss guid:g1 0 0 1 manual %d  7
2 feed:example.com/rss guid:g4 0 %d 0 manual %d  8
`, c.t2.UnixMilli(), c.t4.UnixMilli(), c.t4.UnixMilli(), c.t1.UnixMilli(), c.t5.UnixMilli(), c.t5.UnixMilli())
	if got != want {
		t.Errorf("episode state:\n%s\nwant\n%s", got, want)
	}
	var next int64
	if err := db.QueryRow(`SELECT next_seq FROM podcast_sync_seq`).Scan(&next); err != nil || next != 9 {
		t.Errorf("next_seq = %d, %v; want 9", next, err)
	}
}

func checkMigratedMirrors(t *testing.T, db *sql.DB) {
	t.Helper()
	c := migrationClock
	// Viewer: the reset after the saved position wins both registers.
	if got, want := progressRow(t, db, 2, 1), fmt.Sprintf("pos=0 acc=10 finished=false at=%d", c.t5.UnixMilli()); got != want {
		t.Errorf("viewer mirror = %s, want %s", got, want)
	}
	// Admin: finished with the position of the same edit, accumulation kept.
	if got, want := progressRow(t, db, 1, 1), fmt.Sprintf("pos=300 acc=50 finished=true at=%d", c.t4.UnixMilli()); got != want {
		t.Errorf("admin mirror = %s, want %s", got, want)
	}
}

func TestPodcastSyncMigrationClearsLegacyOrphans(t *testing.T) {
	// A media link that the foreign key check flags is cleared during the
	// copy rather than aborting, so a start never fails on legacy orphans.
	path := filepath.Join(t.TempDir(), "orphan.db")
	db := legacyPodcastDB(t, path)
	seedLegacyPodcasts(t, db)
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE podcast_episodes SET media_id = 99 WHERE id = 2`,
		`INSERT INTO podcast_episodes (id, feed_id, guid, episode_url) VALUES (9, 77, 'gone', 'https://cdn.example.com/9.mp3')`,
		`INSERT INTO podcast_status (user_id, episode_id, is_completed) VALUES (2, 9, 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrate with orphans: %v", err)
	}
	defer s.Close()
	var media sql.NullInt64
	if err := s.db.QueryRow(`SELECT media_id FROM podcast_episodes WHERE id = 2`).Scan(&media); err != nil || media.Valid {
		t.Errorf("dangling media link kept: %v %v", media, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM podcast_episodes WHERE id = 9`).Scan(&n); err != nil || n != 0 {
		t.Errorf("episode of a deleted feed copied: %d %v", n, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM podcast_status WHERE episode_id = 9`).Scan(&n); err != nil || n != 0 {
		t.Errorf("status of a dropped episode kept: %d %v", n, err)
	}
}

func TestCheckEpisodeForeignKeysReportsViolation(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO podcast_episodes (feed_id, episode_key, episode_url) VALUES (404, 'guid:x', 'u')`); err != nil {
		t.Fatal(err)
	}
	if err := checkEpisodeForeignKeys(ctx, tx); err == nil || !strings.Contains(err.Error(), "violation") {
		t.Fatalf("violation not reported: %v", err)
	}
}
