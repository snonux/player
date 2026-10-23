package service

import (
	"sort"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// ArtistCredit is one explicitly tagged person and their roles on a song.
type ArtistCredit struct {
	Key   string   `json:"artist_key"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

// Artist is a permission-filtered catalog of credited releases and songs.
type Artist struct {
	Rating     int  `json:"rating"`
	IsFavorite bool `json:"is_favorite"`
	ArtistCredit
	AlbumCount int           `json:"album_count"`
	SongCount  int           `json:"song_count"`
	Albums     []ArtistAlbum `json:"albums,omitempty"`
}

// ArtistAlbum describes a release and the person's roles on its songs.
type ArtistAlbum struct {
	Album
	Roles []string `json:"roles"`
}

// MusicCredits preserves structured names without splitting human punctuation.
// Album artist falls back to the track artist, matching album grouping.
func MusicCredits(song model.Media) []ArtistCredit {
	m := song.Music
	if m == nil {
		m = &model.MusicMetadata{}
	}
	people := map[string]*ArtistCredit{}
	add := func(role string, names []string) {
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := catalogKey("ar", normalized(name))
			person := people[key]
			if person == nil {
				person = &ArtistCredit{Key: key, Name: name}
				people[key] = person
			}
			if !containsRole(person.Roles, role) {
				person.Roles = append(person.Roles, role)
			}
		}
	}
	tracks := creditNames(m.Artists, m.Artist)
	if len(tracks) == 0 {
		tracks = []string{"Unknown artist"}
	}
	albums := creditNames(m.AlbumArtists, m.AlbumArtist)
	if len(albums) == 0 {
		if m.Compilation {
			albums = []string{"Various Artists"}
		} else {
			albums = tracks
		}
	}
	add("track_artist", tracks)
	add("album_artist", albums)
	add("composer", m.Composers)
	add("performer", m.Performers)
	result := make([]ArtistCredit, 0, len(people))
	for _, person := range people {
		result = append(result, *person)
	}
	sort.Slice(result, func(i, j int) bool { return normalized(result[i].Name) < normalized(result[j].Name) })
	return result
}

// MusicArtists derives all counts and links from already authorized albums.
func MusicArtists(albums []Album) []Artist {
	people := map[string]*Artist{}
	songIDs := map[string]map[int64]bool{}
	for _, album := range albums {
		credits := map[string]*ArtistAlbum{}
		for _, song := range album.Songs {
			for _, credit := range MusicCredits(song) {
				person := people[credit.Key]
				if person == nil {
					person = &Artist{ArtistCredit: credit}
					people[credit.Key] = person
					songIDs[credit.Key] = map[int64]bool{}
				}
				songIDs[credit.Key][song.ID] = true
				release := credits[credit.Key]
				if release == nil {
					summary := album
					summary.Discs = nil
					summary.Songs = nil
					summary.SongCredits = nil
					release = &ArtistAlbum{Album: summary}
					credits[credit.Key] = release
				}
				for _, role := range credit.Roles {
					if !containsRole(person.Roles, role) {
						person.Roles = append(person.Roles, role)
					}
					if !containsRole(release.Roles, role) {
						release.Roles = append(release.Roles, role)
					}
				}
			}
		}
		for key, release := range credits {
			people[key].Albums = append(people[key].Albums, *release)
		}
	}
	result := make([]Artist, 0, len(people))
	for key, person := range people {
		person.AlbumCount = len(person.Albums)
		person.SongCount = len(songIDs[key])
		sort.Strings(person.Roles)
		result = append(result, *person)
	}
	sort.Slice(result, func(i, j int) bool { return normalized(result[i].Name) < normalized(result[j].Name) })
	return result
}

func creditNames(names []string, fallback string) []string {
	if len(names) > 0 {
		return names
	}
	if strings.TrimSpace(fallback) != "" {
		return []string{fallback}
	}
	return nil
}
func containsRole(roles []string, role string) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}
