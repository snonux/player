package model

// PlaybackSync is a per-user snapshot. Milliseconds are UTC Unix instants,
// captured at edit time and preserved through retries. Equal timestamps keep
// the stored value, making repeated requests idempotent.
type PlaybackSync struct {
	MusicPlays         []SyncedMusicPlay `json:"music_plays,omitempty"`
	AcceptedMusicPlays []string          `json:"accepted_music_plays,omitempty"`
	Progress           *SyncedProgress   `json:"progress,omitempty"`
	Bookmarks          []SyncedBookmark  `json:"bookmarks"`
}
type SyncedProgress struct {
	PositionSeconds float64 `json:"position_seconds"`
	Finished        bool    `json:"finished"`
	ObservedMS      int64   `json:"observed_ms"`
}
type SyncedBookmark struct {
	Key             string  `json:"sync_key"`
	PositionSeconds float64 `json:"position_seconds"`
	Label           string  `json:"label"`
	ObservedMS      int64   `json:"observed_ms"`
	Deleted         bool    `json:"deleted"`
}

// SyncedMusicPlay is a durable offline qualification, separate from resume edits.
// Clients emit one unique ID only after 60 capped listening seconds.
type SyncedMusicPlay struct {
	ID                 string  `json:"id"`
	ObservedMS         int64   `json:"observed_ms"`
	AccumulatedSeconds float64 `json:"accumulated_seconds"`
}
