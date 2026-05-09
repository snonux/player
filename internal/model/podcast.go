// Package model defines domain entities for podcast feeds and episodes.
package model

import "time"

// PodcastFeed represents a subscribed RSS/Atom feed linked to a set.
type PodcastFeed struct {
	ID                   int64      `json:"id"`
	SetID                int64      `json:"set_id"`
	FeedURL              string     `json:"feed_url"`
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	ImageURL             string     `json:"image_url"`
	LastCheckedAt        *time.Time `json:"last_checked_at"`
	LastETag             string     `json:"last_etag"`
	CheckIntervalMinutes int        `json:"check_interval_minutes"`
	AutoDownload         bool       `json:"auto_download"`
	CreatedAt            time.Time  `json:"created_at"`
}

// PodcastEpisode represents an individual episode from a feed.
type PodcastEpisode struct {
	ID              int64      `json:"id"`
	FeedID          int64      `json:"feed_id"`
	MediaID         *int64     `json:"media_id"`
	GUID            string     `json:"guid"`
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

// PodcastStatus tracks per-user completion and progress for an episode.
type PodcastStatus struct {
	UserID          int64     `json:"user_id"`
	EpisodeID       int64     `json:"episode_id"`
	IsCompleted     bool      `json:"is_completed"`
	PositionSeconds float64   `json:"position_seconds"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PodcastEpisodeWithStatus is a PodcastEpisode augmented with per-user status.
type PodcastEpisodeWithStatus struct {
	PodcastEpisode
	IsCompleted     bool    `json:"is_completed"`
	PositionSeconds float64 `json:"position_seconds"`
}
