package repository

import (
	"context"
	"database/sql"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// DefaultPodcastSyncLimit is the page size of a pull without a limit.
const DefaultPodcastSyncLimit = 500

// pullRowsQuery lists the caller's changed rows after a cursor, across the
// four tables that share the change sequence, in sequence order. Alias rows
// are visible only when their target is a feed or episode the caller has a
// subscription or state row for, so no other user's feeds leak.
const pullRowsQuery = `SELECT kind, change_seq, k1, k2, k3 FROM (
 SELECT 's' AS kind, change_seq, feed_key AS k1, '' AS k2, '' AS k3
  FROM podcast_subscriptions WHERE user_id = ?1 AND change_seq > ?2
 UNION ALL
 SELECT 'e', change_seq, feed_key, episode_key, ''
  FROM podcast_episode_state WHERE user_id = ?1 AND change_seq > ?2 AND (position_ms > 0 OR completion_ms > 0)
 UNION ALL
 SELECT 'f', change_seq, alias_key, feed_key, '' FROM podcast_feed_aliases a WHERE change_seq > ?2 AND (
  EXISTS (SELECT 1 FROM podcast_subscriptions s WHERE s.user_id = ?1 AND s.feed_key = a.feed_key)
  OR EXISTS (SELECT 1 FROM podcast_episode_state t WHERE t.user_id = ?1 AND t.feed_key = a.feed_key))
 UNION ALL
 SELECT 'a', change_seq, feed_key, alias_key, episode_key FROM podcast_episode_aliases a WHERE change_seq > ?2 AND (
  EXISTS (SELECT 1 FROM podcast_subscriptions s WHERE s.user_id = ?1 AND s.feed_key = a.feed_key)
  OR EXISTS (SELECT 1 FROM podcast_episode_state t
   WHERE t.user_id = ?1 AND t.feed_key = a.feed_key AND t.episode_key = a.episode_key))
) ORDER BY change_seq LIMIT ?3`

// pulledRow is one row of pullRowsQuery.
type pulledRow struct {
	kind       string
	seq        int64
	k1, k2, k3 string
}

// pullPodcastChanges adds one page of the user's changes to out. A foreign
// epoch (database restored or replaced) or a cursor beyond the highest
// sequence number restarts the pull from 0, so a client resynchronizes in
// full instead of missing rows.
func pullPodcastChanges(ctx context.Context, tx *sql.Tx, userID int64, in model.PodcastSyncBatch, out *model.PodcastSyncResponse, aliases aliasSet) error {
	since, err := effectiveSince(ctx, tx, in, out.Epoch)
	if err != nil {
		return err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultPodcastSyncLimit
	}
	rows, err := queryPulledRows(ctx, tx, userID, since, limit+1)
	if err != nil {
		return err
	}
	next, hasMore := since, len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	for _, r := range rows {
		if err := addPulledRow(ctx, tx, userID, r, out, aliases); err != nil {
			return err
		}
		next = r.seq
	}
	out.NextSince, out.HasMore = &next, &hasMore
	return nil
}

// effectiveSince returns the cursor the pull starts after.
func effectiveSince(ctx context.Context, tx *sql.Tx, in model.PodcastSyncBatch, epoch string) (int64, error) {
	if in.Epoch != epoch || in.Since < 0 {
		return 0, nil
	}
	highest, err := maxChangeSeq(ctx, tx)
	if err != nil || in.Since > highest {
		return 0, err
	}
	return in.Since, nil
}

// queryPulledRows reads the changed rows; they are read completely before
// any of them is resolved, so no two statements run at once on the
// transaction's connection.
func queryPulledRows(ctx context.Context, tx *sql.Tx, userID, since int64, limit int) ([]pulledRow, error) {
	rows, err := tx.QueryContext(ctx, pullRowsQuery, userID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("pull podcast changes: %w", err)
	}
	var pulled []pulledRow
	for rows.Next() {
		var r pulledRow
		if err := rows.Scan(&r.kind, &r.seq, &r.k1, &r.k2, &r.k3); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan podcast change: %w", err)
		}
		pulled = append(pulled, r)
	}
	return pulled, closeRows(rows)
}

