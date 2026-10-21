package repository

import (
	"context"
	"database/sql"
	"fmt"

	"codeberg.org/snonux/player/internal/podcast"
)

// Feed and episode merges, legacy key retirement and re-keying of account
// state (docs/podcast-sync-contract.md §3.1, §3.3).
//
// Keys are minted once and never change for the identity they denote, but
// the server may learn later that two keys denote one feed or episode: a
// permanent redirect into another catalog feed, a corrected URL of a
// legacy-feed: row, an ingest that claims a legacy-episode: by its
// enclosure, or an alias that is already the key of another episode. The
// key that loses becomes an alias of the winner and everything kept under
// it moves to the winner in the same transaction:
//
//   - catalog rows: the loser's rows take the winner's key (a losing feed
//     row is marked merged_into and keeps its episodes and server copies,
//     which every lookup by feed key then finds);
//   - aliases: the loser's aliases point at the winner, each under a new
//     change sequence number, so clients pull the new target;
//   - account state of every user: subscription and episode registers are
//     merged into the winner's register by register (§4: the strictly
//     greater clock wins, a tie keeps the winner) and the loser's row is
//     deleted. A merge never adds a row, so it can never exceed a quota.
//
// Only server observations call these functions; keys a client sends are
// only looked up, so no user can merge or redirect another user's feeds.
// For feeds the redirect target always wins, so a feed that redirects into
// another moves only its own subscribers and never rewrites the keys of
// the target's subscribers. Nothing is deleted except state rows that were
// merged into their winner.

// FeedMove is the outcome of MoveFeed.
type FeedMove int

const (
	// FeedNotMoved: the target had no feed key or the feed is gone.
	FeedNotMoved FeedMove = iota
	// FeedMoved: the feed follows the target (fetch URL, alias, or a
	// retired legacy key) and stays a catalog feed.
	FeedMoved
	// FeedMerged: the target is another catalog feed; the moved feed was
	// merged into it and no longer refreshes.
	FeedMerged
)

