package model

// MusicMetadata holds embedded music tags, independently of file identity and user tags.
// Empty fields represent missing tags; numbers are positive or zero (unknown).
type MusicMetadata struct {
	Title        string   `json:"title,omitempty"`
	Artist       string   `json:"artist,omitempty"`
	Artists      []string `json:"artists,omitempty"`
	AlbumArtist  string   `json:"album_artist,omitempty"`
	AlbumArtists []string `json:"album_artists,omitempty"`
	Album        string   `json:"album,omitempty"`
	Track        int      `json:"track,omitempty"`
	TrackTotal   int      `json:"track_total,omitempty"`
	Disc         int      `json:"disc,omitempty"`
	DiscTotal    int      `json:"disc_total,omitempty"`
	Date         string   `json:"date,omitempty"`
	Year         int      `json:"year,omitempty"`
	Genres       []string `json:"genres,omitempty"`
}
