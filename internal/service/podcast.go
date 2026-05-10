package service

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"codeberg.org/snonux/player/internal/clock"
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
	parseFeed       func(*http.Client, string) (*podcast.ParsedFeed, error)
	parseFeedReader func(io.Reader) (*podcast.ParsedFeed, error)
	downloadCover   func(*http.Client, string, string) error
	*podcastSubscriptionService
	*podcastEpisodeService
	*podcastFeedChecker
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
	s.podcastSubscriptionService = newPodcastSubscriptionService(s)
	s.podcastEpisodeService = newPodcastEpisodeService(s)
	s.podcastFeedChecker = newPodcastFeedChecker(s)
	return s
}
