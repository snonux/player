package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/snonux/player/internal/model"
)

// Per-account subscription registers (docs/podcast-sync-contract.md, §4 and
// §5.1). A register is (subscribed, subscription_ms) plus display data; an
// edit replaces it only with a strictly greater clock, and an unsubscribe
// is a tombstone that is kept forever (bounded by the per-user quota).

const (
	// MaxPodcastSubscriptionsPerUser bounds a user's subscription rows,
	// tombstones included; an edit that would add one more is rejected.
	MaxPodcastSubscriptionsPerUser = 5000
	// MaxCatalogFeedsPerUserHour bounds the catalog rows one user's
	// subscriptions may create per hour (§7.3).
	MaxCatalogFeedsPerUserHour = 20
)

// PodcastSyncRepo synchronizes a user's per-account podcast state.
type PodcastSyncRepo interface {
	// SyncPodcast applies validated subscription and episode edits of one
	// user and pulls that user's changes (or one scoped episode), all in
	// one transaction.
	SyncPodcast(ctx context.Context, userID int64, in model.PodcastSyncBatch) (*model.PodcastSyncResponse, error)
}

// PodcastSubscriptionRepo stores the per-account subscription registers.
type PodcastSubscriptionRepo interface {
	// SetPodcastSubscription writes a server-side edit of the user's
	// register for feed at server time (advanced by 1 ms past the stored
	// clock when needed, so the edit always wins).
	SetPodcastSubscription(ctx context.Context, userID int64, feed *model.PodcastFeed, subscribed bool, now time.Time) error
	// PodcastSubscriptionStates returns the user's registers as feed key →
	// subscribed (false for tombstones).
	PodcastSubscriptionStates(ctx context.Context, userID int64) (map[string]bool, error)
	// HasActivePodcastSubscription reports whether any user is subscribed
	// to the feed key or to one of its aliases.
	HasActivePodcastSubscription(ctx context.Context, feedKey string) (bool, error)
}

var _ PodcastSubscriptionRepo = (*SQLite)(nil)

var _ PodcastSyncRepo = (*SQLite)(nil)

// subscriptionRow is a stored subscription register.
type subscriptionRow struct {
	model.PodcastSubscription
	changeSeq int64
}

// nextChangeSeq takes the next change sequence number inside tx. Every
// written sync row takes its own, so paging never splits or skips one, and
// with SQLite's single writer commit order equals sequence order.
func nextChangeSeq(ctx context.Context, tx sqlQueryRower) (int64, error) {
	var seq int64
	err := tx.QueryRowContext(ctx,
		`UPDATE podcast_sync_seq SET next_seq = next_seq + 1 WHERE id = 1 RETURNING next_seq - 1`).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("next change seq: %w", err)
	}
	return seq, nil
}

// maxChangeSeq is the highest sequence number handed out so far.
func maxChangeSeq(ctx context.Context, q sqlQueryRower) (int64, error) {
	var next int64
	if err := q.QueryRowContext(ctx, `SELECT next_seq FROM podcast_sync_seq WHERE id = 1`).Scan(&next); err != nil {
		return 0, fmt.Errorf("read change seq: %w", err)
	}
	return next - 1, nil
}

// podcastSyncEpoch returns the sync epoch in hex.
func podcastSyncEpoch(ctx context.Context, q sqlQueryRower) (string, error) {
	var epoch []byte
	if err := q.QueryRowContext(ctx, `SELECT value FROM server_secrets WHERE name = ?`,
		podcastSyncEpochSecret).Scan(&epoch); err != nil {
		return "", fmt.Errorf("read sync epoch: %w", err)
	}
	return hex.EncodeToString(epoch), nil
}

