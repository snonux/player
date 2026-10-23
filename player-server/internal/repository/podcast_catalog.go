package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/podcast"
)

// Catalog identity: resolving feed keys through aliases, creating catalog
// rows for subscriptions, and keyed episode ingest (contract §3.3, §7.3).
// Only the server's own observations create aliases; keys a client sends
// are only looked up, so no user can link or redirect another user's feeds.

const (
	// maxSubscriptionURLs bounds the urls of one subscription register.
	maxSubscriptionURLs = 10
	// podcastSetRoot is the root path of the shared podcast set.
	podcastSetRoot = "podcast"
	// legacyFeedPrefix and legacyEpisodePrefix start server-minted keys
	// of rows whose stored data had no identity of its own (§3.1).
	legacyFeedPrefix    = "legacy-feed:"
	legacyEpisodePrefix = "legacy-episode:"
)

// PodcastCatalogRepo resolves catalog identities and ingests feed documents.
type PodcastCatalogRepo interface {
	// GetFeedByKey returns the catalog feed (not merged into another) whose
	// key, or one of whose aliases, is key; nil when there is none.
	GetFeedByKey(ctx context.Context, key string) (*model.PodcastFeed, error)
	// CreateDeferredPodcastFeeds retries catalog creation for active
	// subscriptions skipped by the hourly quota, without fetching.
	CreateDeferredPodcastFeeds(ctx context.Context, now time.Time, checkInterval int) error
	// IngestFeedEpisodes matches the episodes of one fetched feed document
	// against the feed's catalog episodes (podcast.MatchEpisodes), creates
	// the new ones, refreshes the enclosure of the known ones and records
	// the aliases ingest found, in one transaction. It never downloads.
	IngestFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, episodes []model.PodcastEpisode, now time.Time) error
	// ClearEpisodeDownloads clears stale download flags across the feed's
	// catalog rows after copy deletion. Live media links stay intact.
	ClearEpisodeDownloads(ctx context.Context, feedID int64) error
	// MoveFeed records that a catalog feed permanently moved to target (a
	// 301/308 redirect, <itunes:new-feed-url>, or an admin's corrected
	// URL): target becomes its fetch URL and the target's key an alias of
	// the feed; a legacy-feed: key is retired into the target's key; when
	// the target is another catalog feed, the feed is merged into it
	// (contract §3.3). FeedNotMoved when the target has no feed key or the
	// feed is not a catalog feed (missing or merged already).
	MoveFeed(ctx context.Context, feedID int64, target string) (FeedMove, error)
}

var _ PodcastCatalogRepo = (*SQLite)(nil)

// validFeedURL reports whether url has a feed key.
func validFeedURL(url string) bool {
	_, err := podcast.FeedKey(url)
	return err == nil
}

// canonicalFeedKey returns the canonical key a key or alias denotes in the
// catalog, or "" when it denotes no catalog feed.
func canonicalFeedKey(ctx context.Context, q sqlQueryRower, key string) (string, error) {
	var canonical string
	err := q.QueryRowContext(ctx, `SELECT feed_key FROM podcast_feeds WHERE feed_key = ? AND merged_into IS NULL
UNION ALL SELECT feed_key FROM podcast_feed_aliases WHERE alias_key = ? LIMIT 1`, key, key).Scan(&canonical)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve feed key: %w", err)
	}
	return canonical, nil
}

// resolveFeedKey maps a key a client sent to the key its register is kept
// under: the catalog's canonical key when the key, an alias, or the key of
// one of the sent urls denotes a catalog feed; otherwise the key itself (a
// state-only key, which joins the catalog once a feed with that key
// exists). ok is false for a legacy key the catalog does not know.
func resolveFeedKey(ctx context.Context, q sqlQueryRower, key string, urls []string) (string, bool, error) {
	candidates := []string{key}
	for _, u := range urls {
		if k, err := podcast.FeedKey(u); err == nil {
			candidates = append(candidates, k)
		}
	}
	for _, c := range candidates {
		canonical, err := canonicalFeedKey(ctx, q, c)
		if err != nil || canonical != "" {
			return canonical, err == nil, err
		}
	}
	if strings.HasPrefix(key, legacyFeedPrefix) {
		return "", false, nil
	}
	return key, true, nil
}

