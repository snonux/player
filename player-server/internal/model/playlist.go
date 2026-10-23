package model

// Playlist is an account-owned document; metadata aggregates are derived per viewer.
type Playlist struct {
	ArtworkRevision string          `json:"artwork_revision,omitempty"`
	Source          *PlaylistSource `json:"source,omitempty"`
	OwnerID         int64           `json:"owner_id"`
	OwnerName       string          `json:"owner_name"`
	Visibility      string          `json:"visibility"`
	CanEdit         bool            `json:"can_edit"`
	CanTransfer     bool            `json:"can_transfer"`
	ID              string          `json:"playlist_id"`
	Name            string          `json:"name"`
	Comment         string          `json:"comment"`
	Entries         []PlaylistEntry `json:"entries,omitempty"`
}

// PlaylistEntry preserves occurrence identity and order, including duplicate songs.
type PlaylistEntry struct {
	ID      string `json:"entry_id"`
	MediaID int64  `json:"media_id"`
}

// PlaylistSource is editable-only filesystem binding status.
type PlaylistSource struct {
	Path        string `json:"path"`
	Base        string `json:"base"`
	Status      string `json:"status"`
	Unavailable int    `json:"unavailable"`
	Rejected    int    `json:"rejected"`
}
