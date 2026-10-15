package model

import "time"

// Bookmark is a named audio/video position belonging to one user.
type Bookmark struct {
	ID              int64     `json:"id"`
	MediaID         int64     `json:"media_id"`
	UserID          int64     `json:"-"`
	PositionSeconds float64   `json:"position_seconds"`
	Label           string    `json:"label"`
	CreatedAt       time.Time `json:"created_at"`
}
