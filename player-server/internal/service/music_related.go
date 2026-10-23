package service

import (
	"sort"
	"strconv"
	"strings"

	"github.com/snonux/player/internal/model"
)

// RelatedMusic is an explained local metadata match, never an acoustic estimate.
type RelatedMusic struct {
	Kind         string       `json:"kind"`
	Key          string       `json:"key"`
	Title        string       `json:"title"`
	Basis        []string     `json:"basis"`
	CoverMediaID int64        `json:"cover_media_id,omitempty"`
	Song         *model.Media `json:"song,omitempty"`
}

type relatedCandidate struct {
	item  RelatedMusic
	tags  map[string]string
	score int
}

// MusicRelated returns distinct same-kind neighbours from already authorized albums.
// The boolean distinguishes an unavailable source from a source without matches.
func MusicRelated(albums []Album, kind, key string) ([]RelatedMusic, bool) {
	candidates := relatedCandidates(albums, kind)
	var source *relatedCandidate
	for i := range candidates {
		if candidates[i].item.Key == key {
			source = &candidates[i]
			break
		}
	}
	result := []RelatedMusic{}
	if source == nil {
		// Catalog fallback identities are valid sources, but cannot suggest
		// relationships based on missing or synthetic credit names.
		if kind == "artist" {
			for _, artist := range MusicArtists(albums) {
				if artist.Key == key {
					return result, true
				}
			}
		}
		return result, false
	}
	ranked := []relatedCandidate{}
	for _, candidate := range candidates {
		if candidate.item.Key == key {
			continue
		}
		for tag, label := range candidate.tags {
			if _, ok := source.tags[tag]; ok {
				candidate.item.Basis = append(candidate.item.Basis, label)
				candidate.score++
			}
		}
		if candidate.score > 0 {
			sort.Strings(candidate.item.Basis)
			ranked = append(ranked, candidate)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if normalized(a.item.Title) != normalized(b.item.Title) {
			return normalized(a.item.Title) < normalized(b.item.Title)
		}
		return a.item.Key < b.item.Key
	})
	for _, candidate := range ranked {
		result = append(result, candidate.item)
		if len(result) == 12 {
			break
		}
	}
	return result, true
}

func relatedCandidates(albums []Album, kind string) []relatedCandidate {
	result := []relatedCandidate{}
	artists := map[string]*relatedCandidate{}
	seen := map[int64]bool{}
	for _, album := range albums {
		aggregate := relatedCandidate{item: RelatedMusic{Kind: kind, Key: album.Key, Title: album.Title, CoverMediaID: album.CoverMediaID}, tags: map[string]string{}}
		for _, song := range album.Songs {
			tags := relatedSongTags(song)
			for tag, label := range tags {
				aggregate.tags[tag] = label
			}
			if kind == "song" && !seen[song.ID] {
				copy := song
				title := song.FileName
				if song.Music != nil && strings.TrimSpace(song.Music.Title) != "" {
					title = song.Music.Title
				}
				result = append(result, relatedCandidate{item: RelatedMusic{Kind: kind, Key: strconv.FormatInt(song.ID, 10), Title: title, Song: &copy}, tags: tags})
				seen[song.ID] = true
			}
			if kind == "artist" {
				for _, credit := range MusicCredits(song) {
					// Synthetic fallback artists cannot establish meaningful similarity.
					if normalized(credit.Name) == "unknown artist" || normalized(credit.Name) == "various artists" {
						continue
					}
					person := artists[credit.Key]
					if person == nil {
						person = &relatedCandidate{item: RelatedMusic{Kind: kind, Key: credit.Key, Title: credit.Name, CoverMediaID: album.CoverMediaID}, tags: map[string]string{}}
						artists[credit.Key] = person
					}
					for tag, label := range tags {
						if strings.HasPrefix(tag, "genre:") {
							person.tags[tag] = label
						}
					}
				}
			}
		}
		if kind == "album" {
			result = append(result, aggregate)
		}
	}
	for _, artist := range artists {
		result = append(result, *artist)
	}
	return result
}
func relatedSongTags(song model.Media) map[string]string {
	tags := map[string]string{}
	if song.Music != nil {
		for _, genre := range song.Music.Genres {
			if value := normalized(genre); value != "" {
				tags["genre:"+value] = "Genre: " + strings.TrimSpace(genre)
			}
		}
	}
	for _, credit := range MusicCredits(song) {
		if normalized(credit.Name) == "unknown artist" || normalized(credit.Name) == "various artists" {
			continue
		}
		tags["credit:"+credit.Key] = "Artist/credit: " + credit.Name
	}
	return tags
}
