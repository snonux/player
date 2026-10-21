package repository

import (
	"context"
	"database/sql"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// subscriptionApply is the outcome of applying one subscription edit.
type subscriptionApply struct {
	key      string // canonical key the register is kept under
	rejected string // rejection reason, "" when acked
}

// syncRun collects the outcome of one sync request: the response, the
// keys whose current rows the response must carry, and the resolutions
// already reported (each mapping once).
type syncRun struct {
	tx             *sql.Tx
	userID         int64
	in             model.PodcastSyncBatch
	out            *model.PodcastSyncResponse
	editedFeeds    []string
	editedEpisodes []episodeStateKey
	resolved       map[model.PodcastKeyMapping]bool
}

// SyncPodcast implements PodcastSyncRepo. Edits are applied first
// (identity resolution, register merge, catalog rows, progress mirrors),
// then the user's changes are pulled, all in one transaction, so the pull
// sees the edits and a page never contains half of another writer's
// changes.
func (s *SQLite) SyncPodcast(ctx context.Context, userID int64, in model.PodcastSyncBatch) (*model.PodcastSyncResponse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("podcast sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	run := &syncRun{tx: tx, userID: userID, in: in, out: newPodcastSyncResponse(),
		resolved: map[model.PodcastKeyMapping]bool{}}
	if run.out.Epoch, err = podcastSyncEpoch(ctx, tx); err != nil {
		return nil, err
	}
	if err := run.applySubscriptionEdits(ctx); err != nil {
		return nil, err
	}
	episodes := newEpisodeResolver()
	if err := run.applyEpisodeEdits(ctx, episodes); err != nil {
		return nil, err
	}
	if err := run.pull(ctx, episodes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("podcast sync: %w", err)
	}
	return run.out, nil
}

// newPodcastSyncResponse returns a response whose lists encode as [].
func newPodcastSyncResponse() *model.PodcastSyncResponse {
	return &model.PodcastSyncResponse{
		Acked: []string{}, Rejected: []model.PodcastSyncRejection{}, Clamped: []string{},
		Resolved: []model.PodcastKeyMapping{}, Subscriptions: []model.PodcastSubscription{},
		Episodes: []model.PodcastEpisodeState{}, Aliases: []model.PodcastKeyMapping{},
	}
}

// reject reports a rejected edit.
func (run *syncRun) reject(editID, reason string) {
	run.out.Rejected = append(run.out.Rejected, model.PodcastSyncRejection{EditID: editID, Reason: reason})
}

// resolve reports a key mapping the server applied, once per request.
func (run *syncRun) resolve(m model.PodcastKeyMapping) {
	if m.From == m.To || run.resolved[m] {
		return
	}
	run.resolved[m] = true
	run.out.Resolved = append(run.out.Resolved, m)
}

// applySubscriptionEdits merges every subscription edit.
func (run *syncRun) applySubscriptionEdits(ctx context.Context) error {
	for _, e := range run.in.Subscriptions {
		res, err := applySubscriptionEdit(ctx, run.tx, run.userID, e, run.in)
		if err != nil {
			return err
		}
		if res.rejected != "" {
			run.reject(e.EditID, res.rejected)
			continue
		}
		run.out.Acked = append(run.out.Acked, e.EditID)
		run.editedFeeds = append(run.editedFeeds, res.key)
		run.resolve(model.PodcastKeyMapping{Kind: model.PodcastKeyKindFeed, From: e.FeedKey, To: res.key})
	}
	return nil
}

// pull adds the user's changes after the cursor, or the scoped episode,
// and then the current row of every key the request edited: a client
// replaces an acked pending register with exactly that row.
func (run *syncRun) pull(ctx context.Context, episodes *episodeResolver) error {
	aliases := newAliasSet(run.out)
	var err error
	if run.in.Scope != nil {
		err = run.pullScope(ctx, episodes, aliases)
	} else {
		err = pullPodcastChanges(ctx, run.tx, run.userID, run.in, run.out, aliases)
	}
	if err != nil {
		return err
	}
	if err := addEditedSubscriptions(ctx, run.tx, run.userID, run.editedFeeds, run.out, aliases); err != nil {
		return err
	}
	return addEditedEpisodes(ctx, run.tx, run.editedEpisodes, run.out, aliases)
}

// applySubscriptionEdit merges one edit into the user's register. An edit
// whose clock is not greater than the stored one is acknowledged without
// change (a retry, or superseded). A new row beyond the per-user quota is
// rejected. Whenever the register ends up subscribed, a missing catalog row
// is created, so a retried subscribe also repairs a skipped creation.
func applySubscriptionEdit(ctx context.Context, tx *sql.Tx, userID int64, e model.PodcastSubscriptionEdit, in model.PodcastSyncBatch) (subscriptionApply, error) {
	key, ok, err := resolveFeedKey(ctx, tx, e.FeedKey, e.URLs)
	if err != nil || !ok {
		return subscriptionApply{rejected: model.PodcastRejectInvalidKey}, err
	}
	old, err := getSubscription(ctx, tx, userID, key)
	if err != nil {
		return subscriptionApply{}, err
	}
	if old == nil {
		if full, err := subscriptionQuotaFull(ctx, tx, userID); err != nil || full {
			return subscriptionApply{rejected: model.PodcastRejectQuota}, err
		}
	}
	current := model.PodcastSubscription{FeedKey: key, Subscribed: e.Subscribed, SubscriptionMS: e.ObservedMS,
		Title: e.Title, FetchURL: e.FetchURL, URLs: e.URLs}
	if old != nil && old.SubscriptionMS >= e.ObservedMS {
		current = old.PodcastSubscription
	} else if err := putSubscription(ctx, tx, userID, current); err != nil {
		return subscriptionApply{}, err
	}
	if current.Subscribed {
		if err := ensureCatalogFeed(ctx, tx, userID, current, in); err != nil {
			return subscriptionApply{}, err
		}
	}
	return subscriptionApply{key: key}, nil
}

// subscriptionQuotaFull reports whether the user has no room for another
// subscription row (tombstones count).
func subscriptionQuotaFull(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM podcast_subscriptions WHERE user_id = ?`,
		userID).Scan(&n); err != nil {
		return false, fmt.Errorf("count subscriptions: %w", err)
	}
	return n >= MaxPodcastSubscriptionsPerUser, nil
}

// addEditedSubscriptions adds the current register of every edited key the
// pull did not return already.
func addEditedSubscriptions(ctx context.Context, tx *sql.Tx, userID int64, keys []string, out *model.PodcastSyncResponse, aliases aliasSet) error {
	have := map[string]bool{}
	for _, sub := range out.Subscriptions {
		have[sub.FeedKey] = true
	}
	for _, key := range keys {
		if have[key] {
			continue
		}
		have[key] = true
		row, err := getSubscription(ctx, tx, userID, key)
		if err != nil {
			return err
		}
		if row != nil {
			out.Subscriptions = append(out.Subscriptions, row.PodcastSubscription)
			if err := addFeedAliases(ctx, tx, key, aliases); err != nil {
				return err
			}
		}
	}
	return nil
}
