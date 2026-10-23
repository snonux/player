package model

import "time"

// MusicHistorySettings controls the separate history projection, not play counters.
type MusicHistorySettings struct {
	Recording     bool `json:"recording"`
	RetentionDays int  `json:"retention_days"`
}

// MusicHistoryEvent is one qualified play. Session identifiers remain private.
type MusicHistoryEvent struct {
	ID       int64     `json:"id"`
	MediaID  int64     `json:"media_id"`
	PlayedAt time.Time `json:"played_at"`
	Title    string    `json:"title"`
	Artist   string    `json:"artist,omitempty"`
}
