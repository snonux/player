package api

import (
	"codeberg.org/snonux/player/internal/service"
	"net/http"
)

func (s *Server) handleMusicRelated(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	kind := r.PathValue("kind")
	if kind != "song" && kind != "album" && kind != "artist" {
		writeError(w, http.StatusBadRequest, "Unknown music kind.")
		return
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	result, found := service.MusicRelated(albums, kind, r.PathValue("entity_key"))
	if !found {
		writeError(w, http.StatusNotFound, "Music is unavailable or you no longer have permission.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
