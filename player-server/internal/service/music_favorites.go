package service

import (
	"github.com/snonux/player/internal/repository"
	"context"
)

// SetMusicFavorite validates current read access before saving an account heart.
func SetMusicFavorite(ctx context.Context, repo repository.MusicFavoriteRepo, source CatalogSource, playlists *PlaylistService, user int64, kind, key string, favorite bool) error {
	switch kind {
	case "album", "artist":
		albums, err := MusicAlbums(ctx, source, user)
		if err != nil {
			return err
		}
		found := false
		if kind == "album" {
			for _, a := range albums {
				if a.Key == key {
					found = true
					break
				}
			}
		} else {
			for _, a := range MusicArtists(albums) {
				if a.Key == key {
					found = true
					break
				}
			}
		}
		if !found {
			return ErrNotFound
		}
	case "playlist":
		if playlists == nil {
			return ErrNotFound
		}
		if _, err := playlists.Get(ctx, user, key); err != nil {
			return err
		}
	default:
		return &apiError{msg: "Choose album, artist or playlist.", status: 400}
	}
	return repo.SetMusicFavorite(ctx, user, kind, key, favorite)
}
