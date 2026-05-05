package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// ------------------------------------------------------------------
// Interfaces
// ------------------------------------------------------------------

// PodcastSubService manages podcast feed subscriptions.
type PodcastSubService interface {
	SubscribeFeed(ctx context.Context, feedURL, setName string, userID int64) (*model.PodcastFeed, error)
	EditFeed(ctx context.Context, feedID int64, feedURL string, checkInterval int, userID int64) error
	UnsubscribeFeed(ctx context.Context, feedID int64, userID int64) error
}

// PodcastEpisodeService manages episode browsing and downloading.
type PodcastEpisodeService interface {
	SubscribeFeed(ctx context.Context, feedURL, setName string, userID int64) (*model.PodcastFeed, error)
	EditFeed(ctx context.Context, feedID int64, feedURL string, checkInterval int, userID int64) error
	UnsubscribeFeed(ctx context.Context, feedID int64, userID int64) error
	ListEpisodes(ctx context.Context, setID, userID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error)
	DownloadEpisode(ctx context.Context, episodeID, userID int64) (*model.Media, error)
	ToggleEpisodeComplete(ctx context.Context, episodeID, userID int64) error
	CheckFeeds(ctx context.Context) error
}

// PodcastChecker triggers background feed refresh.
type PodcastChecker interface {
	CheckFeeds(ctx context.Context) error
}

// ------------------------------------------------------------------
// Store interface
// ------------------------------------------------------------------

// PodcastServiceStore is the data layer dependency for podcast operations.
type PodcastServiceStore interface {
	repository.PodcastRepo
	repository.SetRepo
	repository.SetPermissionRepo
	repository.MediaRepo
	repository.UserRepo
}

// ------------------------------------------------------------------
// Service implementation
// ------------------------------------------------------------------

type podcastService struct {
	store         PodcastServiceStore
	clock         clock.Clock
	mediaRoot     string
	helper        *accessHelper
	prober        probe.Prober
	thumbGen      thumb.Generator
	httpClient    *http.Client
	checkInterval int // minutes
}

// NewPodcastService creates a PodcastService with the given dependencies.
func NewPodcastService(store PodcastServiceStore, clk clock.Clock, mediaRoot string, helper *accessHelper, prober probe.Prober, thumbGen thumb.Generator) *podcastService {
	return &podcastService{
		store:         store,
		clock:         clk,
		mediaRoot:     mediaRoot,
		helper:        helper,
		prober:        prober,
		thumbGen:      thumbGen,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		checkInterval: 60,
	}
}

// ------------------------------------------------------------------
// Subscription
// ------------------------------------------------------------------

func (s *podcastService) SubscribeFeed(ctx context.Context, feedURL, setName string, userID int64) (*model.PodcastFeed, error) {
	// Only admins can create podcast sets.
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil || !user.IsAdmin {
		return nil, ErrForbidden
	}

	// Parse feed to validate URL and extract metadata.
	parsed, err := podcast.ParseFeed(feedURL)
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	// Sanitize set name for filesystem.
	safeName := sanitizeSetName(setName)
	if safeName == "" {
		safeName = sanitizeSetName(parsed.Title)
	}
	if safeName == "" {
		safeName = "podcast"
	}
	setPath := filepath.Join(s.mediaRoot, safeName)

	// Create folder on disk.
	if err := os.MkdirAll(setPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir set path: %w", err)
	}

	// Create set row.
	set := &model.Set{
		Name:      parsed.Title,
		RootPath:  safeName,
		IsPodcast: true,
		CreatedAt: s.clock.Now(),
	}
	setID, err := s.store.CreateSet(ctx, set)
	if err != nil {
		os.Remove(setPath)
		return nil, fmt.Errorf("create set: %w", err)
	}
	set.ID = setID

	// Grant owner permission to the creating user.
	perm := &model.SetPermission{SetID: setID, UserID: userID, Role: model.RoleOwner}
	if err := s.store.GrantPermission(ctx, perm); err != nil {
		// Rollback set on error.
		_ = s.store.DeleteSet(ctx, setID)
		os.Remove(setPath)
		return nil, fmt.Errorf("grant permission: %w", err)
	}

	// Insert podcast feed row.
	feed := &model.PodcastFeed{
		SetID:                setID,
		FeedURL:              feedURL,
		Title:                parsed.Title,
		Description:          parsed.Description,
		ImageURL:             parsed.ImageURL,
		CheckIntervalMinutes: s.checkInterval,
		CreatedAt:            s.clock.Now(),
	}
	feedID, err := s.store.CreateFeed(ctx, feed)
	if err != nil {
		_ = s.store.DeleteSet(ctx, setID)
		os.Remove(setPath)
		return nil, fmt.Errorf("create feed: %w", err)
	}
	feed.ID = feedID

	// Download cover image.
	if parsed.ImageURL != "" {
		_ = podcast.DownloadCoverImage(parsed.ImageURL, setPath)
	}

	// Insert episodes.
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
			CreatedAt:       s.clock.Now(),
		}
		_, _ = s.store.CreateEpisode(ctx, episode)
	}

	// Mark feed as checked.
	now := s.clock.Now()
	feed.LastCheckedAt = &now
	_ = s.store.UpdateFeed(ctx, feed)

	return feed, nil
}

