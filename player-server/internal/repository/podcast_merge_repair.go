package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/snonux/player/internal/podcast"
)

// repairPodcastIdentities runs on every start, after the podcast migration
// and the indexes, and brings databases written before merges existed (or
// by an interrupted admin removal) into the shape merges maintain. It is
// idempotent and runs in one transaction:
//
//  1. Catalog rows whose merged_into names a row that no longer exists
//     (the winner was removed from the server by an older release that
//     deleted only one row) are re-attached: to the canonical row of their
//     key when there is one, else the oldest of them becomes canonical, so
//     their episodes and server copies stay listed and removable.
//  2. Account state kept under a key that is an alias by now (aliases
//     recorded before state followed them) is merged into the alias's
//     target, as a new alias does today.
func repairPodcastIdentities(db *sql.DB) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repair podcast identities: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	reattached, err := reattachDanglingMerges(ctx, tx)
	if err != nil {
		return err
	}
	if err := retireCorrectedLegacyFeeds(ctx, tx); err != nil {
		return err
	}
	rekeyed, err := rekeyAliasedState(ctx, tx)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repair podcast identities: %w", err)
	}
	if reattached+rekeyed > 0 {
		slog.Warn("repository repaired podcast identities",
			"reattached_feed_rows", reattached, "rekeyed_state_keys", rekeyed)
	}
	return nil
}

// reattachDanglingMerges is step 1 and returns how many rows it changed.
// Rows are visited oldest first, so the oldest row of a key without a
// canonical row is the one promoted.
func reattachDanglingMerges(ctx context.Context, tx *sql.Tx) (int, error) {
	ids, err := queryInts(ctx, tx, `SELECT id FROM podcast_feeds f WHERE merged_into IS NOT NULL
AND NOT EXISTS (SELECT 1 FROM podcast_feeds w WHERE w.id = f.merged_into) ORDER BY id`)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		// NULL (promotion) when the key has no canonical row yet.
		if _, err := tx.ExecContext(ctx, `UPDATE podcast_feeds SET merged_into = (SELECT w.id FROM podcast_feeds w
WHERE w.feed_key = podcast_feeds.feed_key AND w.merged_into IS NULL) WHERE id = ?`, id); err != nil {
			return 0, fmt.Errorf("reattach merged feed row %d: %w", id, err)
		}
	}
	return len(ids), nil
}

// rekeyAliasedState is step 2 and returns how many keys it moved state
// from: feed aliases first (which also moves episode state under aliases
// of the target feed), then episode aliases of feeds whose state was not
// moved yet.
func rekeyAliasedState(ctx context.Context, tx *sql.Tx) (int, error) {
	feeds, err := queryPairs(ctx, tx, `SELECT a.alias_key, a.feed_key FROM podcast_feed_aliases a
WHERE EXISTS (SELECT 1 FROM podcast_subscriptions s WHERE s.feed_key = a.alias_key)
 OR EXISTS (SELECT 1 FROM podcast_episode_state t WHERE t.feed_key = a.alias_key) ORDER BY a.change_seq`)
	if err != nil {
		return 0, err
	}
	for _, p := range feeds {
		if err := moveFeedState(ctx, tx, p[0], p[1]); err != nil {
			return 0, err
		}
	}
	episodeFeeds, err := queryStrings(ctx, tx, `SELECT DISTINCT a.feed_key FROM podcast_episode_aliases a
JOIN podcast_episode_state s ON s.feed_key = a.feed_key AND s.episode_key = a.alias_key ORDER BY a.feed_key`)
	if err != nil {
		return 0, err
	}
	for _, feedKey := range episodeFeeds {
		if err := rekeyAliasedEpisodeStates(ctx, tx, feedKey); err != nil {
			return 0, err
		}
	}
	return len(feeds) + len(episodeFeeds), nil
}

// retireCorrectedLegacyFeeds also repairs URLs corrected by older code,
// before legacy retirement was supported. No redirect is required when the
// stored fetch URL itself has become canonicalizable.
func retireCorrectedLegacyFeeds(ctx context.Context, tx *sql.Tx) error {
	ids, err := queryInts(ctx, tx, `SELECT id FROM podcast_feeds
WHERE merged_into IS NULL AND feed_key LIKE 'legacy-feed:%' ORDER BY id`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		var key, url string
		if err := tx.QueryRowContext(ctx, `SELECT feed_key, feed_url FROM podcast_feeds WHERE id = ?`, id).Scan(&key, &url); err != nil {
			return err
		}
		if target, err := podcast.FeedKey(url); err == nil {
			if _, err := moveFeedKey(ctx, tx, id, key, target); err != nil {
				return err
			}
		}
	}
	return nil
}
