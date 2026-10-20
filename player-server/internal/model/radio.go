package model

import (
	"regexp"
	"strings"
	"time"
)

// RadioStation is an internet radio station from the Radio Browser
// directory. StreamURL and FaviconURL are the station's own addresses; they
// are only used by the server, clients get relay URLs instead.
type RadioStation struct {
	UUID        string
	Name        string
	StreamURL   string
	Homepage    string
	FaviconURL  string
	Country     string
	CountryCode string
	Language    string
	Tags        []string
	Codec       string
	Bitrate     int
	Votes       int
	// BookmarkedAt is set on stations the user bookmarked.
	BookmarkedAt *time.Time
}

// RadioBookmark is a station a user bookmarked, with the station data as it
// was when bookmarked, so bookmarks keep working without the directory.
type RadioBookmark struct {
	UserID    int64
	Station   RadioStation
	CreatedAt time.Time
}

// RadioTag is a directory tag (genre) with its number of stations.
type RadioTag struct {
	Name         string `json:"name"`
	StationCount int    `json:"station_count"`
}

// RadioCountry is a country with its number of stations.
type RadioCountry struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	StationCount int    `json:"station_count"`
}

// Station search orders.
const (
	RadioOrderPopular = "popular"
	RadioOrderVotes   = "votes"
	RadioOrderName    = "name"
	RadioOrderRandom  = "random"
)

// RadioQuery is a station search.
type RadioQuery struct {
	Name        string
	Tag         string
	CountryCode string
	Order       string
	Limit       int
	Offset      int
}

var (
	radioUUIDPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	radioCountryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

// ValidRadioUUID reports whether s is a station UUID in the lower-case form
// Radio Browser uses.
func ValidRadioUUID(s string) bool { return radioUUIDPattern.MatchString(s) }

// ValidRadioCountryCode reports whether s is an upper-case ISO 3166-1
// alpha-2 code.
func ValidRadioCountryCode(s string) bool { return radioCountryPattern.MatchString(s) }

// SplitRadioTags splits Radio Browser's comma-separated tag list, dropping
// empty and duplicate entries.
func SplitRadioTags(raw string) []string {
	tags := []string{}
	seen := map[string]bool{}
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		tags = append(tags, t)
	}
	return tags
}
