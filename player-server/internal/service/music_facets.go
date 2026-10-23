package service

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/snonux/player/internal/model"
)

// MusicFilter combines metadata dimensions on one authorized song.
type MusicFilter map[string][]string

const musicFacetKeys = "genre year role release_type compilation mood grouping label"

// ParseMusicFilter validates structured facets while leaving unknown text literal.
func ParseMusicFilter(values url.Values) (MusicFilter, error) {
	result := MusicFilter{}
	for _, key := range strings.Fields(musicFacetKeys) {
		for _, value := range values[key] {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			switch key {
			case "role":
				if !containsRole([]string{"artists", "track_artist", "album_artist", "composer", "performer"}, value) {
					return nil, fmt.Errorf("Unknown artist role.")
				}
			case "compilation":
				if value != "true" && value != "false" {
					return nil, fmt.Errorf("Invalid compilation filter.")
				}
			case "year":
				if _, _, ok := musicYearRange(value); !ok {
					return nil, fmt.Errorf("Year must be a year or inclusive year range.")
				}
			}
			result[key] = append(result[key], value)
		}
	}
	return result, nil
}

// MusicFacetOptions derives choices exclusively from the authorized catalog.
func MusicFacetOptions(albums []Album) map[string][]string {
	sets := map[string]map[string]string{}
	for _, key := range strings.Fields(musicFacetKeys) {
		sets[key] = map[string]string{}
	}
	for _, album := range albums {
		for _, song := range album.Songs {
			for key, values := range musicSongFacets(song) {
				for _, value := range values {
					n := normalized(value)
					if n != "" {
						sets[key][n] = value
					}
				}
			}
		}
	}
	result := map[string][]string{}
	for _, key := range strings.Fields(musicFacetKeys) {
		result[key] = []string{}
		for _, value := range sets[key] {
			result[key] = append(result[key], value)
		}
		sort.Slice(result[key], func(i, j int) bool { return normalized(result[key][i]) < normalized(result[key][j]) })
	}
	return result
}

// FilterMusic returns catalog aggregates containing only matching authorized songs.
// Full detail endpoints must continue using the unfiltered catalog.
func FilterMusic(albums []Album, filter MusicFilter) []Album {
	if len(filter) == 0 {
		return albums
	}
	result := []Album{}
	for _, album := range albums {
		songs := []model.Media{}
		for _, song := range album.Songs {
			if musicSongMatches(song, filter) {
				songs = append(songs, song)
			}
		}
		if len(songs) == 0 {
			continue
		}
		album.Songs, album.SongCount, album.Duration = songs, len(songs), 0
		album.ListeningStats = model.ListeningStats{}
		for _, song := range songs {
			album.Duration += song.Duration
			album.ListeningStats.Add(song.ListeningStats)
		}
		album.Discs, album.SongCredits, album.Credits = nil, nil, nil
		album.CoverMediaID = 0
		for _, song := range songs {
			if song.ThumbnailPath != "" {
				album.CoverMediaID = song.ID
				break
			}
		}
		result = append(result, album)
	}
	return result
}

func musicSongMatches(song model.Media, filter MusicFilter) bool {
	facets := musicSongFacets(song)
	for key, choices := range filter {
		matched := false
		for _, choice := range choices {
			if key == "year" {
				from, to, _ := musicYearRange(choice)
				if song.Music != nil && song.Music.Year > 0 && song.Music.Year >= from && song.Music.Year <= to {
					matched = true
				}
			} else {
				for _, value := range facets[key] {
					if normalized(value) == normalized(choice) || (key == "role" && choice == "artists" && (value == "track_artist" || value == "album_artist")) {
						matched = true
					}
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func musicSongFacets(song model.Media) map[string][]string {
	m := song.Music
	if m == nil {
		m = &model.MusicMetadata{}
	}
	result := map[string][]string{"genre": m.Genres, "release_type": m.ReleaseTypes, "mood": m.Moods, "grouping": {m.Grouping}, "label": m.Labels, "compilation": {strconv.FormatBool(m.Compilation)}}
	if m.Year > 0 {
		result["year"] = []string{strconv.Itoa(m.Year)}
	}
	for _, person := range MusicCredits(song) {
		for _, role := range person.Roles {
			result["role"] = append(result["role"], role)
		}
	}
	return result
}

func musicYearRange(value string) (int, int, bool) {
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, 0, false
	}
	from, err := strconv.Atoi(parts[0])
	if err != nil || len(parts[0]) != 4 || from < 1 {
		return 0, 0, false
	}
	to := from
	if len(parts) == 2 {
		var err error
		to, err = strconv.Atoi(parts[1])
		if err != nil || len(parts[1]) != 4 {
			return 0, 0, false
		}
	}
	return from, to, to >= from && to <= 9999
}
