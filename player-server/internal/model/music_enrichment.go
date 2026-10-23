package model

import "time"

// MusicEnrichment is optional external descriptive metadata, never embedded tags.
type MusicEnrichment struct {
	Description string              `json:"description,omitempty"`
	ImageURL    string              `json:"image_url,omitempty"`
	Source      string              `json:"source,omitempty"`
	Links       []MusicMetadataLink `json:"links,omitempty"`
	FetchedAt   *time.Time          `json:"fetched_at,omitempty"`
}

// MusicMetadataLink identifies an attributed external metadata page.
type MusicMetadataLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
