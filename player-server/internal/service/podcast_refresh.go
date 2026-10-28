package service

import (
	"context"
	"errors"
	"time"
)

var ErrPodcastRefreshInvalid = errors.New("invalid feed key")
var ErrPodcastRefreshMissing = errors.New("active podcast subscription not found")
var ErrPodcastRefreshBusy = errors.New("podcast refresh busy or waiting to retry")
var ErrPodcastRefreshFailed = errors.New("podcast could not be refreshed; retry later")

type PodcastRefreshResult struct {
	FeedKey string `json:"feed_key"`
	Status  string `json:"status"`
}

type PodcastRefreshService interface {
	RefreshSubscribedFeed(context.Context, int64, string) (*PodcastRefreshResult, error)
}

// RefreshSubscribedFeed fetches only metadata for an account's existing catalog
// subscription. Public outbound policy applies even to admin-created feeds.
// Two slots bound work; duplicate requests fail quickly instead of queuing.
func (s *podcastService) RefreshSubscribedFeed(ctx context.Context, userID int64, key string) (*PodcastRefreshResult, error) {
	if !validFeedKeyShape(key) {
		return nil, ErrPodcastRefreshInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	feed, err := s.store.GetSubscribedPodcastFeed(ctx, userID, key)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		return nil, ErrPodcastRefreshMissing
	}
	s.refreshMu.Lock()
	if s.refreshing[feed.ID] || len(s.refreshing) >= 2 {
		s.refreshMu.Unlock()
		return nil, ErrPodcastRefreshBusy
	}
	refreshID := feed.ID
	s.refreshing[refreshID] = true
	s.refreshMu.Unlock()
	defer func() { s.refreshMu.Lock(); delete(s.refreshing, refreshID); s.refreshMu.Unlock() }()
	// Reload after claiming the slot: a prior request may have just completed.
	feed, err = s.store.GetSubscribedPodcastFeed(ctx, userID, key)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		return nil, ErrPodcastRefreshMissing
	}

	// A move may merge into an empty catalog row. Fetch that winner as well;
	// new-feed-url is an identity statement, not a document for the new URL.
	// Three documents and the original deadline bound chained moves.
	for hop := 0; hop < 3; hop++ {
		// A merge can happen between either pair of ownership lookups. Claim
		// the actual canonical row before inspecting freshness or fetching it.
		if feed.ID != refreshID {
			s.refreshMu.Lock()
			if s.refreshing[feed.ID] {
				s.refreshMu.Unlock()
				return nil, ErrPodcastRefreshBusy
			}
			delete(s.refreshing, refreshID)
			refreshID = feed.ID
			s.refreshing[refreshID] = true
			s.refreshMu.Unlock()
		}
		now := s.clock.Now()
		if feed.NextCheckAt != nil && now.Before(*feed.NextCheckAt) {
			return nil, ErrPodcastRefreshBusy
		}
		if feed.LastCheckedAt != nil && now.Sub(*feed.LastCheckedAt) < time.Minute {
			return &PodcastRefreshResult{feed.FeedKey, "fresh"}, nil
		}
		if err := s.checkFeedWithClient(ctx, *feed, s.restrictedClient, false); err != nil {
			s.logger.Warn("subscription metadata refresh failed", "feed_id", feed.ID, "err", err)
			return nil, ErrPodcastRefreshFailed
		}
		canonical, err := s.store.GetSubscribedPodcastFeed(ctx, userID, key)
		if err != nil {
			return nil, err
		}
		if canonical == nil {
			return nil, ErrPodcastRefreshMissing
		}
		if canonical.ID == refreshID {
			return &PodcastRefreshResult{canonical.FeedKey, "refreshed"}, nil
		}
		feed = canonical
	}
	return nil, ErrPodcastRefreshBusy
}