func (s *podcastService) EditFeed(ctx context.Context, feedID int64, feedURL string, checkInterval int, userID int64) error {
	feed, err := s.store.GetFeedByID(ctx, feedID)
	if err != nil {
		return fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return ErrNotFound
	}

	// Verify owner permission on the set.
	if err := s.helper.verifySetModifyAccess(ctx, feed.SetID, userID); err != nil {
		return err
	}

	feed.FeedURL = feedURL
	if checkInterval >= 1 {
		feed.CheckIntervalMinutes = checkInterval
	}
	return s.store.UpdateFeed(ctx, feed)
}

func (s *podcastService) UnsubscribeFeed(ctx context.Context, feedID int64, userID int64) error {
	feed, err := s.store.GetFeedByID(ctx, feedID)
	if err != nil {
		return fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return ErrNotFound
	}

	// Verify owner permission on the set.
	if err := s.helper.verifySetModifyAccess(ctx, feed.SetID, userID); err != nil {
		return err
	}

	// Delete the set row; ON DELETE CASCADE removes feed + episodes.
	set, err := s.store.GetSetByID(ctx, feed.SetID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}

	if err := s.store.DeleteSet(ctx, feed.SetID); err != nil {
		return fmt.Errorf("delete set: %w", err)
	}

	// Optionally delete the folder contents on disk.
	if set != nil {
		setPath := filepath.Join(s.mediaRoot, set.RootPath)
		_ = os.RemoveAll(setPath)
	}

	return nil
}

// ------------------------------------------------------------------
// Episodes
// ------------------------------------------------------------------

func (s *podcastService) ListEpisodes(ctx context.Context, setID, userID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
	if err := s.helper.checkSetPermission(ctx, setID, userID, ""); err != nil {
		return nil, err
	}

	feed, err := s.store.GetFeedBySetID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get feed by set: %w", err)
	}
	if feed == nil {
		return nil, ErrNotFound
	}

	return s.store.ListEpisodesWithStatus(ctx, userID, feed.ID, limit, offset)
}

func (s *podcastService) DownloadEpisode(ctx context.Context, episodeID, userID int64) (*model.Media, error) {
	// Fetch episode.
	episode, err := s.store.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return nil, fmt.Errorf("get episode: %w", err)
	}
	if episode == nil {
		return nil, ErrNotFound
	}

	// Fetch feed for set path.
	feed, err := s.store.GetFeedByID(ctx, episode.FeedID)
	if err != nil {
		return nil, fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return nil, ErrNotFound
	}

	// Verify permission.
	if err := s.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
		return nil, err
	}

	// Get set for root path.
	set, err := s.store.GetSetByID(ctx, feed.SetID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	// Determine target filename: YYYY-MM-DD - sanitized-title.ext
	var dateStr string
	if episode.PublishedAt != nil {
		dateStr = episode.PublishedAt.Format("2006-01-02")
	} else {
		dateStr = s.clock.Now().Format("2006-01-02")
	}

	ext := filepath.Ext(episode.EpisodeURL)
	if ext == "" {
		ext = ".mp3"
	}
	cleanTitle := sanitizeFilename(episode.Title)
	if cleanTitle == "" {
		cleanTitle = fmt.Sprintf("episode-%d", episode.ID)
	}
	filename := fmt.Sprintf("%s - %s%s", dateStr, cleanTitle, ext)

	setPath := filepath.Join(s.mediaRoot, set.RootPath)
	path := uniqueFilename(setPath, filename)

	// Download enclosure.
	resp, err := s.httpClient.Get(episode.EpisodeURL)
	if err != nil {
		return nil, fmt.Errorf("download episode: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download episode: status %d", resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("write file: %w", err)
	}

	// Create media row.
	media := &model.Media{
		SetID:         set.ID,
		RelPath:       filepath.Base(path),
		FileName:      filepath.Base(path),
		AbsPath:       path,
		Type:          mediatype.TypeForExt(filepath.Base(path)),
		FileSizeBytes: n,
		CreatedAt:     s.clock.Now(),
	}
	mediaID, err := s.store.CreateMedia(ctx, media)
	if err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = mediaID

	// Probe, thumbnail, and update metadata using shared helper.
	if err := ImportMediaFile(ctx, s.store, media, s.prober, s.thumbGen); err != nil {
		os.Remove(path)
		_ = s.store.HardDeleteMedia(ctx, media.ID)
		return nil, err
	}

	// Link episode to media row.
	if err := s.store.UpdateEpisodeMedia(ctx, episode.ID, media.ID, filepath.Base(path)); err != nil {
		return nil, fmt.Errorf("update episode media: %w", err)
	}

	return media, nil
}

func (s *podcastService) ToggleEpisodeComplete(ctx context.Context, episodeID, userID int64) error {
	// Verify user has access to the set containing this episode.
	episode, err := s.store.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return fmt.Errorf("get episode: %w", err)
	}
	if episode == nil {
		return ErrNotFound
	}

	feed, err := s.store.GetFeedByID(ctx, episode.FeedID)
	if err != nil {
		return fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return ErrNotFound
	}

	if err := s.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
		return err
	}

	// Get current status.
	status, err := s.store.GetEpisodeProgress(ctx, userID, episodeID)
	if err != nil {
		return fmt.Errorf("get episode progress: %w", err)
	}

	isCompleted := true
	if status != nil && status.IsCompleted {
		isCompleted = false
	}

	now := s.clock.Now()
	newStatus := &model.PodcastStatus{
		UserID:      userID,
		EpisodeID:   episodeID,
		IsCompleted: isCompleted,
		UpdatedAt:   now,
	}
	return s.store.UpsertEpisodeProgress(ctx, newStatus)
}

