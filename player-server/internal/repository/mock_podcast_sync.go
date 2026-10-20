package repository

import (
	"context"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// MockPodcastSyncRepo is a fake PodcastSubscriptionRepo and
// PodcastCatalogRepo. Unset funcs return zero values and no error.
type MockPodcastSyncRepo struct {
	SyncPodcastSubscriptionsFunc     func(ctx context.Context, userID int64, in model.PodcastSubscriptionSync) (*model.PodcastSyncResponse, error)
	SetPodcastSubscriptionFunc       func(ctx context.Context, userID int64, feed *model.PodcastFeed, subscribed bool, now time.Time) error
	PodcastSubscriptionStatesFunc    func(ctx context.Context, userID int64) (map[string]bool, error)
	HasActivePodcastSubscriptionFunc func(ctx context.Context, feedKey string) (bool, error)
	GetFeedByKeyFunc                 func(ctx context.Context, key string) (*model.PodcastFeed, error)
	IngestFeedEpisodesFunc           func(ctx context.Context, feed *model.PodcastFeed, episodes []model.PodcastEpisode, now time.Time) error
	ClearEpisodeDownloadsFunc        func(ctx context.Context, feedID int64) error
	MoveFeedFunc                     func(ctx context.Context, feedID int64, target string) (bool, error)
}

// SyncPodcastSubscriptions implements PodcastSubscriptionRepo.
func (m *MockStore) SyncPodcastSubscriptions(ctx context.Context, userID int64, in model.PodcastSubscriptionSync) (*model.PodcastSyncResponse, error) {
	if f := m.PodcastSyncRepo.SyncPodcastSubscriptionsFunc; f != nil {
		return f(ctx, userID, in)
	}
	return &model.PodcastSyncResponse{}, nil
}

// SetPodcastSubscription implements PodcastSubscriptionRepo.
func (m *MockStore) SetPodcastSubscription(ctx context.Context, userID int64, feed *model.PodcastFeed, subscribed bool, now time.Time) error {
	if f := m.PodcastSyncRepo.SetPodcastSubscriptionFunc; f != nil {
		return f(ctx, userID, feed, subscribed, now)
	}
	return nil
}

// PodcastSubscriptionStates implements PodcastSubscriptionRepo.
func (m *MockStore) PodcastSubscriptionStates(ctx context.Context, userID int64) (map[string]bool, error) {
	if f := m.PodcastSyncRepo.PodcastSubscriptionStatesFunc; f != nil {
		return f(ctx, userID)
	}
	return map[string]bool{}, nil
}

// HasActivePodcastSubscription implements PodcastSubscriptionRepo.
func (m *MockStore) HasActivePodcastSubscription(ctx context.Context, feedKey string) (bool, error) {
	if f := m.PodcastSyncRepo.HasActivePodcastSubscriptionFunc; f != nil {
		return f(ctx, feedKey)
	}
	return false, nil
}

// GetFeedByKey implements PodcastCatalogRepo.
func (m *MockStore) GetFeedByKey(ctx context.Context, key string) (*model.PodcastFeed, error) {
	if f := m.PodcastSyncRepo.GetFeedByKeyFunc; f != nil {
		return f(ctx, key)
	}
	return nil, nil
}

// IngestFeedEpisodes implements PodcastCatalogRepo.
func (m *MockStore) IngestFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, episodes []model.PodcastEpisode, now time.Time) error {
	if f := m.PodcastSyncRepo.IngestFeedEpisodesFunc; f != nil {
		return f(ctx, feed, episodes, now)
	}
	return nil
}

// ClearEpisodeDownloads implements PodcastCatalogRepo.
func (m *MockStore) ClearEpisodeDownloads(ctx context.Context, feedID int64) error {
	if f := m.PodcastSyncRepo.ClearEpisodeDownloadsFunc; f != nil {
		return f(ctx, feedID)
	}
	return nil
}

// MoveFeed implements PodcastCatalogRepo.
func (m *MockStore) MoveFeed(ctx context.Context, feedID int64, target string) (bool, error) {
	if f := m.PodcastSyncRepo.MoveFeedFunc; f != nil {
		return f(ctx, feedID, target)
	}
	return false, nil
}
