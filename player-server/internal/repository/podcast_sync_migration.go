package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"

	"codeberg.org/snonux/player/internal/podcast"
)

// The podcast synchronization migration (docs/podcast-sync-contract.md, §9.1).
//
// tablesSchema still creates podcast_feeds and podcast_episodes in their
// pre-sync shape; migratePodcastSync upgrades both, for fresh and existing
// databases alike, so there is exactly one definition of the final shape and
// the upgrade path is exercised on every start. Steps 1 and 2 each run in
// their own transaction, gated on the column they add; steps 3-7 run in one
// transaction gated on the absence of podcast_subscriptions, so a crash
// leaves either none or all of the backfills and a restart never repeats
// them. No file is touched, no audio downloaded, no permission granted.

// podcastSyncTables are the per-account state tables (§5.1). All clocks are
// INTEGER milliseconds, so none of them belongs in timeColumns. Every table
// referencing users cascades and is listed in userOwned; none references
// media or podcast_episodes, so deleting a server copy or a catalog row can
// never delete account state.
const podcastSyncTables = `
CREATE TABLE podcast_subscriptions (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feed_key TEXT NOT NULL,
    subscribed INTEGER NOT NULL,
    subscription_ms INTEGER NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    fetch_url TEXT NOT NULL DEFAULT '',
    urls TEXT NOT NULL DEFAULT '[]',
    change_seq INTEGER NOT NULL,
    PRIMARY KEY (user_id, feed_key));
CREATE INDEX idx_podcast_sub_seq ON podcast_subscriptions(user_id, change_seq);
CREATE INDEX idx_podcast_sub_feed ON podcast_subscriptions(feed_key, subscribed);

CREATE TABLE podcast_episode_state (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feed_key TEXT NOT NULL,
    episode_key TEXT NOT NULL,
    position_seconds REAL NOT NULL DEFAULT 0,
    position_ms INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    completion_source TEXT NOT NULL DEFAULT '',
    completion_ms INTEGER NOT NULL DEFAULT 0,
    enclosure_url TEXT NOT NULL DEFAULT '',
    change_seq INTEGER NOT NULL,
    PRIMARY KEY (user_id, feed_key, episode_key));
CREATE INDEX idx_podcast_state_seq ON podcast_episode_state(user_id, change_seq);

CREATE TABLE podcast_feed_aliases (
    alias_key TEXT PRIMARY KEY,
    feed_key TEXT NOT NULL,
    change_seq INTEGER NOT NULL);
CREATE INDEX idx_podcast_feed_aliases_feed ON podcast_feed_aliases(feed_key);
CREATE INDEX idx_podcast_feed_aliases_seq ON podcast_feed_aliases(change_seq);

CREATE TABLE podcast_episode_aliases (
    feed_key TEXT NOT NULL,
    alias_key TEXT NOT NULL,
    episode_key TEXT NOT NULL,
    change_seq INTEGER NOT NULL,
    PRIMARY KEY (feed_key, alias_key));
CREATE INDEX idx_podcast_episode_aliases_target ON podcast_episode_aliases(feed_key, episode_key);
CREATE INDEX idx_podcast_episode_aliases_seq ON podcast_episode_aliases(change_seq);

-- One row: the next change sequence number. Every written row takes its own
-- number inside the write transaction, so paging by "change_seq > since"
-- never splits or skips one.
CREATE TABLE podcast_sync_seq (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    next_seq INTEGER NOT NULL);
`

// podcastEpisodesRebuilt is the final shape of podcast_episodes: guid
// nullable, identity by episode_key, latest enclosure in enclosure_key.
const podcastEpisodesRebuilt = `
CREATE TABLE podcast_episodes_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL REFERENCES podcast_feeds(id) ON DELETE CASCADE,
    media_id INTEGER UNIQUE REFERENCES media(id) ON DELETE SET NULL,
    guid TEXT,
    episode_key TEXT NOT NULL,
    enclosure_key TEXT NOT NULL DEFAULT '',
    title TEXT,
    description TEXT,
    published_at DATETIME,
    episode_url TEXT NOT NULL,
    duration_seconds REAL,
    file_size INTEGER,
    file_name TEXT,
    is_downloaded INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, episode_key));`

