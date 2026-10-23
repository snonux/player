package api

import (
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/service"
)

type queueAlbumMembership struct {
	Key   string `json:"album_key"`
	Order int    `json:"order"`
}

// handleMusicQueueAlbums exposes only caller-visible canonical album membership.
func (s *Server) handleMusicQueueAlbums(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 200000)
	defer r.Body.Close()
	var incoming struct {
		IDs []int64 `json:"media_ids"`
	}
	if err := readJSON(r, &incoming); err != nil || incoming.IDs == nil || len(incoming.IDs) > 10000 {
		badRequest(w, "invalid queue media IDs")
		return
	}
	requested := map[int64]bool{}
	for _, id := range incoming.IDs {
		if id <= 0 || id > 9007199254740991 {
			badRequest(w, "invalid queue media ID")
			return
		}
		requested[id] = true
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	result := map[string]queueAlbumMembership{}
	for _, album := range albums {
		for order, song := range album.Songs {
			if requested[song.ID] {
				result[strconv.FormatInt(song.ID, 10)] = queueAlbumMembership{album.Key, order}
			}
		}
	}
	writeJSON(w, http.StatusOK, result)
}
