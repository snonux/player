package service

import (
	"path"
	"strings"

	"github.com/snonux/player/internal/model"
)

// MusicSearchResult groups matches from an already permission-filtered catalog.
type MusicSearchResult struct {
	Songs   []model.Media `json:"songs"`
	Albums  []Album       `json:"albums"`
	Artists []Artist      `json:"artists"`
}

// SearchMusic matches literal text without changing catalog identities or ordering.
// Callers must derive albums through MusicAlbums before invoking this function.
func SearchMusic(albums []Album, query, artist, album string) MusicSearchResult {
	result := MusicSearchResult{Songs: []model.Media{}, Albums: []Album{}, Artists: []Artist{}}
	query, artist, album = normalized(query), normalized(artist), normalized(album)
	if query == "" && artist == "" && album == "" {
		return result
	}
	for _, release := range albums {
		var people []string
		for _, song := range release.Songs {
			names := searchArtistNames(song)
			people = append(people, names...)
			title := strings.TrimSuffix(song.FileName, path.Ext(song.FileName))
			if song.Music != nil && strings.TrimSpace(song.Music.Title) != "" {
				title = song.Music.Title
			}
			fields := append([]string{title, release.Title}, names...)
			if searchContains(query, fields...) && searchContains(artist, names...) && searchContains(album, release.Title) {
				result.Songs = append(result.Songs, song)
			}
		}
		people = append(people, release.Artist)
		if searchContains(query, append([]string{release.Title}, people...)...) && searchContains(artist, people...) && searchContains(album, release.Title) {
			release.Songs, release.Discs, release.SongCredits = nil, nil, nil
			result.Albums = append(result.Albums, release)
		}
	}
	for _, person := range MusicArtists(albums) {
		if !containsRole(person.Roles, "track_artist") && !containsRole(person.Roles, "album_artist") {
			continue
		}
		if !searchContains(query, person.Name) || !searchContains(artist, person.Name) {
			continue
		}
		matches := album == ""
		for _, release := range person.Albums {
			if searchContains(album, release.Title) {
				matches = true
			}
		}
		if matches {
			person.Albums = nil
			result.Artists = append(result.Artists, person)
		}
	}
	return result
}

func searchArtistNames(song model.Media) []string {
	names := []string{}
	for _, person := range MusicCredits(song) {
		if containsRole(person.Roles, "track_artist") || containsRole(person.Roles, "album_artist") {
			names = append(names, person.Name)
		}
	}
	if song.Music != nil {
		names = append(names, song.Music.Artist, song.Music.AlbumArtist)
	}
	return names
}

func searchContains(query string, values ...string) bool {
	if query == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(normalized(value), query) {
			return true
		}
	}
	return false
}