// podcastSyncEpochSecret names the server_secrets row holding the sync
// epoch, a random value that changes when the database is replaced.
const podcastSyncEpochSecret = "podcast_sync_epoch"

// migratePodcastSync runs the three gated steps of the migration in order.
func migratePodcastSync(db *sql.DB) error {
	if err := addPodcastFeedKeys(db); err != nil {
		return fmt.Errorf("podcast feed keys: %w", err)
	}
	if err := rebuildPodcastEpisodes(db); err != nil {
		return fmt.Errorf("rebuild podcast episodes: %w", err)
	}
	if err := createPodcastAccountState(db); err != nil {
		return fmt.Errorf("podcast account state: %w", err)
	}
	return nil
}

// hasColumn reports whether table has column.
func hasColumn(q sqlQueryRower, table, column string) (bool, error) {
	var n int
	err := q.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	return n > 0, err
}

// hasTable reports whether a table of that name exists.
func hasTable(q sqlQueryRower, table string) (bool, error) {
	var n int
	err := q.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n)
	return n > 0, err
}

// feedKeyOrLegacy returns the identity of a stored feed URL, or the
// server-minted legacy key when the URL has none (§3.1).
func feedKeyOrLegacy(feedURL string, id int64) string {
	if key, err := podcast.FeedKey(feedURL); err == nil {
		return key
	}
	return fmt.Sprintf("legacy-feed:%d", id)
}