// mergeFeedInto merges the catalog feed row feedID (key from) into the feed
// key to, which the redirect target denotes: when to has a catalog row,
// feedID and every row merged into it are marked merged into that row; when
// it has none (its row was removed from the server while aliases stayed),
// feedID becomes the catalog row of to. Either way from becomes an alias of
// to and all state moves (rekeyFeed). It reports whether a row was merged.
func mergeFeedInto(ctx context.Context, tx *sql.Tx, feedID int64, from, to string) (bool, error) {
	var winner int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM podcast_feeds WHERE feed_key = ? AND merged_into IS NULL`, to).Scan(&winner)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("find merge target: %w", err)
	}
	merged := err == nil
	if merged {
		// Flatten: rows already merged into feedID follow it, so no chain
		// of merged_into ever needs to be walked.
		if _, err := tx.ExecContext(ctx, `UPDATE podcast_feeds SET merged_into = ? WHERE id = ? OR merged_into = ?`,
			winner, feedID, feedID); err != nil {
			return false, fmt.Errorf("merge feed: %w", err)
		}
	}
	return merged, rekeyFeed(ctx, tx, from, to)
}

// rekeyFeed makes feed key from an alias of feed key to and moves
// everything kept under from: catalog rows, feed aliases, episode aliases
// and every user's subscription and episode registers. The caller has
// marked from's catalog row merged (or to has no catalog row), so the
// unique index on canonical keys holds.
func rekeyFeed(ctx context.Context, tx *sql.Tx, from, to string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE podcast_feeds SET feed_key = ? WHERE feed_key = ?`, to, from); err != nil {
		return fmt.Errorf("rekey catalog feed: %w", err)
	}
	if err := repointFeedAliases(ctx, tx, from, to); err != nil {
		return err
	}
	if err := moveEpisodeAliases(ctx, tx, from, to); err != nil {
		return err
	}
	if err := mergeAliasedEpisodes(ctx, tx, to); err != nil {
		return err
	}
	if err := moveFeedState(ctx, tx, from, to); err != nil {
		return err
	}
	// Existing target listeners also acquire the retained copies from the
	// losing catalog, without rewriting their subscription/state clocks.
	ids, err := queryInts(ctx, tx, `SELECT e.id FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id WHERE f.feed_key = ?`, to)
	if err != nil {
		return err
	}
	for _, id := range ids {
		var mediaID sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT media_id FROM podcast_episodes WHERE id = ?`, id).Scan(&mediaID); err != nil {
			return err
		}
		if mediaID.Valid {
			if err := seedEpisodeMirrors(ctx, tx, id, mediaID.Int64); err != nil {
				return err
			}
		}
	}
	return nil
}

// repointFeedAliases points every alias of from at to and records from
// itself as an alias of to, each under a new change sequence number. The
// alias cap is not applied here: dropping an alias would strand the keys
// clients still hold, while merges are rare and bounded by the caps of the
// merged feeds.
func repointFeedAliases(ctx context.Context, tx *sql.Tx, from, to string) error {
	// to is canonical from now on, so it cannot stay an alias of anything.
	if _, err := tx.ExecContext(ctx, `DELETE FROM podcast_feed_aliases WHERE alias_key = ?`, to); err != nil {
		return fmt.Errorf("drop feed alias: %w", err)
	}
	aliases, err := queryStrings(ctx, tx, `SELECT alias_key FROM podcast_feed_aliases WHERE feed_key = ? ORDER BY change_seq`, from)
	if err != nil {
		return err
	}
	for _, alias := range append(aliases, from) {
		if err := putFeedAlias(ctx, tx, alias, to); err != nil {
			return err
		}
	}
	return nil
}

// putFeedAlias records (or re-targets) alias → feedKey under a new change
// sequence number.
func putFeedAlias(ctx context.Context, tx *sql.Tx, alias, feedKey string) error {
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO podcast_feed_aliases (alias_key, feed_key, change_seq) VALUES (?, ?, ?)
ON CONFLICT (alias_key) DO UPDATE SET feed_key = excluded.feed_key, change_seq = excluded.change_seq`, alias, feedKey, seq)
	if err != nil {
		return fmt.Errorf("record feed alias: %w", err)
	}
	return nil
}

// moveEpisodeAliases moves the episode aliases recorded under feed key from
// to feed key to. A shared alias denoting different canonical episodes
// proves that those episode keys also need merging, with the older row winning.
func moveEpisodeAliases(ctx context.Context, tx *sql.Tx, from, to string) error {
	pairs, err := queryPairs(ctx, tx, `SELECT alias_key, episode_key FROM podcast_episode_aliases
WHERE feed_key = ? ORDER BY change_seq`, from)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		// An earlier conflicting alias can have merged p's snapshotted
		// target into an older episode. Resolve it again before storing the
		// next mapping, so transferred aliases remain direct, never chains.
		var canonical string
		err := tx.QueryRowContext(ctx, `SELECT episode_key FROM podcast_episode_aliases WHERE feed_key = ? AND alias_key = ?`, to, p[1]).Scan(&canonical)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("resolve transferred episode alias: %w", err)
		}
		if err == nil {
			p[1] = canonical
		}
		if p[0] == p[1] {
			continue
		}
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT episode_key FROM podcast_episode_aliases WHERE feed_key = ? AND alias_key = ?`, to, p[0]).Scan(&existing)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("find conflicting episode alias: %w", err)
		}
		if err == nil {
			if existing != p[1] {
				if err := mergeEpisodeKeys(ctx, tx, to, existing, p[1]); err != nil {
					return err
				}
			}
			continue
		}
		seq, err := nextChangeSeq(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO podcast_episode_aliases (feed_key, alias_key, episode_key, change_seq)
VALUES (?, ?, ?, ?)`, to, p[0], p[1], seq); err != nil {
			return fmt.Errorf("move episode alias: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM podcast_episode_aliases WHERE feed_key = ?`, from); err != nil {
		return fmt.Errorf("move episode aliases: %w", err)
	}
	return nil
}

