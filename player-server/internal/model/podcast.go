// Package model defines domain entities for podcast feeds and episodes.
package model

import "time"

// PodcastFeed is a catalog feed: an RSS/Atom feed the server fetches,
// linked to the shared podcast set. Who listens to it is recorded per user
// in the subscription registers (PodcastSubscription), not here.
type PodcastFeed struct {
	ID    int64 `json:"id"`
	SetID int64 `json:"set_id"`
	// FeedKey is the stable identity of the feed (podcast.FeedKey, or
	// "legacy-feed:<id>" for a stored URL that has none). It is minted once
	// and never changes; FeedURL is only where the feed is fetched from.
	FeedKey string `json:"feed_key"`
	// MergedInto is the id of the catalog row this one was merged into, or
	// nil. A merged row is no longer listed or refreshed.
	MergedInto *int64 `json:"-"`
	// CreatedBy is the user whose sync subscription created the row, nil
	// for rows created by an admin through POST /api/podcasts. It decides
	// whether fetching goes through the outbound address policy.
	CreatedBy *int64 `json:"-"`
	// Subscribed is the caller's subscription register for FeedKey; only
	// filled by the legacy feed listing.
	Subscribed           bool       `json:"subscribed"`
	FeedURL              string     `json:"feed_url"`
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	ImageURL             string     `json:"image_url"`
	LastCheckedAt        *time.Time `json:"last_checked_at"`
	LastETag             string     `json:"last_etag"`
	CheckIntervalMinutes int        `json:"check_interval_minutes"`
	AutoDownload         bool       `json:"auto_download"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	NextCheckAt          *time.Time `json:"next_check_at"`
	CreatedAt            time.Time  `json:"created_at"`
}

// PodcastEpisode represents an individual episode from a feed.
type PodcastEpisode struct {
	ID      int64  `json:"id"`
	FeedID  int64  `json:"feed_id"`
	MediaID *int64 `json:"media_id"`
	GUID    string `json:"guid"`
	// EpisodeKey is the stable identity of the episode within its feed
	// (guid:, enc: or legacy-episode: key, see podcast.EpisodeKeys).
	EpisodeKey string `json:"episode_key"`
	// EnclosureKey is the enclosure key of the latest feed document that
	// carried the episode, "" when unknown. Not part of the API.
	EnclosureKey    string     `json:"-"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	PublishedAt     *time.Time `json:"published_at"`
	EpisodeURL      string     `json:"episode_url"`
	DurationSeconds *float64   `json:"duration_seconds"`
	FileSize        *int64     `json:"file_size"`
	FileName        string     `json:"file_name"`
	IsDownloaded    bool       `json:"is_downloaded"`
	CreatedAt       time.Time  `json:"created_at"`
}

// PodcastEpisodeWithStatus is a PodcastEpisode augmented with the caller's
// episode state (the completion and position registers, contract §10).
type PodcastEpisodeWithStatus struct {
	PodcastEpisode
	// FeedKey is the identity of the episode's feed.
	FeedKey string `json:"feed_key"`
	// IsCompleted is the caller's completion register, false when unset.
	IsCompleted bool `json:"is_completed"`
	// PositionSeconds is the caller's position register, 0 when unset.
	PositionSeconds float64 `json:"position_seconds"`
}

// Completion filters of the episode views (contract §6.3).
const (
	// PodcastFilterUncompleted lists episodes that are not completed,
	// started ones included; it is the default.
	PodcastFilterUncompleted = "uncompleted"
	// PodcastFilterCompleted lists completed episodes.
	PodcastFilterCompleted = "completed"
	// PodcastFilterAll lists every episode.
	PodcastFilterAll = "all"
)

// PodcastEpisodeView is one episode of a subscription or all-episodes view
// (contract §6.3): catalog metadata of an episode of a feed the caller is
// subscribed to, with the caller's episode state.
type PodcastEpisodeView struct {
	// ID is the catalog episode id, for the routes that still take it
	// (server download); it is never an identity.
	ID              int64      `json:"id"`
	FeedKey         string     `json:"feed_key"`
	EpisodeKey      string     `json:"episode_key"`
	FeedTitle       string     `json:"feed_title"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	PublishedAt     *time.Time `json:"published_at"`
	DurationSeconds *float64   `json:"duration_seconds"`
	EpisodeURL      string     `json:"episode_url"`
	// Completed, CompletionSource and PositionSeconds are the caller's
	// registers; unset registers read as false, "" and 0.
	Completed        bool    `json:"completed"`
	CompletionSource string  `json:"completion_source"`
	PositionSeconds  float64 `json:"position_seconds"`
	// ServerCopy is set only when a non-deleted server copy exists and the
	// caller may read the podcast set.
	ServerCopy *PodcastServerCopy `json:"server_copy"`
	// SetID is the catalog feed's set, for the permission check of
	// ServerCopy.
	SetID int64 `json:"-"`
}

// PodcastServerCopy names the media row of an episode's server copy.
type PodcastServerCopy struct {
	MediaID int64 `json:"media_id"`
}
