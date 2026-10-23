package api

import (
	"codeberg.org/snonux/player/internal/service"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
)

func (s *Server) handleMusicRating(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	var body struct {
		Rating *int `json:"rating"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid rating.")
		return
	}
	if body.Rating == nil {
		writeError(w, http.StatusBadRequest, "A rating from 0 to 5 is required.")
		return
	}
	if err := service.SetMusicRating(r.Context(), s.store, s.media.Browse, userIDFromContext(r), r.PathValue("kind"), r.PathValue("entity_key"), *body.Rating); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"rating": *body.Rating})
}

// rateMusic decorates only the already authorized catalog.
func (s *Server) rateMusic(ctx context.Context, user int64, albums []service.Album) (map[string]int, error) {
	artists := map[string]int{}
	if s.store == nil {
		return artists, nil
	}
	albumStars, err := s.store.ListMusicRatings(ctx, user, "album")
	if err != nil {
		return nil, err
	}
	songStars, err := s.store.ListMusicRatings(ctx, user, "song")
	if err != nil {
		return nil, err
	}
	artists, err = s.store.ListMusicRatings(ctx, user, "artist")
	if err != nil {
		return nil, err
	}
	for i := range albums {
		albums[i].Rating = albumStars[albums[i].Key]
		for j := range albums[i].Songs {
			song := &albums[i].Songs[j]
			song.Rating = songStars[strconv.FormatInt(song.ID, 10)]
		}
	}
	return artists, nil
}
func ratedAlbums(items []service.Album, r *http.Request) []service.Album {
	result := make([]service.Album, 0, len(items))
	for _, item := range items {
		if r.URL.Query().Get("rated") != "true" || item.Rating > 0 {
			result = append(result, item)
		}
	}
	if r.URL.Query().Get("sort") == "rating" {
		sort.SliceStable(result, func(i, j int) bool { return result[i].Rating > result[j].Rating })
	}
	return result
}
func ratedArtists(items []service.Artist, r *http.Request) []service.Artist {
	result := make([]service.Artist, 0, len(items))
	for _, item := range items {
		if r.URL.Query().Get("rated") != "true" || item.Rating > 0 {
			result = append(result, item)
		}
	}
	if r.URL.Query().Get("sort") == "rating" {
		sort.SliceStable(result, func(i, j int) bool { return result[i].Rating > result[j].Rating })
	}
	return result
}

func rateSearch(result service.MusicSearchResult, stars map[string]int, r *http.Request) service.MusicSearchResult {
	for i := range result.Artists {
		result.Artists[i].Rating = stars[result.Artists[i].Key]
	}
	result.Albums = ratedAlbums(result.Albums, r)
	result.Artists = ratedArtists(result.Artists, r)
	songs := result.Songs[:0]
	for _, song := range result.Songs {
		if r.URL.Query().Get("rated") != "true" || song.Rating > 0 {
			songs = append(songs, song)
		}
	}
	result.Songs = songs
	if r.URL.Query().Get("sort") == "rating" {
		sort.SliceStable(result.Songs, func(i, j int) bool { return result.Songs[i].Rating > result.Songs[j].Rating })
	}
	return result
}