// GetFeedByKey implements PodcastCatalogRepo.
func (s *SQLite) GetFeedByKey(ctx context.Context, key string) (*model.PodcastFeed, error) {
	canonical, err := canonicalFeedKey(ctx, s.db, key)
	if err != nil || canonical == "" {
		return nil, err
	}
	feed, err := scanFeed(s.db.QueryRowContext(ctx,
		`SELECT `+feedColumns+` FROM podcast_feeds WHERE feed_key = ? AND merged_into IS NULL`, canonical))
	if err != nil {
		return nil, fmt.Errorf("get feed by key: %w", err)
	}
	return feed, nil
}

// ensureCatalogFeed creates the catalog row of a subscribed feed key that
// has none (§7.3): in the shared podcast set, created_by the subscriber,
// never auto-downloading, without folders, permissions or fetching; the
// feed checker fetches it later. Beyond MaxCatalogFeedsPerUserHour rows per
// user and hour nothing is created; the checker retries deferred rows.
// Legacy keys always have a catalog row and are skipped.
func ensureCatalogFeed(ctx context.Context, tx *sql.Tx, userID int64, sub model.PodcastSubscription, in model.PodcastSyncBatch) error {
	if !strings.HasPrefix(sub.FeedKey, "feed:") {
		return nil
	}
	if canonical, err := canonicalFeedKey(ctx, tx, sub.FeedKey); err != nil || canonical != "" {
		return err
	}
	var recent int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM podcast_catalog_creations WHERE user_id = ? AND created_at > ?`,
		userID, in.Now.Add(-time.Hour)).Scan(&recent); err != nil {
		return fmt.Errorf("count recent catalog feeds: %w", err)
	}
	if recent >= MaxCatalogFeedsPerUserHour {
		return nil
	}
	setID, err := podcastSetID(ctx, tx, in.Now)
	if err != nil {
		return err
	}
	_, err = insertFeed(ctx, tx, &model.PodcastFeed{
		SetID: setID, FeedKey: sub.FeedKey, FeedURL: catalogFetchURL(sub), Title: sub.Title,
		CheckIntervalMinutes: in.CheckIntervalMinutes, CreatedBy: &userID, CreatedAt: in.Now,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO podcast_catalog_creations (user_id, created_at) VALUES (?, ?)`, userID, in.Now)
	return err
}

// catalogFetchURL picks where a new catalog feed is fetched from: the first
// of the register's fetch URL and URLs that has the register's key, else
// https at the key's location. A URL with another key is never used, so a
// subscription cannot point a catalog row at a different feed.
func catalogFetchURL(sub model.PodcastSubscription) string {
	for _, u := range append([]string{sub.FetchURL}, sub.URLs...) {
		if k, err := podcast.FeedKey(u); err == nil && k == sub.FeedKey {
			return u
		}
	}
	return "https://" + strings.TrimPrefix(sub.FeedKey, "feed:")
}

// podcastSetID returns the id of the shared podcast set, creating its row
// (but no directory) when it does not exist yet. Like the service's
// findPodcastSet it prefers a podcast set rooted at "podcast", then any
// podcast set, then a plain set at that root, which becomes the podcast set.
func podcastSetID(ctx context.Context, tx *sql.Tx, now time.Time) (int64, error) {
	var id int64
	var isPodcast bool
	err := tx.QueryRowContext(ctx, `SELECT id, is_podcast FROM sets WHERE is_podcast = 1 OR root_path = ?
ORDER BY is_podcast DESC, root_path = ? DESC, id LIMIT 1`, podcastSetRoot, podcastSetRoot).Scan(&id, &isPodcast)
	if err == nil {
		if !isPodcast {
			_, err = tx.ExecContext(ctx, `UPDATE sets SET is_podcast = 1 WHERE id = ?`, id)
		}
		return id, err
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("find podcast set: %w", err)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO sets (name, root_path, is_podcast, created_at) VALUES (?, ?, 1, ?)`,
		podcastSetRoot, podcastSetRoot, now)
	if err != nil {
		return 0, fmt.Errorf("create podcast set: %w", err)
	}
	return res.LastInsertId()
}

// ClearEpisodeDownloads implements PodcastCatalogRepo.
func (s *SQLite) ClearEpisodeDownloads(ctx context.Context, feedID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE podcast_episodes SET is_downloaded = 0, file_name = NULL
WHERE media_id IS NULL AND feed_id IN
 (SELECT id FROM podcast_feeds WHERE feed_key = (SELECT feed_key FROM podcast_feeds WHERE id = ?))`, feedID)
	if err != nil {
		return fmt.Errorf("clear episode downloads: %w", err)
	}
	return nil
}

