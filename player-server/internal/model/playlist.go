package model

// Playlist is an account-owned document; metadata aggregates are derived per viewer.
type Playlist struct {
	ID      string          `json:"playlist_id"`
	Name    string          `json:"name"`
	Comment string          `json:"comment"`
	Entries []PlaylistEntry `json:"entries,omitempty"`
}

// PlaylistEntry preserves occurrence identity and order, including duplicate songs.
type PlaylistEntry struct {
	ID      string `json:"entry_id"`
	MediaID int64  `json:"media_id"`
}
