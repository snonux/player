package model

// MusicMetadata holds embedded music tags, independently of file identity and user tags.
// Empty fields represent missing tags; numbers are positive or zero (unknown).
type MusicMetadata struct {
	DiscSubtitle   string        `json:"disc_subtitle,omitempty"`
	Compilation    bool          `json:"compilation,omitempty"`
	ReleaseDate    string        `json:"release_date,omitempty"`
	OriginalDate   string        `json:"original_date,omitempty"`
	RecordingDate  string        `json:"recording_date,omitempty"`
	ReleaseVersion string        `json:"release_version,omitempty"`
	ReleaseTypes   []string      `json:"release_types,omitempty"`
	Labels         []string      `json:"labels,omitempty"`
	CatalogNumbers []string      `json:"catalog_numbers,omitempty"`
	Media          []string      `json:"media,omitempty"`
	Moods          []string      `json:"moods,omitempty"`
	Grouping       string        `json:"grouping,omitempty"`
	Comment        string        `json:"comment,omitempty"`
	Credits        []MusicCredit `json:"credits,omitempty"`
	Composers      []string      `json:"composers,omitempty"`
	Performers     []string      `json:"performers,omitempty"`
	ReleaseMBID    string        `json:"release_mbid,omitempty"`
	Title          string        `json:"title,omitempty"`
	Artist         string        `json:"artist,omitempty"`
	Artists        []string      `json:"artists,omitempty"`
	AlbumArtist    string        `json:"album_artist,omitempty"`
	AlbumArtists   []string      `json:"album_artists,omitempty"`
	Album          string        `json:"album,omitempty"`
	Track          int           `json:"track,omitempty"`
	TrackTotal     int           `json:"track_total,omitempty"`
	Disc           int           `json:"disc,omitempty"`
	DiscTotal      int           `json:"disc_total,omitempty"`
	Date           string        `json:"date,omitempty"`
	Year           int           `json:"year,omitempty"`
	Genres         []string      `json:"genres,omitempty"`
}

// MusicCredit preserves a named embedded role and its structured participants.
// These credits are descriptive metadata, not additional catalog artist roles.
type MusicCredit struct {
	Role  string   `json:"role"`
	Names []string `json:"names"`
}
