package service

import (
	"fmt"
	"math/rand/v2"
	"sort"
)

// DiscoverAlbums orders authorized albums using the account's personal activity.
// Zero activity and unrated albums are omitted from their respective collections.
// The caller must hydrate favorites and ratings before calling this function.
func DiscoverAlbums(albums []Album, collection string) ([]Album, error) {
	if collection == "" {
		return albums, nil
	}
	switch collection {
	case "recently_added", "recently_played", "most_played", "random", "favorites", "top_rated":
	default:
		return nil, fmt.Errorf("Unknown album collection.")
	}
	result := make([]Album, 0, len(albums))
	for _, album := range albums {
		if collection == "recently_played" && album.LastPlayedAt == nil || collection == "most_played" && album.PersonalPlayCount == 0 || collection == "favorites" && !album.IsFavorite || collection == "top_rated" && album.Rating == 0 {
			continue
		}
		result = append(result, album)
	}
	if collection == "random" {
		rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
		return result, nil
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		switch collection {
		case "recently_added":
			if !a.AddedAt.Equal(b.AddedAt) {
				return a.AddedAt.After(b.AddedAt)
			}
		case "recently_played":
			if !a.LastPlayedAt.Equal(*b.LastPlayedAt) {
				return a.LastPlayedAt.After(*b.LastPlayedAt)
			}
		case "most_played":
			if a.PersonalPlayCount != b.PersonalPlayCount {
				return a.PersonalPlayCount > b.PersonalPlayCount
			}
		case "top_rated":
			if a.Rating != b.Rating {
				return a.Rating > b.Rating
			}
		}
		if normalized(a.Title) != normalized(b.Title) {
			return normalized(a.Title) < normalized(b.Title)
		}
		return a.Key < b.Key
	})
	return result, nil
}
