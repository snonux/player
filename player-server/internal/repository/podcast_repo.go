package repository

import (
	"context"
	"time"

	"github.com/snonux/player/internal/model"
)

// PodcastRepo manages podcast feeds, episodes, and per-user status.
type PodcastRepo interface {
	// CreateFeed stores a new podcast feed and returns its database ID.
	CreateFeed(ctx context.Context, feed *model.PodcastFeed) (int64, error)
	// UpdateFeed replaces mutable fields for a podcast feed.
	UpdateFeed(ctx context.Context, feed *model.PodcastFeed) error
	// DeleteFeed removes a podcast feed by database ID.
	DeleteFeed(ctx context.Context, id int64) error
	// GetFeedByID returns a podcast feed by database ID.
	GetFeedByID(ctx context.Context, id int64) (*model.PodcastFeed, error)
	// GetFeedBySetID returns a podcast feed linked to a set.
	GetFeedBySetID(ctx context.Context, setID int64) (*model.PodcastFeed, error)
	// ListFeedsBySetID returns all podcast feeds linked to a set.
	ListFeedsBySetID(ctx context.Context, setID int64) ([]model.PodcastFeed, error)
	// ListFeeds returns all podcast feeds.
	ListFeeds(ctx context.Context) ([]model.PodcastFeed, error)
	// ListFeedsNeedingCheck returns feeds whose next_check_at has passed and
	// last_checked_at is older than the given threshold.
	ListFeedsNeedingCheck(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error)

	// CreateEpisode stores a new podcast episode and returns its database ID.
	CreateEpisode(ctx context.Context, episode *model.PodcastEpisode) (int64, error)
	// GetEpisodeByID returns a podcast episode by database ID.
	GetEpisodeByID(ctx context.Context, id int64) (*model.PodcastEpisode, error)
	// GetEpisodeByGUID returns an episode by feed ID and GUID.
	GetEpisodeByGUID(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error)
	// ListEpisodesByFeed returns episodes for a feed with pagination.
	ListEpisodesByFeed(ctx context.Context, feedID int64, limit, offset int) ([]model.PodcastEpisode, error)
	// ListEpisodeMediaIDs returns the media IDs linked to downloaded episodes
	// of the given feeds, unpaged, so callers never miss rows between pages.
	ListEpisodeMediaIDs(ctx context.Context, feedIDs []int64) ([]int64, error)
	// ListEpisodesByFeedIDsWithStatus returns the episodes of several feeds
	// with the user's completion and position registers, applying a global
	// limit/offset.
	ListEpisodesByFeedIDsWithStatus(ctx context.Context, userID int64, feedIDs []int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error)
	// UpdateEpisodeMedia links an episode to a media row after downloading.
	UpdateEpisodeMedia(ctx context.Context, episodeID, mediaID int64, fileName string) error
	// DeleteEpisodesByFeed removes all episodes for a feed.
	DeleteEpisodesByFeed(ctx context.Context, feedID int64) error

	// ListEpisodesWithStatus returns a feed's episodes with the user's
	// completion and position registers.
	ListEpisodesWithStatus(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error)
}