// moveFeedState moves every user's registers kept under feed key from to
// feed key to (contract §3.3: state rows of the loser are merged into the
// winner with the normal register rules and get new change sequence
// numbers). Episode keys are resolved against to's catalog first, so state
// a user kept under a key that is now an alias joins its episode.
func moveFeedState(ctx context.Context, tx *sql.Tx, from, to string) error {
	users, err := queryInts(ctx, tx, `SELECT user_id FROM podcast_subscriptions WHERE feed_key = ? ORDER BY user_id`, from)
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := moveSubscription(ctx, tx, user, from, to); err != nil {
			return err
		}
	}
	states, err := loadStatesOfFeed(ctx, tx, from)
	if err != nil {
		return err
	}
	if len(states) > 0 {
		known, err := loadKnownEpisodesByKey(ctx, tx, to)
		if err != nil {
			return err
		}
		for _, st := range states {
			target := episodeStateKey{userID: st.key.userID, feedKey: to,
				episodeKey: stateEpisodeKey(known, st.key.episodeKey, st.enclosureURL)}
			if err := moveEpisodeState(ctx, tx, st, target); err != nil {
				return err
			}
		}
	}
	return rekeyAliasedEpisodeStates(ctx, tx, to)
}

// stateEpisodeKey maps an episode key kept in account state to the key of
// the catalog episode it denotes under a feed's catalog: a canonical key
// stays as it is; otherwise it resolves like a key a client sends
// (podcast.ResolveClientEpisode, with the stored enclosure as evidence);
// a key that resolves to nothing stays a state-only key.
func stateEpisodeKey(known []podcast.KnownEpisode, key, enclosureURL string) string {
	for _, ep := range known {
		if ep.Key == key {
			return key
		}
	}
	if canonical, ok := podcast.ResolveClientEpisode(known, key, enclosureURL); ok {
		return canonical
	}
	return key
}

// mergeAliasedEpisodes merges the episodes of a feed key that two canonical
// keys denote: an episode alias whose alias key is itself the key of a
// catalog episode (two catalogs met in a feed merge). Each such pair is
// merged once; a pair that can no longer be merged is skipped.
func mergeAliasedEpisodes(ctx context.Context, tx *sql.Tx, feedKey string) error {
	pairs, err := queryPairs(ctx, tx, `SELECT a.alias_key, a.episode_key FROM podcast_episode_aliases a
WHERE a.feed_key = ?1 AND a.alias_key <> a.episode_key AND EXISTS (SELECT 1 FROM podcast_episodes e
 JOIN podcast_feeds f ON f.id = e.feed_id WHERE f.feed_key = ?1 AND e.episode_key = a.alias_key)
ORDER BY a.change_seq`, feedKey)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		if err := mergeEpisodeKeys(ctx, tx, feedKey, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
}

// mergeEpisodeKeys merges two keys of a feed that denote one episode. The
// older catalog entry (smaller row id) wins; when neither has a catalog
// row, the lexicographically smaller key (contract §3.3). Catalog copies
// stay intact, including two physical episode rows in the same feed.
func mergeEpisodeKeys(ctx context.Context, tx *sql.Tx, feedKey, a, b string) error {
	ageA, err := episodeAge(ctx, tx, feedKey, a)
	if err != nil {
		return err
	}
	ageB, err := episodeAge(ctx, tx, feedKey, b)
	if err != nil {
		return err
	}
	winner, loser := a, b
	if ageB.Valid && (!ageA.Valid || ageB.Int64 < ageA.Int64) || !ageA.Valid && !ageB.Valid && b < a {
		winner, loser = b, a
	}

	return renameEpisodeKey(ctx, tx, feedKey, loser, winner)
}

// episodeAge returns the smallest catalog row id of an episode key under a
// feed key; not valid when no catalog row carries it.
func episodeAge(ctx context.Context, tx *sql.Tx, feedKey, key string) (sql.NullInt64, error) {
	var id sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT MIN(e.id) FROM podcast_episodes e JOIN podcast_feeds f ON f.id = e.feed_id
WHERE f.feed_key = ? AND e.episode_key = ?`, feedKey, key).Scan(&id)
	if err != nil {
		return id, fmt.Errorf("episode age: %w", err)
	}
	return id, nil
}

// renameEpisodeKey makes episode key from of a feed an alias of key to:
// catalog rows carrying from take to (they keep their ids, metadata and
// server copies; when the same feed already holds to, the losing physical
// key stays and reads through its alias), aliases of from point at to, and every user's state
// under from merges into to. It serves episode merges and the retirement
// of legacy-episode: keys (§3.1).
func renameEpisodeKey(ctx context.Context, tx *sql.Tx, feedKey, from, to string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE podcast_episodes SET episode_key = ?1
WHERE episode_key = ?2 AND feed_id IN (SELECT id FROM podcast_feeds WHERE feed_key = ?3)
 AND NOT EXISTS (SELECT 1 FROM podcast_episodes winner
  WHERE winner.feed_id = podcast_episodes.feed_id AND winner.episode_key = ?1)`, to, from, feedKey); err != nil {
		return fmt.Errorf("rename episode key: %w", err)
	}
	// to is canonical from now on, so it cannot stay an alias.
	if _, err := tx.ExecContext(ctx, `DELETE FROM podcast_episode_aliases WHERE feed_key = ? AND alias_key = ?`, feedKey, to); err != nil {
		return fmt.Errorf("drop episode alias: %w", err)
	}
	aliases, err := queryStrings(ctx, tx, `SELECT alias_key FROM podcast_episode_aliases
WHERE feed_key = ? AND episode_key = ? ORDER BY change_seq`, feedKey, from)
	if err != nil {
		return err
	}
	for _, alias := range append(aliases, from) {
		if err := putEpisodeAlias(ctx, tx, feedKey, alias, to); err != nil {
			return err
		}
	}
	return moveEpisodeKeyStates(ctx, tx, feedKey, from, to)
}

