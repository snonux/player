package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
)

// Episode edits of POST /api/v1/podcasts/sync (docs/podcast-sync-contract.md
// §6.2): identity resolution of the keys a client sent (§3.3), the per-user
// quota, the register merge (§4) and the mirror update (§7.1), all inside
// the request's transaction.

// MaxPodcastEpisodeStatesPerUser bounds a user's episode state rows; an
// edit that would add one more is rejected with quota.
const MaxPodcastEpisodeStatesPerUser = 200000

// episodeResolver maps the keys a client sent to the keys the user's state
// is kept under, caching each feed's resolution and catalog episodes for
// the rest of the request.
type episodeResolver struct {
	feeds map[string]resolvedFeed
	known map[string][]podcast.KnownEpisode // by canonical feed key
}

// resolvedFeed is the resolution of one feed key a client sent.
type resolvedFeed struct {
	key string
	ok  bool
}

func newEpisodeResolver() *episodeResolver {
	return &episodeResolver{feeds: map[string]resolvedFeed{}, known: map[string][]podcast.KnownEpisode{}}
}

// resolve returns the state key (without user) of an episode a client
// sent: the canonical feed key, and the canonical key of the catalog
// episode podcast.ResolveClientEpisode finds, else the episode key itself
// as a state-only key. ok is false for a legacy key the catalog does not
// know, which can never be stored.
func (rv *episodeResolver) resolve(ctx context.Context, tx *sql.Tx, feedKey, episodeKey, enclosureURL string) (episodeStateKey, bool, error) {
	feed, err := rv.feed(ctx, tx, feedKey)
	if err != nil || !feed.ok {
		return episodeStateKey{}, false, err
	}
	known, err := rv.episodes(ctx, tx, feed.key)
	if err != nil {
		return episodeStateKey{}, false, err
	}
	k := episodeStateKey{feedKey: feed.key, episodeKey: episodeKey}
	if canonical, found := podcast.ResolveClientEpisode(known, episodeKey, enclosureURL); found {
		k.episodeKey = canonical
	} else if strings.HasPrefix(episodeKey, legacyEpisodePrefix) {
		return k, false, nil
	}
	return k, true, nil
}

// feed resolves a feed key like a subscription edit without urls.
func (rv *episodeResolver) feed(ctx context.Context, tx *sql.Tx, key string) (resolvedFeed, error) {
	if f, ok := rv.feeds[key]; ok {
		return f, nil
	}
	canonical, ok, err := resolveFeedKey(ctx, tx, key, nil)
	if err != nil {
		return resolvedFeed{}, err
	}
	rv.feeds[key] = resolvedFeed{key: canonical, ok: ok}
	return rv.feeds[key], nil
}

// episodes returns the catalog episodes of a canonical feed key with their
// aliases; none for a state-only feed key.
func (rv *episodeResolver) episodes(ctx context.Context, tx *sql.Tx, feedKey string) ([]podcast.KnownEpisode, error) {
	if known, ok := rv.known[feedKey]; ok {
		return known, nil
	}
	known, err := loadKnownEpisodesByKey(ctx, tx, feedKey)
	if err != nil {
		return nil, err
	}
	rv.known[feedKey] = known
	return known, nil
}

// loadKnownEpisodesByKey lists the catalog episodes of every catalog row
// with the feed key (merged rows share it), each key once, oldest first.
func loadKnownEpisodesByKey(ctx context.Context, tx *sql.Tx, feedKey string) ([]podcast.KnownEpisode, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.episode_key, e.enclosure_key FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id WHERE f.feed_key = ? ORDER BY e.id`, feedKey)
	if err != nil {
		return nil, fmt.Errorf("load catalog episodes: %w", err)
	}
	known := []podcast.KnownEpisode{}
	index := map[string]int{}
	for rows.Next() {
		var ep podcast.KnownEpisode
		if err := rows.Scan(&ep.Key, &ep.EnclosureKey); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan catalog episode: %w", err)
		}
		if _, dup := index[ep.Key]; !dup {
			index[ep.Key] = len(known)
			known = append(known, ep)
		}
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return known, loadEpisodeAliases(ctx, tx, feedKey, known, index)
}

// episodeQuota counts the user's episode state rows once per request and
// keeps the count up to date as the request adds rows.
type episodeQuota struct {
	count  int
	loaded bool
}

// full reports whether the user has no room for another row.
func (q *episodeQuota) full(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	if !q.loaded {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM podcast_episode_state WHERE user_id = ?`,
			userID).Scan(&q.count); err != nil {
			return false, fmt.Errorf("count episode states: %w", err)
		}
		q.loaded = true
	}
	return q.count >= MaxPodcastEpisodeStatesPerUser, nil
}

