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

// SyncPodcastSubscriptions implements PodcastSubscriptionRepo. Edits are
// applied first (identity resolution, register merge, catalog rows), then
// the user's changes are pulled, all in one transaction, so the pull sees
// the edits and a page never contains half of another writer's changes.
func (s *SQLite) SyncPodcastSubscriptions(ctx context.Context, userID int64, in model.PodcastSubscriptionSync) (*model.PodcastSyncResponse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("podcast sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	out := newPodcastSyncResponse()
	if out.Epoch, err = podcastSyncEpoch(ctx, tx); err != nil {
		return nil, err
	}
	var edited []string
	for _, e := range in.Edits {
		res, err := applySubscriptionEdit(ctx, tx, userID, e, in)
		if err != nil {
			return nil, err
		}
		if res.rejected != "" {
			out.Rejected = append(out.Rejected, model.PodcastSyncRejection{EditID: e.EditID, Reason: res.rejected})
			continue
		}
		out.Acked = append(out.Acked, e.EditID)
		edited = append(edited, res.key)
		if res.key != e.FeedKey {
			out.Resolved = append(out.Resolved, model.PodcastKeyMapping{Kind: model.PodcastKeyKindFeed, From: e.FeedKey, To: res.key})
		}
	}
	aliases := newAliasSet(out)
	if err := pullPodcastChanges(ctx, tx, userID, in, out, aliases); err != nil {
		return nil, err
	}
	if err := addEditedSubscriptions(ctx, tx, userID, edited, out, aliases); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("podcast sync: %w", err)
	}
	return out, nil
}

// newPodcastSyncResponse returns a response whose lists encode as [].
func newPodcastSyncResponse() *model.PodcastSyncResponse {
	return &model.PodcastSyncResponse{
		Acked: []string{}, Rejected: []model.PodcastSyncRejection{}, Clamped: []string{},
		Resolved: []model.PodcastKeyMapping{}, Subscriptions: []model.PodcastSubscription{},
		Episodes: []model.PodcastEpisodeState{}, Aliases: []model.PodcastKeyMapping{},
	}
}

// applySubscriptionEdit merges one edit into the user's register. An edit
// whose clock is not greater than the stored one is acknowledged without
// change (a retry, or superseded). A new row beyond the per-user quota is
// rejected. Whenever the register ends up subscribed, a missing catalog row
// is created, so a retried subscribe also repairs a skipped creation.
func applySubscriptionEdit(ctx context.Context, tx *sql.Tx, userID int64, e model.PodcastSubscriptionEdit, in model.PodcastSubscriptionSync) (subscriptionApply, error) {
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
// pull did not return already: a client replaces an acked pending register
// with exactly this row.
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