// putEpisodeAlias records (or re-targets) an episode alias under a new
// change sequence number.
func putEpisodeAlias(ctx context.Context, tx *sql.Tx, feedKey, alias, episodeKey string) error {
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO podcast_episode_aliases (feed_key, alias_key, episode_key, change_seq)
VALUES (?, ?, ?, ?) ON CONFLICT (feed_key, alias_key) DO UPDATE SET episode_key = excluded.episode_key,
 change_seq = excluded.change_seq`, feedKey, alias, episodeKey, seq)
	if err != nil {
		return fmt.Errorf("record episode alias: %w", err)
	}
	return nil
}

// rekeyAliasedEpisodeStates moves every user's state kept under an episode
// alias of a feed into the alias's episode: state a client stored under a
// key that only later became an alias (a GUID change ingest found).
func rekeyAliasedEpisodeStates(ctx context.Context, tx *sql.Tx, feedKey string) error {
	pairs, err := queryPairs(ctx, tx, `SELECT DISTINCT a.alias_key, a.episode_key FROM podcast_episode_aliases a
JOIN podcast_episode_state s ON s.feed_key = a.feed_key AND s.episode_key = a.alias_key
WHERE a.feed_key = ? ORDER BY a.alias_key`, feedKey)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		if err := moveEpisodeKeyStates(ctx, tx, feedKey, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
}

// moveEpisodeKeyStates merges every user's state of episode key from into
// episode key to, both under feedKey.
func moveEpisodeKeyStates(ctx context.Context, tx *sql.Tx, feedKey, from, to string) error {
	if from == to {
		return nil
	}
	states, err := loadStates(ctx, tx, `WHERE feed_key = ? AND episode_key = ? ORDER BY user_id`, feedKey, from)
	if err != nil {
		return err
	}
	for _, st := range states {
		target := episodeStateKey{userID: st.key.userID, feedKey: feedKey, episodeKey: to}
		if err := moveEpisodeState(ctx, tx, st, target); err != nil {
			return err
		}
	}
	return nil
}