// addPulledRow adds one pulled row, and the aliases every subscription and
// episode row carries, to out.
func addPulledRow(ctx context.Context, tx *sql.Tx, userID int64, r pulledRow, out *model.PodcastSyncResponse, aliases aliasSet) error {
	switch r.kind {
	case "s":
		sub, err := getSubscription(ctx, tx, userID, r.k1)
		if err != nil || sub == nil {
			return err
		}
		out.Subscriptions = append(out.Subscriptions, sub.PodcastSubscription)
		return addFeedAliases(ctx, tx, r.k1, aliases)
	case "e":
		state, err := getEpisodeState(ctx, tx, userID, r.k1, r.k2)
		if err != nil || state == nil {
			return err
		}
		out.Episodes = append(out.Episodes, *state)
		return addEpisodeAliases(ctx, tx, r.k1, r.k2, aliases)
	case "f":
		aliases.add(model.PodcastKeyMapping{Kind: model.PodcastKeyKindFeed, From: r.k1, To: r.k2})
	case "a":
		aliases.add(model.PodcastKeyMapping{Kind: model.PodcastKeyKindEpisode, FeedKey: r.k1, From: r.k2, To: r.k3})
	}
	return nil
}

// aliasSet appends alias mappings to a response once each.
type aliasSet struct {
	out  *model.PodcastSyncResponse
	seen map[model.PodcastKeyMapping]bool
}

func newAliasSet(out *model.PodcastSyncResponse) aliasSet {
	return aliasSet{out: out, seen: map[model.PodcastKeyMapping]bool{}}
}

func (s aliasSet) add(a model.PodcastKeyMapping) {
	if !s.seen[a] {
		s.seen[a] = true
		s.out.Aliases = append(s.out.Aliases, a)
	}
}

// addFeedAliases adds every alias of a feed (at most 16 by the alias cap),
// whatever its sequence number, so a client that subscribes after an alias
// was written still learns it.
func addFeedAliases(ctx context.Context, tx *sql.Tx, feedKey string, aliases aliasSet) error {
	return addAliasRows(ctx, tx, aliases, model.PodcastKeyMapping{Kind: model.PodcastKeyKindFeed, To: feedKey},
		`SELECT alias_key FROM podcast_feed_aliases WHERE feed_key = ? ORDER BY change_seq`, feedKey)
}

// addEpisodeAliases adds every alias of an episode, like addFeedAliases.
func addEpisodeAliases(ctx context.Context, tx *sql.Tx, feedKey, episodeKey string, aliases aliasSet) error {
	return addAliasRows(ctx, tx, aliases,
		model.PodcastKeyMapping{Kind: model.PodcastKeyKindEpisode, FeedKey: feedKey, To: episodeKey},
		`SELECT alias_key FROM podcast_episode_aliases WHERE feed_key = ? AND episode_key = ? ORDER BY change_seq`,
		feedKey, episodeKey)
}

// addAliasRows adds one mapping per alias key the query returns.
func addAliasRows(ctx context.Context, tx *sql.Tx, aliases aliasSet, mapping model.PodcastKeyMapping, query string, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list aliases: %w", err)
	}
	var froms []string
	for rows.Next() {
		var from string
		if err := rows.Scan(&from); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan alias: %w", err)
		}
		froms = append(froms, from)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, from := range froms {
		mapping.From = from
		aliases.add(mapping)
	}
	return nil
}

// getEpisodeState returns the user's registers of one episode, or nil.
// A register that was never set (clock 0) is left out.
func getEpisodeState(ctx context.Context, q sqlQueryRower, userID int64, feedKey, episodeKey string) (*model.PodcastEpisodeState, error) {
	r, exists, err := loadRegisters(ctx, q, episodeStateKey{userID: userID, feedKey: feedKey, episodeKey: episodeKey})
	if err != nil || !exists {
		return nil, err
	}
	state := &model.PodcastEpisodeState{FeedKey: feedKey, EpisodeKey: episodeKey}
	if r.positionMS > 0 {
		state.Position = &model.PodcastPositionRegister{Seconds: r.position, ObservedMS: r.positionMS}
	}
	if r.completionMS > 0 {
		state.Completion = &model.PodcastCompletionRegister{Completed: r.completed, Source: r.completionSource, ObservedMS: r.completionMS}
	}
	return state, nil
}
