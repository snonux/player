package model

// RadioStation is a shared live source, distinct from library-based mixes.
type RadioStation struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	StreamURL    string `json:"stream_url"`
	HomepageURL  string `json:"homepage_url"`
	ArtworkURL   string `json:"artwork_url"`
	LocalNetwork bool   `json:"local_network"`
}
