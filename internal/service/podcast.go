package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	ListFeeds(ctx context.Context, userID int64) ([]model.PodcastFeed, error)
	EditFeed(ctx context.Context, feedID int64, feedURL string, checkInterval int, userID int64) error
	UnsubscribeFeed(ctx context.Context, feedID int64, userID int64) error
}

// PodcastEpisodeService manages episode browsing and downloading.
type PodcastEpisodeService interface {
	SubscribeFeed(ctx context.Context, feedURL, setName string, userID int64) (*model.PodcastFeed, error)
	ListFeeds(ctx context.Context, userID int64) ([]model.PodcastFeed, error)
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
	store           PodcastServiceStore
	clock           clock.Clock
	mediaRoot       string
	helper          *accessHelper
	prober          probe.Prober
	thumbGen        thumb.Generator
	httpClient      *http.Client
	checkInterval   int // minutes
	logger          *slog.Logger
	parseFeed       func(string) (*podcast.ParsedFeed, error)
	parseFeedReader func(io.Reader) (*podcast.ParsedFeed, error)
	downloadCover   func(*http.Client, string, string) error
}

// DefaultHTTPClientTimeout is the fallback timeout used when no http.Client is injected.
const DefaultHTTPClientTimeout = 30 * time.Second

// NewPodcastService creates a PodcastService with the given dependencies.
// checkInterval should be the number of minutes between background feed checks.
func NewPodcastService(store PodcastServiceStore, clk clock.Clock, mediaRoot string, helper *accessHelper, prober probe.Prober, thumbGen thumb.Generator, httpClient *http.Client, checkInterval int) *podcastService {
	return NewPodcastServiceWithLogger(store, clk, mediaRoot, helper, prober, thumbGen, httpClient, checkInterval, slog.Default())
}

// NewPodcastServiceWithLogger creates a PodcastService with an injected logger.
func NewPodcastServiceWithLogger(store PodcastServiceStore, clk clock.Clock, mediaRoot string, helper *accessHelper, prober probe.Prober, thumbGen thumb.Generator, httpClient *http.Client, checkInterval int, logger *slog.Logger) *podcastService {
	if checkInterval <= 0 {
		checkInterval = 60
	}
	if logger == nil {
		logger = slog.Default()
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultHTTPClientTimeout}
	}
	s := &podcastService{
		store:         store,
		clock:         clk,
		mediaRoot:     mediaRoot,
		helper:        helper,
		prober:        prober,
		thumbGen:      thumbGen,
		httpClient:    httpClient,
		checkInterval: checkInterval,
		logger:        logger,
	}
	// Wire package-level helpers so tests can inject fakes.
	s.parseFeed = podcast.ParseFeed
	s.parseFeedReader = podcast.ParseFeedReader
	s.downloadCover = podcast.DownloadCoverImage
	return s
}

// ------------------------------------------------------------------
// Subscription
// ------------------------------------------------------------------

const podcastSetName = "podcast"

func (s *podcastService) SubscribeFeed(ctx context.Context, feedURL, setName string, userID int64) (*model.PodcastFeed, error) {
	if err := s.verifyAdmin(ctx, userID); err != nil {
		return nil, err
	}

	parsed, err := s.parseFeed(feedURL)
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
		feed.CheckIntervalMinutes = s.checkInterval
	}

	folderPath := filepath.Join(s.mediaRoot, set.RootPath, podcastFolderName("", parsed.Title, feed.ID))
	if err := os.MkdirAll(folderPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir podcast folder: %w", err)
	}
	s.downloadCover(s.httpClient, parsed.ImageURL, folderPath)
	s.insertPodcastEpisodes(ctx, parsed, feed.ID)

	now := s.clock.Now()
	feed.LastCheckedAt = &now
	if err := s.store.UpdateFeed(ctx, feed); err != nil {
		return nil, fmt.Errorf("update feed: %w", err)
	}

	return feed, nil
}

