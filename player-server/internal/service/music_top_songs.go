package service

import (
	"sort"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// ArtistTopSongs explains a local popularity ranking over caller-visible songs.
type ArtistTopSongs struct {
	Source string        `json:"source"`
	Songs  []model.Media `json:"songs"`
}

// MusicArtistTopSongs prefers qualified personal plays, falling back to the
// existing global counter. Callers must supply permission-filtered albums.
func MusicArtistTopSongs(albums []Album, key string) ArtistTopSongs {
	result := ArtistTopSongs{Source: "none", Songs: []model.Media{}}
	seen := map[int64]bool{}
	songs := []model.Media{}
	for _, album := range albums {
		for _, song := range album.Songs {
			if seen[song.ID] {
				continue
			}
			for _, credit := range MusicCredits(song) {
				if credit.Key == key {
					songs = append(songs, song)
					seen[song.ID] = true
					if song.PersonalPlayCount > 0 {
						result.Source = "personal"
					}
					break
				}
			}
		}
	}
	count := func(song model.Media) int {
		if result.Source == "personal" {
			return song.PersonalPlayCount
		}
		return song.PlayCount
	}
	for _, song := range songs {
		if count(song) > 0 {
			result.Songs = append(result.Songs, song)
		}
	}
	if result.Source == "none" && len(result.Songs) > 0 {
		result.Source = "global"
	}
	sort.Slice(result.Songs, func(i, j int) bool {
		a, b := result.Songs[i], result.Songs[j]
		if count(a) != count(b) {
			return count(a) > count(b)
		}
		if topSongTitle(a) != topSongTitle(b) {
			return topSongTitle(a) < topSongTitle(b)
		}
		return a.ID < b.ID
	})
	if len(result.Songs) > 10 {
		result.Songs = result.Songs[:10]
	}
	return result
}

func topSongTitle(song model.Media) string {
	if song.Music != nil && strings.TrimSpace(song.Music.Title) != "" {
		return normalized(song.Music.Title)
	}
	return normalized(song.FileName)
}