// getSubscription returns the user's register for feedKey, or nil.
func getSubscription(ctx context.Context, q sqlQueryRower, userID int64, feedKey string) (*subscriptionRow, error) {
	var r subscriptionRow
	var urls string
	err := q.QueryRowContext(ctx, `SELECT feed_key, subscribed, subscription_ms, title, fetch_url, urls, change_seq
FROM podcast_subscriptions WHERE user_id = ? AND feed_key = ?`, userID, feedKey).Scan(
		&r.FeedKey, &r.Subscribed, &r.SubscriptionMS, &r.Title, &r.FetchURL, &urls, &r.changeSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	if err := json.Unmarshal([]byte(urls), &r.URLs); err != nil || r.URLs == nil {
		r.URLs = []string{}
	}
	return &r, nil
}

// putSubscription stores a register under a fresh change sequence number.
func putSubscription(ctx context.Context, tx *sql.Tx, userID int64, sub model.PodcastSubscription) error {
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return err
	}
	if sub.URLs == nil {
		sub.URLs = []string{}
	}
	urls, err := json.Marshal(sub.URLs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO podcast_subscriptions
(user_id, feed_key, subscribed, subscription_ms, title, fetch_url, urls, change_seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, feed_key) DO UPDATE SET subscribed = excluded.subscribed,
 subscription_ms = excluded.subscription_ms, title = excluded.title, fetch_url = excluded.fetch_url,
 urls = excluded.urls, change_seq = excluded.change_seq`,
		userID, sub.FeedKey, boolToInt(sub.Subscribed), sub.SubscriptionMS, sub.Title, sub.FetchURL, string(urls), seq)
	if err != nil {
		return fmt.Errorf("put subscription: %w", err)
	}
	return nil
}

// SetPodcastSubscription implements PodcastSubscriptionRepo. The register
// keeps the URLs it already knows and adds the feed's fetch URL.
func (s *SQLite) SetPodcastSubscription(ctx context.Context, userID int64, feed *model.PodcastFeed, subscribed bool, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	old, err := getSubscription(ctx, tx, userID, feed.FeedKey)
	if err != nil {
		return err
	}
	sub := model.PodcastSubscription{FeedKey: feed.FeedKey, Subscribed: subscribed,
		SubscriptionMS: now.UnixMilli(), Title: feed.Title, FetchURL: feed.FeedURL, URLs: []string{}}
	if old != nil {
		sub.SubscriptionMS = max(sub.SubscriptionMS, old.SubscriptionMS+1)
		sub.URLs = old.URLs
	}
	sub.URLs = appendURL(sub.URLs, feed.FeedURL)
	if err := putSubscription(ctx, tx, userID, sub); err != nil {
		return err
	}
	return tx.Commit()
}

// appendURL adds url to urls unless it is there already, it has no feed
// key, or the list is full.
func appendURL(urls []string, url string) []string {
	if !validFeedURL(url) || len(urls) >= maxSubscriptionURLs {
		return urls
	}
	for _, u := range urls {
		if u == url {
			return urls
		}
	}
	return append(urls, url)
}

// PodcastSubscriptionStates implements PodcastSubscriptionRepo.
func (s *SQLite) PodcastSubscriptionStates(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT COALESCE(a.feed_key, s.feed_key), s.subscribed FROM podcast_subscriptions s
LEFT JOIN podcast_feed_aliases a ON a.alias_key = s.feed_key
WHERE s.user_id = ? ORDER BY s.subscription_ms, s.feed_key <> COALESCE(a.feed_key, s.feed_key) DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	states := map[string]bool{}
	for rows.Next() {
		var key string
		var subscribed bool
		if err := rows.Scan(&key, &subscribed); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		states[key] = subscribed
	}
	return states, closeRows(rows)
}

// HasActivePodcastSubscription implements PodcastSubscriptionRepo.
func (s *SQLite) HasActivePodcastSubscription(ctx context.Context, feedKey string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM podcast_subscriptions WHERE subscribed = 1
 AND (feed_key = ? OR feed_key IN (SELECT alias_key FROM podcast_feed_aliases WHERE feed_key = ?))`, feedKey, feedKey).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count subscriptions: %w", err)
	}
	return n > 0, nil
}