// ------------------------------------------------------------------
// Background checker
// ------------------------------------------------------------------

func (s *podcastService) CheckFeeds(ctx context.Context) error {
	before := s.clock.Now().Add(-time.Duration(s.checkInterval) * time.Minute)
	feeds, err := s.store.ListFeedsNeedingCheck(ctx, before)
	if err != nil {
		return fmt.Errorf("list feeds needing check: %w", err)
	}

	for _, feed := range feeds {
		if err := s.checkFeed(ctx, feed); err != nil {
			// Log and continue with other feeds.
			continue
		}
	}
	return nil
}

func (s *podcastService) checkFeed(ctx context.Context, feed model.PodcastFeed) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.FeedURL, nil)
	if err != nil {
		return err
	}

	// Conditional GET headers.
	if feed.LastETag != "" {
		req.Header.Set("If-None-Match", feed.LastETag)
	}
	if feed.LastCheckedAt != nil {
		req.Header.Set("If-Modified-Since", feed.LastCheckedAt.Format(http.TimeFormat))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		now := s.clock.Now()
		feed.LastCheckedAt = &now
		_ = s.store.UpdateFeed(ctx, &feed)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("feed check status %d", resp.StatusCode)
	}

	parsed, err := podcast.ParseFeed(feed.FeedURL)
	if err != nil {
		return err
	}

	// Update feed metadata.
	feed.Title = parsed.Title
	feed.Description = parsed.Description
	feed.ImageURL = parsed.ImageURL
	feed.LastETag = resp.Header.Get("ETag")
	now := s.clock.Now()
	feed.LastCheckedAt = &now

	if err := s.store.UpdateFeed(ctx, &feed); err != nil {
		return err
	}

	// Upsert episodes.
	for _, ep := range parsed.Episodes {
		existing, err := s.store.GetEpisodeByGUID(ctx, feed.ID, ep.GUID)
		if err != nil {
			continue
		}
		if existing == nil {
			episode := &model.PodcastEpisode{
				FeedID:      feed.ID,
				GUID:        ep.GUID,
				Title:       ep.Title,
				Description: ep.Description,
				PublishedAt: ep.PublishedAt,
				EpisodeURL:  ep.EpisodeURL,
				DurationSeconds: ep.DurationSeconds,
				FileSize:    ep.FileSize,
				CreatedAt:   s.clock.Now(),
			}
			_, _ = s.store.CreateEpisode(ctx, episode)
		}
	}

	// Re-download cover if changed.
	if parsed.ImageURL != "" {
		set, err := s.store.GetSetByID(ctx, feed.SetID)
		if err == nil && set != nil {
			setPath := filepath.Join(s.mediaRoot, set.RootPath)
			_ = podcast.DownloadCoverImage(parsed.ImageURL, setPath)
		}
	}

	return nil
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

func sanitizeSetName(name string) string {
	// Remove path separators and other unsafe characters.
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.TrimSpace(name)
	return name
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.TrimSpace(name)
	return name
}

func uniqueFilename(dir, filename string) string {
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == "" {
		return ""
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}

	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