// applyEpisodeEdits merges every episode item. Both edits of an item share
// its keys, so a key problem rejects both; a stale edit is acked without
// change.
func (run *syncRun) applyEpisodeEdits(ctx context.Context, rv *episodeResolver) error {
	quota := &episodeQuota{}
	for _, item := range run.in.Episodes {
		ids := episodeEditIDs(item)
		k, ok, err := rv.resolve(ctx, run.tx, item.FeedKey, item.EpisodeKey, item.EnclosureURL)
		if err != nil {
			return err
		}
		if !ok {
			run.rejectAll(ids, model.PodcastRejectInvalidKey)
			continue
		}
		k.userID = run.userID
		reason, err := run.mergeEpisodeItem(ctx, k, item, quota)
		if err != nil {
			return err
		}
		if reason != "" {
			run.rejectAll(ids, reason)
			continue
		}
		run.out.Acked = append(run.out.Acked, ids...)
		run.editedEpisodes = append(run.editedEpisodes, k)
		run.resolveEpisode(item.FeedKey, item.EpisodeKey, k)
	}
	return nil
}

// mergeEpisodeItem merges one item's registers into k, returning a
// rejection reason when a new row would exceed the user's quota.
func (run *syncRun) mergeEpisodeItem(ctx context.Context, k episodeStateKey, item model.PodcastEpisodeEdit, quota *episodeQuota) (string, error) {
	_, exists, err := loadRegisters(ctx, run.tx, k)
	if err != nil {
		return "", err
	}
	if !exists {
		if full, err := quota.full(ctx, run.tx, run.userID); err != nil || full {
			return model.PodcastRejectQuota, err
		}
	}
	m, err := mergeEpisodeEdit(ctx, run.tx, k, episodeEditOf(item), item.EnclosureURL)
	if err != nil {
		return "", err
	}
	if !m.existed && m.changed() {
		quota.count++
	}
	return "", nil
}

// episodeEditOf turns a validated wire item into a register edit.
func episodeEditOf(item model.PodcastEpisodeEdit) episodeEdit {
	var e episodeEdit
	if p := item.Position; p != nil {
		e = positionEdit(p.Seconds, p.ObservedMS)
	}
	if c := item.Completion; c != nil {
		e = e.withCompletion(c.Completed, c.Source, c.ObservedMS)
	}
	return e
}

// episodeEditIDs returns the edit ids of an item's registers.
func episodeEditIDs(item model.PodcastEpisodeEdit) []string {
	var ids []string
	if item.Position != nil {
		ids = append(ids, item.Position.EditID)
	}
	if item.Completion != nil {
		ids = append(ids, item.Completion.EditID)
	}
	return ids
}

// rejectAll rejects every edit id with reason.
func (run *syncRun) rejectAll(ids []string, reason string) {
	for _, id := range ids {
		run.reject(id, reason)
	}
}

// resolveEpisode reports how the keys a client sent were mapped: the feed
// key, if it changed, and the episode key under the canonical feed key.
func (run *syncRun) resolveEpisode(feedKey, episodeKey string, k episodeStateKey) {
	run.resolve(model.PodcastKeyMapping{Kind: model.PodcastKeyKindFeed, From: feedKey, To: k.feedKey})
	run.resolve(model.PodcastKeyMapping{Kind: model.PodcastKeyKindEpisode, FeedKey: k.feedKey,
		From: episodeKey, To: k.episodeKey})
}

// pullScope answers a scoped request (the pre-playback request, §8.1):
// the current registers of that one episode and its aliases, no cursor.
// A scope that resolves to nothing stored returns no episode row.
func (run *syncRun) pullScope(ctx context.Context, rv *episodeResolver, aliases aliasSet) error {
	scope := run.in.Scope
	k, ok, err := rv.resolve(ctx, run.tx, scope.FeedKey, scope.EpisodeKey, "")
	if err != nil || !ok {
		return err
	}
	k.userID = run.userID
	run.resolveEpisode(scope.FeedKey, scope.EpisodeKey, k)
	return addEditedEpisodes(ctx, run.tx, []episodeStateKey{k}, run.out, aliases)
}

// addEditedEpisodes adds the current registers (and aliases) of every key
// the response does not carry yet.
func addEditedEpisodes(ctx context.Context, tx *sql.Tx, keys []episodeStateKey, out *model.PodcastSyncResponse, aliases aliasSet) error {
	type pair struct{ feed, episode string }
	have := map[pair]bool{}
	for _, ep := range out.Episodes {
		have[pair{ep.FeedKey, ep.EpisodeKey}] = true
	}
	for _, k := range keys {
		if have[pair{k.feedKey, k.episodeKey}] {
			continue
		}
		have[pair{k.feedKey, k.episodeKey}] = true
		state, err := getEpisodeState(ctx, tx, k.userID, k.feedKey, k.episodeKey)
		if err != nil {
			return err
		}
		if state == nil || (state.Position == nil && state.Completion == nil) {
			continue
		}
		out.Episodes = append(out.Episodes, *state)
		if err := addEpisodeAliases(ctx, tx, k.feedKey, k.episodeKey, aliases); err != nil {
			return err
		}
	}
	return nil
}
