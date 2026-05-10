package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
)

// podcastSubscriptionService manages podcast feed subscriptions.
type podcastSubscriptionService struct {
	*podcastService
}

func newPodcastSubscriptionService(svc *podcastService) *podcastSubscriptionService {
	return &podcastSubscriptionService{podcastService: svc}
}
func (s *podcastSubscriptionService) SubscribeFeed(ctx context.Context, feedURL, setName string, userID int64) (*model.PodcastFeed, error) {
	if err := s.verifyAdmin(ctx, userID); err != nil {
		return nil, err
	}

	parsed, err := s.podcastService.parseFeed(s.podcastService.httpClient, feedURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFeed, err)
	}

	set, err := s.ensurePodcastSet(ctx, userID)
	if err != nil {
		return nil, err
	}

	feed, err := s.findExistingFeed(ctx, set.ID, parsed.Title, feedURL)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		feed, err = s.createPodcastFeed(ctx, parsed, set.ID, feedURL)
		if err != nil {
			return nil, err
		}
	} else {
		feed.FeedURL = feedURL
		feed.Title = parsed.Title
		feed.Description = parsed.Description
		feed.ImageURL = parsed.ImageURL
		feed.CheckIntervalMinutes = s.podcastService.checkInterval
	}

	folderPath := filepath.Join(s.podcastService.mediaRoot, set.RootPath, podcastFolderName("", parsed.Title, feed.ID))
	if err := os.MkdirAll(folderPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir podcast folder: %w", err)
	}
	if err := s.podcastService.downloadCover(s.podcastService.httpClient, parsed.ImageURL, folderPath); err != nil {
		s.podcastService.logger.Warn("download cover failed", "error", err, "feed", feed.FeedURL)
	}
	if err := s.insertPodcastEpisodes(ctx, parsed, feed.ID); err != nil {
		s.podcastService.logger.Warn("insert podcast episodes failed", "error", err, "feed", feed.FeedURL)
	}

	now := s.podcastService.clock.Now()
	feed.LastCheckedAt = &now
	if err := s.podcastService.store.UpdateFeed(ctx, feed); err != nil {
		return nil, fmt.Errorf("update feed: %w", err)
	}

	return feed, nil
}

func (s *podcastSubscriptionService) ensurePodcastSet(ctx context.Context, userID int64) (*model.Set, error) {
	set, err := s.findPodcastSet(ctx)
	if err != nil {
		return nil, err
	}
	setPath := filepath.Join(s.podcastService.mediaRoot, podcastSetName)
	if set == nil {
		set, err = s.createPodcastSet(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(setPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir podcast set path: %w", err)
	}
	if err := s.podcastService.store.GrantPermission(ctx, &model.SetPermission{
		SetID:     set.ID,
		UserID:    userID,
		Role:      model.RoleOwner,
		CreatedAt: s.podcastService.clock.Now(),
	}); err != nil {
		return nil, fmt.Errorf("grant permission: %w", err)
	}
	return set, nil
}

func (s *podcastSubscriptionService) findPodcastSet(ctx context.Context) (*model.Set, error) {
	sets, err := s.podcastService.store.ListSets(ctx)
	if err != nil {
		return nil, err
	}
	for _, set := range sets {
		if set.IsPodcast && (set.RootPath == podcastSetName || strings.EqualFold(set.Name, podcastSetName)) {
			if set.Name != podcastSetName || set.RootPath != podcastSetName {
				set.Name = podcastSetName
				set.RootPath = podcastSetName
				if err := s.podcastService.store.UpdateSet(ctx, &set); err != nil {
					return nil, fmt.Errorf("update podcast set: %w", err)
				}
			}
			return &set, nil
		}
	}
	return nil, nil
}

func (s *podcastSubscriptionService) findExistingFeed(ctx context.Context, setID int64, title, feedURL string) (*model.PodcastFeed, error) {
	feeds, err := s.podcastService.store.ListFeedsBySetID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("list feeds by set: %w", err)
	}
	for _, feed := range feeds {
		if feed.FeedURL == feedURL {
			return &feed, nil
		}
	}
	for _, feed := range feeds {
		if strings.EqualFold(feed.Title, title) {
			return &feed, nil
		}
	}
	return nil, nil
}

func (s *podcastSubscriptionService) verifyAdmin(ctx context.Context, userID int64) error {
	user, err := s.podcastService.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil || !user.IsAdmin {
		return ErrForbidden
	}
	return nil
}

func (s *podcastSubscriptionService) createPodcastSet(ctx context.Context, userID int64) (*model.Set, error) {
	setPath := filepath.Join(s.podcastService.mediaRoot, podcastSetName)
	// Create the directory first so we can clean it up easily on DB errors.
	if err := os.MkdirAll(setPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir set path: %w", err)
	}

	set := &model.Set{
		Name:      podcastSetName,
		RootPath:  podcastSetName,
		IsPodcast: true,
		CreatedAt: s.podcastService.clock.Now(),
	}
	setID, err := s.podcastService.store.CreateSet(ctx, set)
	if err != nil {
		os.RemoveAll(setPath)
		return nil, fmt.Errorf("create set: %w", err)
	}
	set.ID = setID

	perm := &model.SetPermission{SetID: setID, UserID: userID, Role: model.RoleOwner}
	if err := s.podcastService.store.GrantPermission(ctx, perm); err != nil {
		_ = s.podcastService.store.DeleteSet(ctx, setID)
		os.RemoveAll(setPath)
		return nil, fmt.Errorf("grant permission: %w", err)
	}

	return set, nil
}

func (s *podcastSubscriptionService) rollbackSet(ctx context.Context, setID int64, setPath string) {
	_ = s.podcastService.store.DeleteSet(ctx, setID)
	os.RemoveAll(setPath)
}

func (s *podcastSubscriptionService) createPodcastFeed(ctx context.Context, parsed *podcast.ParsedFeed, setID int64, feedURL string) (*model.PodcastFeed, error) {
	feed := &model.PodcastFeed{
		SetID:                setID,
		FeedURL:              feedURL,
		Title:                parsed.Title,
		Description:          parsed.Description,
		ImageURL:             parsed.ImageURL,
		CheckIntervalMinutes: s.podcastService.checkInterval,
		CreatedAt:            s.podcastService.clock.Now(),
	}
	feedID, err := s.podcastService.store.CreateFeed(ctx, feed)
	if err != nil {
		return nil, fmt.Errorf("create feed: %w", err)
	}
	feed.ID = feedID
	return feed, nil
}

// ListFeeds returns the podcast feeds visible to the user.
func (s *podcastSubscriptionService) ListFeeds(ctx context.Context, userID int64) ([]model.PodcastFeed, error) {
	feeds, err := s.podcastService.store.ListFeeds(ctx)
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}

	visible := make([]model.PodcastFeed, 0, len(feeds))
	for _, feed := range feeds {
		if err := s.podcastService.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
			if errors.Is(err, ErrForbidden) {
				continue
			}
			return nil, err
		}
		visible = append(visible, feed)
	}
	return visible, nil
}