// addPodcastFeedKeys is step 1: add feed_key, merged_into and created_by to
// podcast_feeds and mint a key for every row. Rows sharing a key keep the
// smallest id as the catalog feed; the others are marked merged into it.
func addPodcastFeedKeys(db *sql.DB) error {
	if done, err := hasColumn(db, "podcast_feeds", "feed_key"); err != nil || done {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`ALTER TABLE podcast_feeds ADD COLUMN feed_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE podcast_feeds ADD COLUMN merged_into INTEGER`,
		`ALTER TABLE podcast_feeds ADD COLUMN created_by INTEGER`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if err := backfillFeedKeys(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// backfillFeedKeys mints the key of every feed row in id order.
func backfillFeedKeys(tx *sql.Tx) error {
	type feedURL struct {
		id  int64
		url string
	}
	rows, err := tx.Query(`SELECT id, feed_url FROM podcast_feeds ORDER BY id`)
	if err != nil {
		return err
	}
	var feeds []feedURL
	for rows.Next() {
		var f feedURL
		if err := rows.Scan(&f.id, &f.url); err != nil {
			_ = rows.Close()
			return err
		}
		feeds = append(feeds, f)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	owner := map[string]int64{}
	for _, f := range feeds {
		key := feedKeyOrLegacy(f.url, f.id)
		var mergedInto any
		if first, dup := owner[key]; dup {
			mergedInto = first
		} else {
			owner[key] = f.id
		}
		if _, err := tx.Exec(`UPDATE podcast_feeds SET feed_key = ?, merged_into = ? WHERE id = ?`,
			key, mergedInto, f.id); err != nil {
			return err
		}
	}
	return nil
}

// closeRows closes rows and returns the iteration error, if any.
func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}

// rebuildPodcastEpisodes is step 2: rebuild podcast_episodes with the
// SQLite twelve-step procedure, keeping every id so media links and
// podcast_status rows stay valid. Foreign keys must be off while the old
// table is dropped, and the PRAGMA is a no-op inside a transaction, so the
// rebuild runs on one dedicated connection (the DSN normally forces them on,
// see connectionDSN) and switches them back on before releasing it.
func rebuildPodcastEpisodes(db *sql.DB) error {
	if done, err := hasColumn(db, "podcast_episodes", "episode_key"); err != nil || done {
		return err
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	err = rebuildEpisodesOn(ctx, conn)
	if _, onErr := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err == nil {
		err = onErr
	}
	return err
}

// rebuildEpisodesOn runs the rebuild transaction on conn.
func rebuildEpisodesOn(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var oldSeq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM sqlite_sequence WHERE name = 'podcast_episodes'`).Scan(&oldSeq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, podcastEpisodesRebuilt); err != nil {
		return err
	}
	if err := copyEpisodesWithKeys(ctx, tx); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DROP TABLE podcast_episodes`,
		`ALTER TABLE podcast_episodes_new RENAME TO podcast_episodes`,
		// What ON DELETE CASCADE would have removed with the episodes of
		// deleted feeds that copyEpisodesWithKeys left out.
		`DELETE FROM podcast_status WHERE episode_id NOT IN (SELECT id FROM podcast_episodes)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Ids of deleted episodes are never handed out again.
	if _, err := tx.ExecContext(ctx,
		`UPDATE sqlite_sequence SET seq = MAX(seq, ?) WHERE name = 'podcast_episodes'`, oldSeq); err != nil {
		return err
	}
	if err := checkEpisodeForeignKeys(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// legacyEpisode is a stored episode row as the rebuild sees it.
type legacyEpisode struct {
	id, feedID int64
	guid, url  string
}

// copyEpisodesWithKeys copies every episode of an existing feed into the
// new table under its episode key. Each feed's stored rows, in id order,
// count as one feed document (podcast.EpisodeKeys); a row left without a
// key (unusable enclosure, duplicate) gets "legacy-episode:<id>". Episodes
// of feeds deleted while foreign keys were off are not copied, and a link
// to a missing media row is cleared, as the foreign keys would have done.
func copyEpisodesWithKeys(ctx context.Context, tx *sql.Tx) error {
	episodes, err := loadLegacyEpisodes(ctx, tx)
	if err != nil {
		return err
	}
	for start := 0; start < len(episodes); {
		end := start
		for end < len(episodes) && episodes[end].feedID == episodes[start].feedID {
			end++
		}
		if err := copyFeedEpisodes(ctx, tx, episodes[start:end]); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// loadLegacyEpisodes returns the episodes of existing feeds by feed and id.
func loadLegacyEpisodes(ctx context.Context, tx *sql.Tx) ([]legacyEpisode, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.id, e.feed_id, COALESCE(e.guid, ''), e.episode_url
FROM podcast_episodes e JOIN podcast_feeds f ON f.id = e.feed_id ORDER BY e.feed_id, e.id`)
	if err != nil {
		return nil, err
	}
	var episodes []legacyEpisode
	for rows.Next() {
		var e legacyEpisode
		if err := rows.Scan(&e.id, &e.feedID, &e.guid, &e.url); err != nil {
			_ = rows.Close()
			return nil, err
		}
		episodes = append(episodes, e)
	}
	return episodes, closeRows(rows)
}

// copyFeedEpisodes copies the rows of one feed.
func copyFeedEpisodes(ctx context.Context, tx *sql.Tx, episodes []legacyEpisode) error {
	items := make([]podcast.IdentityItem, len(episodes))
	for i, e := range episodes {
		items[i] = podcast.IdentityItem{GUID: e.guid, EnclosureURL: e.url}
	}
	keys := podcast.EpisodeKeys(items)
	for i, e := range episodes {
		key := keys[i]
		if key == "" {
			key = fmt.Sprintf("legacy-episode:%d", e.id)
		}
		enc, err := podcast.EnclosureKey(e.url)
		if err != nil {
			enc = ""
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO podcast_episodes_new
(id, feed_id, media_id, guid, episode_key, enclosure_key, title, description, published_at,
 episode_url, duration_seconds, file_size, file_name, is_downloaded, created_at)
SELECT id, feed_id, (SELECT m.id FROM media m WHERE m.id = e.media_id), NULLIF(guid, ''), ?, ?,
 title, description, published_at, episode_url, duration_seconds, file_size, file_name,
 is_downloaded, created_at
FROM podcast_episodes e WHERE id = ?`, key, enc, e.id); err != nil {
			return fmt.Errorf("copy episode %d: %w", e.id, err)
		}
	}
	return nil
}

// checkEpisodeForeignKeys aborts the rebuild when the new table violates a
// foreign key. Only podcast_episodes is checked: orphans that other tables
// collected while enforcement was off are not caused by the rebuild and are
// cleaned up by removeOrphanRows afterwards.
func checkEpisodeForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check(podcast_episodes)`)
	if err != nil {
		return err
	}
	violations := 0
	for rows.Next() {
		violations++
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	if violations > 0 {
		return fmt.Errorf("foreign key check found %d violation(s) in podcast_episodes", violations)
	}
	return nil
}

// createPodcastAccountState is steps 3-7 in one transaction: create the
// account state tables, the sync epoch and sequence, and backfill
// subscriptions, episode state and the playback_progress mirrors.
func createPodcastAccountState(db *sql.DB) error {
	if done, err := hasTable(db, "podcast_subscriptions"); err != nil || done {
		return err
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, podcastSyncTables); err != nil {
		return err
	}
	if err := ensurePodcastSyncEpoch(ctx, tx); err != nil {
		return err
	}
	seq := &migrationSeq{next: 1}
	if err := backfillSubscriptions(ctx, tx, seq); err != nil {
		return fmt.Errorf("subscriptions: %w", err)
	}
	if err := backfillEpisodeState(ctx, tx, seq); err != nil {
		return fmt.Errorf("episode state: %w", err)
	}
	if err := rewriteAllEpisodeMirrors(ctx, tx); err != nil {
		return fmt.Errorf("progress mirrors: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO podcast_sync_seq (id, next_seq) VALUES (1, ?)`, seq.next); err != nil {
		return err
	}
	return tx.Commit()
}

// migrationSeq hands out change sequence numbers during the migration; the
// final value is stored once in podcast_sync_seq.
type migrationSeq struct{ next int64 }

func (s *migrationSeq) take() int64 {
	s.next++
	return s.next - 1
}

// ensurePodcastSyncEpoch stores a random 128-bit sync epoch unless one exists.
func ensurePodcastSyncEpoch(ctx context.Context, tx *sql.Tx) error {
	epoch := make([]byte, 16)
	if _, err := rand.Read(epoch); err != nil {
		return fmt.Errorf("generate sync epoch: %w", err)
	}
	_, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO server_secrets (name, value) VALUES (?, ?)`, podcastSyncEpochSecret, epoch)
	return err
}

// backfillSubscriptions is step 4: every user who can list a catalog feed
// today (admins, and users with any permission on its set) gets a
// subscription, clocked at the feed's creation time.
func backfillSubscriptions(ctx context.Context, tx *sql.Tx, seq *migrationSeq) error {
	rows, err := tx.QueryContext(ctx, `SELECT u.id, f.feed_key, f.feed_url, COALESCE(f.title, ''), f.created_at
FROM podcast_feeds f JOIN users u ON u.is_admin = 1
  OR EXISTS (SELECT 1 FROM set_permissions p WHERE p.set_id = f.set_id AND p.user_id = u.id)
WHERE f.merged_into IS NULL ORDER BY f.id, u.id`)
	if err != nil {
		return err
	}
	var subs []migratedSubscription
	for rows.Next() {
		var s migratedSubscription
		var created sql.NullTime
		if err := rows.Scan(&s.userID, &s.feedKey, &s.fetchURL, &s.title, &created); err != nil {
			_ = rows.Close()
			return err
		}
		s.ms = clockOf(created)
		subs = append(subs, s)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, s := range subs {
		if err := s.insert(ctx, tx, seq.take()); err != nil {
			return err
		}
	}
	return nil
}

// migratedSubscription is one subscription created by step 4.
type migratedSubscription struct {
	userID            int64
	feedKey, fetchURL string
	title             string
	ms                int64
}

func (s migratedSubscription) insert(ctx context.Context, tx *sql.Tx, changeSeq int64) error {
	urls := []string{}
	if _, err := podcast.FeedKey(s.fetchURL); err == nil {
		urls = append(urls, s.fetchURL)
	}
	encoded, err := json.Marshal(urls)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO podcast_subscriptions
(user_id, feed_key, subscribed, subscription_ms, title, fetch_url, urls, change_seq)
VALUES (?, ?, 1, ?, ?, ?, ?, ?)`, s.userID, s.feedKey, s.ms, s.title, s.fetchURL, string(encoded), changeSeq)
	return err
}

// clockOf converts a stored timestamp to a register clock. A missing time
// becomes 1 ms, older than every real edit, so it never wins a merge.
func clockOf(t sql.NullTime) int64 {
	if !t.Valid || t.Time.UnixMilli() < 1 {
		return 1
	}
	return t.Time.UnixMilli()
}