func (s *podcastService) ensurePodcastSet(ctx context.Context, userID int64) (*model.Set, error) {
	set, err := s.findPodcastSet(ctx)
	if err != nil {
		return nil, err
	}
	setPath := filepath.Join(s.mediaRoot, podcastSetName)
	if set == nil {
		set, err = s.createPodcastSet(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(setPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir podcast set path: %w", err)
	}
	if err := s.store.GrantPermission(ctx, &model.SetPermission{
		SetID:     set.ID,
		UserID:    userID,
		Role:      model.RoleOwner,
		CreatedAt: s.clock.Now(),
	}); err != nil {
		return nil, fmt.Errorf("grant permission: %w", err)
	}
	return set, nil
}

func (s *podcastService) findPodcastSet(ctx context.Context) (*model.Set, error) {
	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return nil, err
	}
	for _, set := range sets {
		if set.IsPodcast && (set.RootPath == podcastSetName || strings.EqualFold(set.Name, podcastSetName)) {
			if set.Name != podcastSetName || set.RootPath != podcastSetName {
				set.Name = podcastSetName
				set.RootPath = podcastSetName
				if err := s.store.UpdateSet(ctx, &set); err != nil {
					return nil, fmt.Errorf("update podcast set: %w", err)
				}
			}
			return &set, nil
		}
	}
	return nil, nil
}

func (s *podcastService) findExistingFeed(ctx context.Context, setID int64, title, feedURL string) (*model.PodcastFeed, error) {
	feeds, err := s.store.ListFeedsBySetID(ctx, setID)
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

func podcastFolderName(requestedName, title string, feedID int64) string {
	name := sanitizeSetName(requestedName)
	if name == "" {
		name = sanitizeSetName(title)
	}
	if name == "" {
		name = fmt.Sprintf("feed-%d", feedID)
	}
	return name
}

func (s *podcastService) verifyAdmin(ctx context.Context, userID int64) error {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil || !user.IsAdmin {
		return ErrForbidden
	}
	return nil
}

func (s *podcastService) createPodcastSet(ctx context.Context, userID int64) (*model.Set, error) {
	setPath := filepath.Join(s.mediaRoot, podcastSetName)
	// Create the directory first so we can clean it up easily on DB errors.
	if err := os.MkdirAll(setPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir set path: %w", err)
	}

	set := &model.Set{
		Name:      podcastSetName,
		RootPath:  podcastSetName,
		IsPodcast: true,
		CreatedAt: s.clock.Now(),
	}
	setID, err := s.store.CreateSet(ctx, set)
	if err != nil {
		os.RemoveAll(setPath)
		return nil, fmt.Errorf("create set: %w", err)
	}
	set.ID = setID

	perm := &model.SetPermission{SetID: setID, UserID: userID, Role: model.RoleOwner}
	if err := s.store.GrantPermission(ctx, perm); err != nil {
		_ = s.store.DeleteSet(ctx, setID)
		os.RemoveAll(setPath)
		return nil, fmt.Errorf("grant permission: %w", err)
	}

	return set, nil
}

func (s *podcastService) rollbackSet(ctx context.Context, setID int64, setPath string) {
	_ = s.store.DeleteSet(ctx, setID)
	os.RemoveAll(setPath)
}

func (s *podcastService) createPodcastFeed(ctx context.Context, parsed *podcast.ParsedFeed, setID int64, feedURL string) (*model.PodcastFeed, error) {
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
		return nil, fmt.Errorf("create feed: %w", err)
	}
	feed.ID = feedID
	return feed, nil
}

func (s *podcastService) ListFeeds(ctx context.Context, userID int64) ([]model.PodcastFeed, error) {
	feeds, err := s.store.ListFeeds(ctx)
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}

	visible := make([]model.PodcastFeed, 0, len(feeds))
	for _, feed := range feeds {
		if err := s.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
			if errors.Is(err, ErrForbidden) {
				continue
			}
			return nil, err
		}
		visible = append(visible, feed)
	}
	return visible, nil
}

func (s *podcastService) insertPodcastEpisodes(ctx context.Context, parsed *podcast.ParsedFeed, feedID int64) {
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

	set, err := s.store.GetSetByID(ctx, feed.SetID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}

	if err := s.store.DeleteFeed(ctx, feedID); err != nil {
		return fmt.Errorf("delete feed: %w", err)
	}

	// Optionally delete the folder contents on disk.
	if set != nil {
		folder := podcastFolderName("", feed.Title, feed.ID)
		_ = os.RemoveAll(filepath.Join(s.mediaRoot, set.RootPath, folder))
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

	feeds, err := s.store.ListFeedsBySetID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("list feeds by set: %w", err)
	}
	if len(feeds) == 0 {
		return nil, ErrNotFound
	}

	all := make([]model.PodcastEpisodeWithStatus, 0)
	for _, feed := range feeds {
		episodes, err := s.store.ListEpisodesWithStatus(ctx, userID, feed.ID, limit, 0)
		if err != nil {
			return nil, err
		}
		all = append(all, episodes...)
	}
	if offset >= len(all) {
		return []model.PodcastEpisodeWithStatus{}, nil
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return all[offset:end], nil
}

func (s *podcastService) DownloadEpisode(ctx context.Context, episodeID, userID int64) (*model.Media, error) {
	episode, set, path, err := s.resolveEpisodeAndSet(ctx, episodeID, userID)
	if err != nil {
		return nil, err
	}

	n, err := s.downloadEnclosure(ctx, episode, path)
	if err != nil {
		return nil, err
	}

	media, cleanup, err := s.persistDownloadedEpisode(ctx, episode, set, path, n)
	if err != nil {
		return nil, err
	}

	// Post-persistence failure: link episode to media row.
	if err := s.store.UpdateEpisodeMedia(ctx, episode.ID, media.ID, filepath.Base(path)); err != nil {
		cleanup()
		return nil, fmt.Errorf("update episode media: %w", err)
	}

	return media, nil
}

// resolveEpisodeAndSet fetches the episode, feed, and set, verifies user
// permission, and returns the unique target file path on disk.
func (s *podcastService) resolveEpisodeAndSet(ctx context.Context, episodeID, userID int64) (*model.PodcastEpisode, *model.Set, string, error) {
	episode, err := s.store.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get episode: %w", err)
	}
	if episode == nil {
		return nil, nil, "", ErrNotFound
	}

	feed, err := s.store.GetFeedByID(ctx, episode.FeedID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return nil, nil, "", ErrNotFound
	}

	if err := s.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
		return nil, nil, "", err
	}

	set, err := s.store.GetSetByID(ctx, feed.SetID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, nil, "", ErrNotFound
	}

	feedFolder := podcastFolderName("", feed.Title, feed.ID)
	setPath := filepath.Join(s.mediaRoot, set.RootPath, feedFolder)
	path := buildEpisodePath(setPath, episode, s.clock.Now())
	return episode, set, path, nil
}

