package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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
	userID int64
	feeds  map[string]resolvedFeed
	known  map[string][]podcast.KnownEpisode // by canonical feed key
	// subs are the user's subscription registers (key and urls), loaded
	// once when a feed key needs resolving through them.
	subs []subscriptionURLs
}

// subscriptionURLs is the identity evidence of one subscription register.
type subscriptionURLs struct {
	feedKey string
	urls    []string
}

// resolvedFeed is the resolution of one feed key a client sent.
type resolvedFeed struct {
	key string
	ok  bool
}

// newEpisodeResolver returns a resolver for the user's keys that already
// knows the feed mappings the request's subscription edits applied, so an
// episode sent under a subscription's old key follows its subscription.
func newEpisodeResolver(userID int64, resolved []model.PodcastKeyMapping) *episodeResolver {
	rv := &episodeResolver{userID: userID, feeds: map[string]resolvedFeed{}, known: map[string][]podcast.KnownEpisode{}}
	for _, m := range resolved {
		if m.Kind == model.PodcastKeyKindFeed {
			rv.feeds[m.From] = resolvedFeed{key: m.To, ok: true}
		}
	}
	return rv
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
	} else {
		// Aliases survive catalog removal. In particular a retired legacy
		// key remains accepted while its episode is temporarily state-only.
		var canonical string
		err := tx.QueryRowContext(ctx, `SELECT episode_key FROM podcast_episode_aliases WHERE feed_key = ? AND alias_key = ?`, feed.key, episodeKey).Scan(&canonical)
		if err != nil && err != sql.ErrNoRows {
			return k, false, fmt.Errorf("resolve episode alias: %w", err)
		}
		if err == nil {
			k.episodeKey = canonical
		} else if strings.HasPrefix(episodeKey, legacyEpisodePrefix) {
			return k, false, nil
		}
	}
	return k, true, nil
}

// feed resolves a feed key as contract §3.3 resolves a subscription's key:
// as a canonical key or alias, else through the user's subscription
// register for it (its key and each of its urls). That register is found
// under the key itself or under the canonical key it was resolved to,
// whose urls then include a URL of the sent key. Only the caller's own
// registers are consulted, so no other user's evidence links keys.
func (rv *episodeResolver) feed(ctx context.Context, tx *sql.Tx, key string) (resolvedFeed, error) {
	if f, ok := rv.feeds[key]; ok {
		return f, nil
	}
	candidates, err := rv.feedCandidates(ctx, tx, key)
	if err != nil {
		return resolvedFeed{}, err
	}
	f := resolvedFeed{key: key, ok: !strings.HasPrefix(key, legacyFeedPrefix)}
	for _, c := range candidates {
		canonical, err := canonicalFeedKey(ctx, tx, c)
		if err != nil {
			return resolvedFeed{}, err
		}
		if canonical != "" {
			f = resolvedFeed{key: canonical, ok: true}
			break
		}
	}
	rv.feeds[key] = f
	return f, nil
}

// feedCandidates lists the keys a sent feed key may denote: the key, then
// the key and url keys of every subscription register of the user that is
// kept under it or lists a URL with that key.
func (rv *episodeResolver) feedCandidates(ctx context.Context, tx *sql.Tx, key string) ([]string, error) {
	if canonical, err := canonicalFeedKey(ctx, tx, key); err != nil || canonical != "" {
		return []string{key}, err
	}
	if err := rv.loadSubscriptions(ctx, tx); err != nil {
		return nil, err
	}
	candidates := []string{key}
	for _, sub := range rv.subs {
		urlKeys := make([]string, 0, len(sub.urls))
		matches := sub.feedKey == key
		for _, u := range sub.urls {
			if k, err := podcast.FeedKey(u); err == nil {
				urlKeys = append(urlKeys, k)
				matches = matches || k == key
			}
		}
		if matches {
			candidates = append(append(candidates, sub.feedKey), urlKeys...)
		}
	}
	return candidates, nil
}

// loadSubscriptions reads the user's subscription registers once.
func (rv *episodeResolver) loadSubscriptions(ctx context.Context, tx *sql.Tx) error {
	if rv.subs != nil {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT feed_key, urls FROM podcast_subscriptions WHERE user_id = ?`, rv.userID)
	if err != nil {
		return fmt.Errorf("load subscription urls: %w", err)
	}
	rv.subs = []subscriptionURLs{}
	for rows.Next() {
		var sub subscriptionURLs
		var urls string
		if err := rows.Scan(&sub.feedKey, &urls); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan subscription urls: %w", err)
		}
		_ = json.Unmarshal([]byte(urls), &sub.urls) // a broken list is no evidence
		rv.subs = append(rv.subs, sub)
	}
	return closeRows(rows)
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
	rows, err := tx.QueryContext(ctx, `SELECT COALESCE(a.episode_key, e.episode_key), e.enclosure_key FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id
LEFT JOIN podcast_episode_aliases a ON a.feed_key = f.feed_key AND a.alias_key = e.episode_key
WHERE f.feed_key = ? ORDER BY e.id`, feedKey)
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
		run.episodeKeys[[2]string{item.FeedKey, item.EpisodeKey}] = k
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
// When the request edited the same keys, their resolution (which had the
// item's enclosure evidence) is reused. A scope that resolves to nothing
// stored returns no episode row.
func (run *syncRun) pullScope(ctx context.Context, rv *episodeResolver, aliases aliasSet) error {
	scope := run.in.Scope
	k, edited := run.episodeKeys[[2]string{scope.FeedKey, scope.EpisodeKey}]
	if !edited {
		var ok bool
		var err error
		if k, ok, err = rv.resolve(ctx, run.tx, scope.FeedKey, scope.EpisodeKey, ""); err != nil || !ok {
			return err
		}
		k.userID = run.userID
	}
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
