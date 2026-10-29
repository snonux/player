package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// Moving account state from a key that lost a merge to the key that won
// (podcast_merge.go). Each move merges the loser's registers into the
// winner's by the register rules of contract §4 (the strictly greater clock
// wins, a tie keeps the winner), stores the winner under a new change
// sequence number, deletes the loser's row and, for episodes, rewrites the
// progress mirrors of the winner's server copies. A move never adds a row,
// so it can never exceed a per-user quota.

// storedEpisodeState is one stored episode state row.
type storedEpisodeState struct {
	key          episodeStateKey
	r            episodeRegisters
	enclosureURL string
}

// loadStatesOfFeed returns every user's episode state kept under feedKey.
func loadStatesOfFeed(ctx context.Context, tx *sql.Tx, feedKey string) ([]storedEpisodeState, error) {
	return loadStates(ctx, tx, `WHERE feed_key = ? ORDER BY user_id, episode_key`, feedKey)
}

// loadStates reads the episode state rows a WHERE clause selects. The rows
// are read completely before the caller writes, so no two statements run
// at once on the transaction's connection.
func loadStates(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]storedEpisodeState, error) {
	rows, err := tx.QueryContext(ctx, `SELECT user_id, feed_key, episode_key, position_seconds, position_ms,
 completed, completion_source, completion_ms, enclosure_url, deleted, deletion_ms FROM podcast_episode_state `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("load episode states: %w", err)
	}
	var states []storedEpisodeState
	for rows.Next() {
		var st storedEpisodeState
		if err := rows.Scan(&st.key.userID, &st.key.feedKey, &st.key.episodeKey, &st.r.position, &st.r.positionMS,
			&st.r.completed, &st.r.completionSource, &st.r.completionMS, &st.enclosureURL, &st.r.deleted, &st.r.deletionMS); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan episode state: %w", err)
		}
		states = append(states, st)
	}
	return states, closeRows(rows)
}

// moveEpisodeState merges the stored row src into the registers of target
// and deletes src. The target keeps its own enclosure evidence when it
// existed already.
func moveEpisodeState(ctx context.Context, tx *sql.Tx, src storedEpisodeState, target episodeStateKey) error {
	if src.key == target {
		return nil
	}
	r, existed, err := loadRegisters(ctx, tx, target)
	if err != nil {
		return err
	}
	merged := r
	merged.setPosition(src.r.position, src.r.positionMS)
	merged.setCompletion(src.r.completed, src.r.completionSource, src.r.completionMS)
	merged.setDeletion(src.r.deleted, src.r.deletionMS)
	if _, err := tx.ExecContext(ctx, `DELETE FROM podcast_episode_state WHERE user_id = ? AND feed_key = ? AND episode_key = ?`,
		src.key.userID, src.key.feedKey, src.key.episodeKey); err != nil {
		return fmt.Errorf("move episode state: %w", err)
	}
	enclosure := src.enclosureURL
	if existed {
		enclosure = ""
	}
	// Even when the target wins unchanged, publish it again: a client
	// holding only the losing row must pull the authoritative tie winner
	// together with the new alias after its existing cursor.
	if err := putEpisodeState(ctx, tx, target, merged, enclosure); err != nil {
		return err
	}
	if merged.positionMS == 0 && merged.completionMS == 0 {
		return nil
	}
	// Server copies of the loser's catalog rows now carry the winner's key,
	// so they are projected from the merged registers too.
	return writeEpisodeMirrors(ctx, tx, target, &merged)
}

// moveSubscription merges the user's subscription register kept under
// from into the one kept under to and deletes the register under from.
// The register with the strictly greater clock wins, with its display
// data; equal clocks keep to's. The URLs of both are kept (to's first, at
// most maxSubscriptionURLs), since they are the evidence episode keys are
// resolved through.
func moveSubscription(ctx context.Context, tx *sql.Tx, userID int64, from, to string) error {
	src, err := getSubscription(ctx, tx, userID, from)
	if err != nil || src == nil {
		return err
	}
	dst, err := getSubscription(ctx, tx, userID, to)
	if err != nil {
		return err
	}
	merged := src.PodcastSubscription
	urls := src.URLs
	if dst != nil {
		if dst.SubscriptionMS >= src.SubscriptionMS {
			merged = dst.PodcastSubscription
		}
		urls = append(append([]string{}, dst.URLs...), src.URLs...)
	}
	merged.FeedKey, merged.URLs = to, []string{}
	for _, u := range urls {
		merged.URLs = appendURL(merged.URLs, u)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM podcast_subscriptions WHERE user_id = ? AND feed_key = ?`, userID, from); err != nil {
		return fmt.Errorf("move subscription: %w", err)
	}
	return putSubscription(ctx, tx, userID, merged)
}

// queryStrings returns the single text column of every row of query.
func queryStrings(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query keys: %w", err)
	}
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan key: %w", err)
		}
		out = append(out, v)
	}
	return out, closeRows(rows)
}

// queryPairs returns the two text columns of every row of query.
func queryPairs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([][2]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query key pairs: %w", err)
	}
	var out [][2]string
	for rows.Next() {
		var p [2]string
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan key pair: %w", err)
		}
		out = append(out, p)
	}
	return out, closeRows(rows)
}

// queryInts returns the single integer column of every row of query.
func queryInts(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ids: %w", err)
	}
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan id: %w", err)
		}
		out = append(out, v)
	}
	return out, closeRows(rows)
}