func (s *podcastSubscriptionService) insertPodcastEpisodes(ctx context.Context, parsed *podcast.ParsedFeed, feedID int64) error {
	for _, ep := range parsed.Episodes {
		episode := &model.PodcastEpisode{
			FeedID:          feedID,
			GUID:            ep.GUID,
			Title:           ep.Title,
			Description:     ep.Description,
			PublishedAt:     ep.PublishedAt,
			EpisodeURL:      ep.EpisodeURL,
			DurationSeconds: ep.DurationSeconds,
			FileSize:        ep.FileSize,
			CreatedAt:       s.podcastService.clock.Now(),
		}
		if _, err := s.podcastService.store.CreateEpisode(ctx, episode); err != nil {
			return fmt.Errorf("create episode %q: %w", episode.GUID, err)
		}
	}
	return nil
}

// EditFeed updates the feed URL and check interval.
func (s *podcastSubscriptionService) EditFeed(ctx context.Context, feedID int64, feedURL string, checkInterval int, userID int64) error {
	feed, err := s.podcastService.store.GetFeedByID(ctx, feedID)
	if err != nil {
		return fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return ErrNotFound
	}

	// Verify owner permission on the set.
	if err := s.podcastService.helper.verifySetModifyAccess(ctx, feed.SetID, userID); err != nil {
		return err
	}

	feed.FeedURL = feedURL
	if checkInterval >= 1 {
		feed.CheckIntervalMinutes = checkInterval
	}
	return s.podcastService.store.UpdateFeed(ctx, feed)
}

// UnsubscribeFeed removes a podcast feed and optionally cleans up on-disk files.
func (s *podcastSubscriptionService) UnsubscribeFeed(ctx context.Context, feedID int64, userID int64) error {
	feed, err := s.podcastService.store.GetFeedByID(ctx, feedID)
	if err != nil {
		return fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return ErrNotFound
	}

	// Verify owner permission on the set.
	if err := s.podcastService.helper.verifySetModifyAccess(ctx, feed.SetID, userID); err != nil {
		return err
	}

	set, err := s.podcastService.store.GetSetByID(ctx, feed.SetID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}

	if err := s.podcastService.store.DeleteFeed(ctx, feedID); err != nil {
		return fmt.Errorf("delete feed: %w", err)
	}

	// Optionally delete the folder contents on disk.
	if set != nil {
		folder := podcastFolderName("", feed.Title, feed.ID)
		_ = os.RemoveAll(filepath.Join(s.podcastService.mediaRoot, set.RootPath, folder))
	}

	return nil
}
