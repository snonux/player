package service

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
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

	// fetchPolicy controls HTTP-level retry/backoff for feed fetches.
	// Exposed as a struct field so tests can shrink delays and shorten the
	// per-host backoff window; production callers get the defaults defined
	// by the exported FeedFetchPolicy* constants below.
	fetchPolicy FeedFetchPolicy

	// hostFailures records the most recent transport / 5xx failure timestamp
	// per host. When a fetch attempt finds an entry within fetchPolicy.HostBackoff
	// it is skipped entirely (still bumps consecutive_failures on the feed)
	// so a single broken host does not get hammered by every scheduled tick
	// nor block scheduling time for the other feeds. Guarded by hostFailuresMu.
	hostFailures   map[string]time.Time
	hostFailuresMu sync.Mutex

	*podcastSubscriptionService
	*podcastEpisodeService
	*podcastFeedChecker
}

// DefaultHTTPClientTimeout is the recommended timeout for production HTTP clients
// passed into the podcast service. The service no longer constructs a fallback
// client — callers must inject an explicit *http.Client (dependency inversion);
// this constant is exported so production wiring can use a sensible default.
const DefaultHTTPClientTimeout = 30 * time.Second

// FeedFetchPolicy configures HTTP-level retry and per-host backoff for podcast
// feed checks. Zero values are replaced with the FeedFetchPolicy* defaults so
// tests can override individual fields without filling the whole struct.
type FeedFetchPolicy struct {
	// MaxAttempts is the total number of HTTP attempts per fetch (including
	// the first one). 1 disables retry. Must be >= 1.
	MaxAttempts int
	// InitialBackoff is the wait before the second attempt. Each subsequent
	// retry doubles this value (capped at MaxBackoff).
	InitialBackoff time.Duration
	// MaxBackoff bounds the exponential backoff so a long run of failures
	// does not stretch a single fetch beyond reasonable time.
	MaxBackoff time.Duration
	// HostBackoff is the cool-off window after a host has failed. While
	// inside the window further fetches for the same host are skipped.
	HostBackoff time.Duration
}

// Exported default policy constants — tests refer to them to assert that the
// constructor wires the production defaults, and production wiring may also
// consume them directly.
const (
	FeedFetchPolicyMaxAttempts    = 3
	FeedFetchPolicyInitialBackoff = 500 * time.Millisecond
	FeedFetchPolicyMaxBackoff     = 2 * time.Second
	FeedFetchPolicyHostBackoff    = 5 * time.Minute
)

// DefaultFeedFetchPolicy returns the production retry/backoff defaults.
func DefaultFeedFetchPolicy() FeedFetchPolicy {
	return FeedFetchPolicy{
		MaxAttempts:    FeedFetchPolicyMaxAttempts,
		InitialBackoff: FeedFetchPolicyInitialBackoff,
		MaxBackoff:     FeedFetchPolicyMaxBackoff,
		HostBackoff:    FeedFetchPolicyHostBackoff,
	}
}

// normalize fills in defaults for any zero-valued fields so callers can pass
// a partial policy (typically from tests overriding one field).
func (p FeedFetchPolicy) normalize() FeedFetchPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = FeedFetchPolicyMaxAttempts
	}
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = FeedFetchPolicyInitialBackoff
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = FeedFetchPolicyMaxBackoff
	}
	if p.HostBackoff <= 0 {
		p.HostBackoff = FeedFetchPolicyHostBackoff
	}
	return p
}

// NewPodcastService creates a PodcastService with the given dependencies.
// checkInterval should be the number of minutes between background feed checks.
// httpClient is required and must not be nil; the service does not construct
// a default client so callers explicitly own the HTTP timeout / transport policy.
func NewPodcastService(store PodcastServiceStore, clk clock.Clock, mediaRoot string, helper *accessHelper, prober probe.Prober, thumbGen thumb.Generator, httpClient *http.Client, checkInterval int) *podcastService {
	return NewPodcastServiceWithLogger(store, clk, mediaRoot, helper, prober, thumbGen, httpClient, checkInterval, slog.Default())
}

// NewPodcastServiceWithLogger creates a PodcastService with an injected logger.
// httpClient is required and must not be nil — passing nil will panic on first
// use. This is intentional: the service depends on an injected client per DIP
// and refuses to silently fabricate one.
func NewPodcastServiceWithLogger(store PodcastServiceStore, clk clock.Clock, mediaRoot string, helper *accessHelper, prober probe.Prober, thumbGen thumb.Generator, httpClient *http.Client, checkInterval int, logger *slog.Logger) *podcastService {
	if checkInterval <= 0 {
		checkInterval = 60
	}
	if logger == nil {
		logger = slog.Default()
	}
	if httpClient == nil {
		panic("service.NewPodcastService: httpClient must not be nil")
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
		fetchPolicy:   DefaultFeedFetchPolicy(),
		hostFailures:  make(map[string]time.Time),
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
