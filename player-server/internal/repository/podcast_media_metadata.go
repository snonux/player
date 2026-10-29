package repository

import (
	"encoding/json"
	"fmt"
	"github.com/snonux/player/internal/model"
	"time"
)

// The correlated JSON projection receives SQLite timestamp text, not JSON's
// RFC3339. Decode it with the stored layouts before exposing time.Time in JSON.
func decodePodcastMediaMetadata(raw string) (*model.PodcastMediaMetadata, error) {
	metadata := new(model.PodcastMediaMetadata)
	wire := struct {
		*model.PodcastMediaMetadata
		PublishedAt *string `json:"published_at"`
	}{PodcastMediaMetadata: metadata}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return nil, fmt.Errorf("decode podcast metadata: %w", err)
	}
	if wire.PublishedAt != nil {
		for _, layout := range []string{goTimeLayout, time.RFC3339Nano, "2006-01-02 15:04:05.999999999"} {
			if parsed, err := time.ParseInLocation(layout, *wire.PublishedAt, time.UTC); err == nil {
				utc := parsed.UTC()
				metadata.PublishedAt = &utc
				break
			}
		}
	}
	return metadata, nil
}
