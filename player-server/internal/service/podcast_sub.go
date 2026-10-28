package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/podcast"
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

	feed, err := s.findExistingFeed(ctx, set.ID, feedURL)
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
	if err := ingestParsedEpisodes(ctx, s.podcastService, feed, parsed); err != nil {
		s.podcastService.logger.Warn("insert podcast episodes failed", "error", err, "feed", feed.FeedURL)
	}

	now := s.podcastService.clock.Now()
	feed.LastCheckedAt = &now
	if err := s.podcastService.store.UpdateFeed(ctx, feed); err != nil {
		return nil, fmt.Errorf("update feed: %w", err)
	}
	// The subscriber's own register (contract §10): the feed now has an
	// active subscription, so the checker keeps refreshing it, and the
	// subscription reaches the admin's other clients through sync.
	if err := s.podcastService.store.SetPodcastSubscription(ctx, userID, feed, true, now); err != nil {
		return nil, fmt.Errorf("record subscription: %w", err)
	}
	feed.Subscribed = true
	return feed, nil
}

// podcastLibrary resolves the library registered for MEDIA_ROOT. Podcast files
// are served and downloaded under that root, regardless of other libraries.
func (s *podcastSubscriptionService) podcastLibrary(ctx context.Context) (*model.MusicLibrary, error) {
	libs, err := s.store.ListMusicLibraries(ctx)
	if err != nil {
		return nil, fmt.Errorf("find podcast library: %w", err)
	}
	root, err := filepath.Abs(s.mediaRoot)
	if err != nil {
		return nil, err
	}
	for _, lib := range libs {
		if lib.RootPath == root {
			return &lib, nil
		}
	}
	return nil, fmt.Errorf("podcast library for media root is not registered")
}

func (s *podcastSubscriptionService) ensurePodcastSet(ctx context.Context, userID int64) (*model.Set, error) {
	lib, err := s.podcastLibrary(ctx)
	if err != nil {
		return nil, err
	}
	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return nil, err
	}
	for _, set := range sets {
		if set.LibraryID != lib.ID || !set.IsPodcast || set.RootPath != podcastSetName {
			continue
		}
		cleanup, err := s.preparePodcastDirectory()
		if err != nil {
			return nil, err
		}
		if err := s.store.GrantPermission(ctx, &model.SetPermission{SetID: set.ID, UserID: userID, Role: model.RoleOwner, CreatedAt: s.clock.Now()}); err != nil {
			cleanup()
			return nil, fmt.Errorf("grant permission: %w", err)
		}
		return &set, nil
	}
	return s.createPodcastSet(ctx, userID, lib.ID)
}

// findExistingFeed returns the catalog feed whose key (or one of whose
// aliases) is the key of feedURL, so http/https and other spellings of one
// feed reuse its row. A URL without a key (credentials, odd scheme) reuses
// a row with exactly that URL, as before keys existed. Titles are not
// identity and are no longer used to match.
func (s *podcastSubscriptionService) findExistingFeed(ctx context.Context, setID int64, feedURL string) (*model.PodcastFeed, error) {
	key, err := podcast.FeedKey(feedURL)
	if err != nil {
		return s.findFeedByURL(ctx, setID, feedURL)
	}
	feed, err := s.podcastService.store.GetFeedByKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("find feed by key: %w", err)
	}
	return feed, nil
}

// findFeedByURL returns the catalog feed of setID fetched from exactly
// feedURL, or nil.
func (s *podcastSubscriptionService) findFeedByURL(ctx context.Context, setID int64, feedURL string) (*model.PodcastFeed, error) {
	feeds, err := s.podcastService.store.ListFeedsBySetID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("list feeds by set: %w", err)
	}
	for i := range feeds {
		if feeds[i].FeedURL == feedURL && feeds[i].MergedInto == nil {
			return &feeds[i], nil
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

func (s *podcastSubscriptionService) preparePodcastDirectory() (func(), error) {
	setPath := filepath.Join(s.mediaRoot, podcastSetName)
	if !confinedPath(s.mediaRoot, setPath) {
		return nil, ErrForbidden
	}
	// Only this call's empty directory may be removed on failure. Existing
	// folders (including unindexed files) belong to the library owner.
	created := false
	if err := os.Mkdir(setPath, 0o755); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return nil, mutationError(err)
	} else {
		info, err := os.Stat(setPath)
		if err != nil {
			return nil, mutationError(err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("podcast set path is not a directory")
		}
	}
	cleanup := func() {
		if created {
			_ = os.Remove(setPath)
		}
	}
	// Reusing an existing folder may require only database metadata writes.
	// Keep read-only subscriptions working when the feed folder also exists.
	if created {
		if err := writableMediaDir(s.mediaRoot, setPath); err != nil {
			cleanup()
			return nil, err
		}
	}
	return cleanup, nil
}

func (s *podcastSubscriptionService) createPodcastSet(ctx context.Context, userID, libraryID int64) (*model.Set, error) {
	cleanup, err := s.preparePodcastDirectory()
	if err != nil {
		return nil, err
	}
	set := &model.Set{LibraryID: libraryID, Name: podcastSetName, RootPath: podcastSetName, IsPodcast: true, CreatedAt: s.clock.Now()}
	id, err := s.store.CreateSet(ctx, set)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create set: %w", err)
	}
	set.ID = id
	if err := s.store.GrantPermission(ctx, &model.SetPermission{SetID: id, UserID: userID, Role: model.RoleOwner, CreatedAt: s.clock.Now()}); err != nil {
		_ = s.store.DeleteSet(ctx, id)
		cleanup()
		return nil, fmt.Errorf("grant permission: %w", err)
	}
	return set, nil
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

// ListFeeds returns the catalog feeds whose set the user may read, as
// before account subscriptions existed, including feeds the user has
// unsubscribed from: it is the catalog view, not the subscription view.
// Each feed carries the user's subscription register in Subscribed.
func (s *podcastSubscriptionService) ListFeeds(ctx context.Context, userID int64) ([]model.PodcastFeed, error) {
	feeds, err := s.podcastService.store.ListFeeds(ctx)
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}
	subscribed, err := s.podcastService.store.PodcastSubscriptionStates(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}

	visible := make([]model.PodcastFeed, 0, len(feeds))
	for _, feed := range feeds {
		if err := s.podcastService.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
			if errors.Is(err, ErrForbidden) {
				continue
			}
			return nil, err
		}
		feed.Subscribed = subscribed[feed.FeedKey]
		visible = append(visible, feed)
	}
	return visible, nil
}

// EditFeed updates the feed URL and check interval. A corrected URL of a
// feed with a legacy key (one whose stored URL had no feed key) retires
// that key (contract §3.1): the feed takes the URL's key, or is merged into
// the catalog feed that already has it, and every user's state follows.
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
	if err := s.podcastService.store.UpdateFeed(ctx, feed); err != nil {
		return err
	}
	if !strings.HasPrefix(feed.FeedKey, legacyFeedKeyPrefix) {
		return nil
	}
	if _, err := s.podcastService.store.MoveFeed(ctx, feed.ID, feedURL); err != nil {
		return fmt.Errorf("retire legacy feed key: %w", err)
	}
	return nil
}