// buildEpisodePath builds a unique local path for the episode enclosure.
func buildEpisodePath(setPath string, episode *model.PodcastEpisode, now time.Time) string {
	dateStr := now.Format("2006-01-02")
	if episode.PublishedAt != nil {
		dateStr = episode.PublishedAt.Format("2006-01-02")
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
	return uniqueFilename(setPath, filename)
}

// downloadEnclosure performs the HTTP GET, writes the body to path, and
// returns the number of bytes written.
func (s *podcastService) downloadEnclosure(ctx context.Context, episode *model.PodcastEpisode, path string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, episode.EpisodeURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build download request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download episode: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download episode: status %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir episode folder: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		os.Remove(path)
		return 0, fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return 0, fmt.Errorf("close file: %w", err)
	}

	return n, nil
}

// persistDownloadedEpisode records the downloaded file in the database,
// probes it, and returns a cleanup function to undo work on failure.
func (s *podcastService) persistDownloadedEpisode(ctx context.Context, episode *model.PodcastEpisode, set *model.Set, path string, n int64) (*model.Media, func(), error) {
	relPath, err := filepath.Rel(filepath.Join(s.mediaRoot, set.RootPath), path)
	if err != nil {
		os.Remove(path)
		return nil, nil, fmt.Errorf("relative episode path: %w", err)
	}
	relPath = filepath.ToSlash(relPath)
	media := &model.Media{
		SetID:         set.ID,
		RelPath:       relPath,
		FileName:      filepath.Base(path),
		AbsPath:       path,
		Type:          mediatype.TypeForExt(filepath.Base(path)),
		FileSizeBytes: n,
		CreatedAt:     s.clock.Now(),
	}
	mediaID, err := s.store.CreateMedia(ctx, media)
	if err != nil {
		os.Remove(path)
		return nil, nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = mediaID

	cleanup := func() {
		os.Remove(path)
		_ = s.store.HardDeleteMedia(ctx, media.ID)
	}

	if err := ImportMediaFile(ctx, s.store, media, s.prober, s.thumbGen); err != nil {
		cleanup()
		return nil, nil, err
	}

	return media, cleanup, nil
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

	s.logger.Info("podcast feed check starting", "count", len(feeds))

	var wg sync.WaitGroup
	for _, feed := range feeds {
		wg.Add(1)
		go func(f model.PodcastFeed) {
			defer wg.Done()
			if err := s.checkFeed(ctx, f); err != nil {
				s.logger.Warn("podcast feed check failed", "feed_id", f.ID, "feed_url", f.FeedURL, "err", err)
			} else {
				s.logger.Info("podcast feed check ok", "feed_id", f.ID, "feed_url", f.FeedURL)
			}
		}(feed)
	}
	wg.Wait()

	s.logger.Info("podcast feed check finished", "count", len(feeds))
	return nil
}

func (s *podcastService) checkFeed(ctx context.Context, feed model.PodcastFeed) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.FeedURL, nil)
	if err != nil {
		return fmt.Errorf("build request for feed %d: %w", feed.ID, err)
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

	parsed, err := s.parseFeedReader(resp.Body)
	if err != nil {
		return err
	}

	if err := s.updateFeedFromParsed(ctx, &feed, parsed, resp.Header.Get("ETag")); err != nil {
		return err
	}

	if err := s.upsertFeedEpisodes(ctx, &feed, parsed); err != nil {
		// Non-fatal: continue.
	}

	if parsed.ImageURL != "" {
		set, err := s.store.GetSetByID(ctx, feed.SetID)
		if err == nil && set != nil {
			setPath := filepath.Join(s.mediaRoot, set.RootPath)
			s.downloadCover(s.httpClient, parsed.ImageURL, setPath)
		}
	}

	return nil
}

func (s *podcastService) updateFeedFromParsed(ctx context.Context, feed *model.PodcastFeed, parsed *podcast.ParsedFeed, etag string) error {
	feed.Title = parsed.Title
	feed.Description = parsed.Description
	feed.ImageURL = parsed.ImageURL
	feed.LastETag = etag
	now := s.clock.Now()
	feed.LastCheckedAt = &now
	return s.store.UpdateFeed(ctx, feed)
}

func (s *podcastService) upsertFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, parsed *podcast.ParsedFeed) error {
	for _, ep := range parsed.Episodes {
		existing, err := s.store.GetEpisodeByGUID(ctx, feed.ID, ep.GUID)
		if err != nil {
			continue
		}
		if existing == nil {
			episode := &model.PodcastEpisode{
				FeedID:          feed.ID,
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
