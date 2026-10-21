package api

import (
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/service"
)

func (s *Server) handleMusicAlbums(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	key, artist := r.PathValue("album_key"), r.URL.Query().Get("artist")
	mediaID, _ := strconv.ParseInt(r.URL.Query().Get("media_id"), 10, 64)
	result := make([]service.Album, 0, len(albums))
	for _, album := range albums {
		if key != "" && album.Key != key {
			continue
		}
		if artist != "" && album.ArtistKey != artist {
			continue
		}
		if mediaID > 0 {
			found := false
			for _, song := range album.Songs {
				if song.ID == mediaID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if key != "" {
			writeJSON(w, http.StatusOK, album)
			return
		}
		album.Songs = nil
		result = append(result, album)
	}
	if key != "" {
		writeError(w, http.StatusNotFound, "Album is unavailable or you no longer have permission.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
