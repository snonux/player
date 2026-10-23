package service

import (
	"github.com/snonux/player/internal/repository"
	"context"
	"strconv"
)

// SetMusicRating verifies current catalog access before saving personal stars.
func SetMusicRating(ctx context.Context, repo repository.MusicRatingRepo, source CatalogSource, user int64, kind, key string, rating int) error {
	if rating < 0 || rating > 5 {
		return &apiError{msg: "Choose a rating from 0 to 5.", status: 400}
	}
	if kind != "song" && kind != "album" && kind != "artist" {
		return &apiError{msg: "Choose song, album or artist.", status: 400}
	}
	albums, err := MusicAlbums(ctx, source, user)
	if err != nil {
		return err
	}
	found := false
	for _, album := range albums {
		if kind == "album" && album.Key == key {
			found = true
		}
		if kind == "song" {
			for _, song := range album.Songs {
				if strconv.FormatInt(song.ID, 10) == key {
					found = true
				}
			}
		}
	}
	if kind == "artist" {
		for _, artist := range MusicArtists(albums) {
			if artist.Key == key {
				found = true
			}
		}
	}
	if !found {
		return ErrNotFound
	}
	return repo.SetMusicRating(ctx, user, kind, key, rating)
}
