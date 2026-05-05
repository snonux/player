// Package podcast implements RSS/Atom feed parsing and cover downloading.
package podcast

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// Episode represents a parsed podcast episode from a feed.
type Episode struct {
	GUID            string
	Title           string
	Description     string
	PublishedAt     *time.Time
	EpisodeURL      string
	DurationSeconds *float64
	FileSize        *int64
}

// ParsedFeed holds the result of parsing a podcast RSS/Atom feed.
type ParsedFeed struct {
	Title       string
	Description string
	ImageURL    string
	Episodes    []Episode
}

// ParseFeed fetches and parses a podcast RSS/Atom feed URL.
func ParseFeed(url string) (*ParsedFeed, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse feed %q: %w", url, err)
	}
	return parsedFeedFromGoFeed(feed), nil
}

// ParseFeedReader reads and parses a podcast RSS/Atom feed from an io.Reader.
func ParseFeedReader(r io.Reader) (*ParsedFeed, error) {
	fp := gofeed.NewParser()
	feed, err := fp.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	return parsedFeedFromGoFeed(feed), nil
}

func parsedFeedFromGoFeed(feed *gofeed.Feed) *ParsedFeed {
	result := &ParsedFeed{
		Title:       feed.Title,
		Description: feed.Description,
		ImageURL:    extractImageURL(feed),
	}

	for _, item := range feed.Items {
		ep := Episode{
			GUID:        item.GUID,
			Title:       item.Title,
			Description: item.Description,
		}

		if item.PublishedParsed != nil {
			t := *item.PublishedParsed
			ep.PublishedAt = &t
		}

		// Extract enclosure URL.
		if len(item.Enclosures) > 0 {
			ep.EpisodeURL = item.Enclosures[0].URL
			if item.Enclosures[0].Length != "" {
				var size int64
				if _, err := fmt.Sscanf(item.Enclosures[0].Length, "%d", &size); err == nil {
					ep.FileSize = &size
				}
			}
		}

		// Extract duration from iTunes extension if present.
		if item.ITunesExt != nil && item.ITunesExt.Duration != "" {
			dur := parseDuration(item.ITunesExt.Duration)
			if dur > 0 {
				ep.DurationSeconds = &dur
			}
		}

		result.Episodes = append(result.Episodes, ep)
	}

	return result
}

// extractImageURL looks for podcast cover images in RSS 2.0, Atom, and iTunes feed metadata.
func extractImageURL(feed *gofeed.Feed) string {
	// iTunes image (most common for podcasts).
	if feed.ITunesExt != nil && feed.ITunesExt.Image != "" {
		return feed.ITunesExt.Image
	}

	// RSS 2.0 <image><url>.
	if feed.Image != nil && feed.Image.URL != "" {
		return feed.Image.URL
	}

	return ""
}

// parseDuration converts an iTunes duration string (HH:MM:SS or MM:SS) to seconds.
func parseDuration(s string) float64 {
	parts := strings.Split(s, ":")
	var hours, minutes, seconds int

	switch len(parts) {
	case 3:
		fmt.Sscanf(parts[0], "%d", &hours)
		fmt.Sscanf(parts[1], "%d", &minutes)
		fmt.Sscanf(parts[2], "%d", &seconds)
	case 2:
		fmt.Sscanf(parts[0], "%d", &minutes)
		fmt.Sscanf(parts[1], "%d", &seconds)
	case 1:
		fmt.Sscanf(parts[0], "%d", &seconds)
	}

	return float64(hours*3600 + minutes*60 + seconds)
}
