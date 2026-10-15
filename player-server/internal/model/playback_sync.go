package model

// PlaybackSync is a per-user snapshot. Milliseconds are UTC Unix instants,
// captured at edit time and preserved through retries. Equal timestamps keep
// the stored value, making repeated requests idempotent.
type PlaybackSync struct {
	Progress  *SyncedProgress  `json:"progress,omitempty"`
	Bookmarks []SyncedBookmark `json:"bookmarks"`
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