// MoveFeed implements PodcastCatalogRepo. All of it happens in one
// transaction: the alias or merge, the re-keying of every user's state and
// the new fetch URL. The redirect target always wins, so a feed moving into
// another moves only its own subscribers.
func (s *SQLite) MoveFeed(ctx context.Context, feedID int64, target string) (FeedMove, error) {
	targetKey, err := podcast.FeedKey(target)
	if err != nil {
		return FeedNotMoved, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FeedNotMoved, fmt.Errorf("move feed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var key string
	err = tx.QueryRowContext(ctx, `SELECT feed_key FROM podcast_feeds WHERE id = ? AND merged_into IS NULL`, feedID).Scan(&key)
	if err == sql.ErrNoRows {
		return FeedNotMoved, nil
	}
	if err != nil {
		return FeedNotMoved, fmt.Errorf("move feed: %w", err)
	}
	move, err := moveFeedKey(ctx, tx, feedID, key, targetKey)
	if err != nil {
		return FeedNotMoved, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE podcast_feeds SET feed_url = ? WHERE id = ?`, target, feedID); err != nil {
		return FeedNotMoved, fmt.Errorf("move feed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return FeedNotMoved, fmt.Errorf("move feed: %w", err)
	}
	return move, nil
}

// moveFeedKey applies the identity side of a move of the catalog feed
// feedID (key) to a URL with targetKey:
//   - the target denotes the feed already: nothing to record;
//   - it denotes another feed (a catalog row, or a key whose row was
//     removed while its aliases remain): the feed is merged into it;
//   - the feed has a legacy key: the key is retired, targetKey becomes the
//     feed's key and the legacy key its alias (§3.1);
//   - otherwise targetKey becomes an alias of the feed, and state users
//     kept under targetKey (a state-only key) moves to the feed.
func moveFeedKey(ctx context.Context, tx *sql.Tx, feedID int64, key, targetKey string) (FeedMove, error) {
	owner, err := canonicalFeedKey(ctx, tx, targetKey)
	if err != nil {
		return FeedNotMoved, err
	}
	switch {
	case owner == key:
		return FeedMoved, nil
	case owner != "":
		merged, err := mergeFeedInto(ctx, tx, feedID, key, owner)
		if err != nil || !merged {
			return FeedMoved, err
		}
		return FeedMerged, nil
	case strings.HasPrefix(key, legacyFeedPrefix):
		return FeedMoved, rekeyFeed(ctx, tx, key, targetKey)
	}
	added, err := addFeedAlias(ctx, tx, key, targetKey)
	if err != nil || !added {
		return FeedMoved, err
	}
	return FeedMoved, moveFeedState(ctx, tx, targetKey, key)
}

// addFeedAlias records alias, which denotes no catalog feed, as an alias
// of the catalog feed key, unless the feed's alias cap is reached; it
// reports whether the alias was added.
func addFeedAlias(ctx context.Context, tx *sql.Tx, key, alias string) (bool, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM podcast_feed_aliases WHERE feed_key = ?`, key).Scan(&n); err != nil {
		return false, fmt.Errorf("count feed aliases: %w", err)
	}
	if n >= podcast.MaxFeedAliases {
		return false, nil
	}
	return true, putFeedAlias(ctx, tx, alias, key)
}
