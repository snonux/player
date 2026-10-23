package service

import (
	"sort"

	"codeberg.org/snonux/player/internal/model"
)

// MusicMix is a finite, explained local-library queue, separate from live radio.
type MusicMix struct {
	Kind      string        `json:"kind"`
	Key       string        `json:"key"`
	Title     string        `json:"title"`
	Context   string        `json:"context"`
	MinRating int           `json:"min_rating"`
	Songs     []model.Media `json:"songs"`
}

type mixRankedSong struct {
	song  model.Media
	score int
	seed  bool
}

// BuildMusicMix ranks only the caller's authorized, personally rated catalog.
// Seed songs qualify on the same rating constraint as every other song.
func BuildMusicMix(albums []Album, kind, key string, minRating int) (MusicMix, bool) {
	result := MusicMix{Kind: kind, Key: key, MinRating: minRating, Songs: []model.Media{}, Context: "From your library · shared artist credits and genres · up to 50 distinct songs"}
	var seed *relatedCandidate
	candidates := relatedCandidates(albums, kind)
	for i := range candidates {
		if candidates[i].item.Key == key {
			seed = &candidates[i]
			break
		}
	}
	if seed == nil {
		if kind == "artist" {
			for _, artist := range MusicArtists(albums) {
				if artist.Key == key {
					result.Title = artist.Name
					return result, true
				}
			}
		}
		return result, false
	}
	result.Title = seed.item.Title
	if kind == "artist" {
		seed.tags["credit:"+key] = "Artist/credit: " + result.Title
	}

	ranked := []mixRankedSong{}
	for _, candidate := range relatedCandidates(albums, "song") {
		song := *candidate.item.Song
		if song.Rating < minRating {
			continue
		}
		score := 0
		for tag := range candidate.tags {
			if _, ok := seed.tags[tag]; ok {
				score++
			}
		}
		// A readable seed with no meaningful metadata cannot establish a mix.
		if score == 0 {
			continue
		}
		ranked = append(ranked, mixRankedSong{song: song, score: score, seed: kind == "song" && candidate.item.Key == key})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].seed != ranked[j].seed {
			return ranked[i].seed
		}
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].song.ID < ranked[j].song.ID
	})
	for _, candidate := range ranked {
		result.Songs = append(result.Songs, candidate.song)
		if len(result.Songs) == 50 {
			break
		}
	}
	return result, true
}
