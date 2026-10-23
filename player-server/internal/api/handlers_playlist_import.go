package api

import (
	"encoding/json"
	"net/http"

	"codeberg.org/snonux/player/internal/service"
)

func (s *Server) handlePlaylistImport(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Playlists) {
		return
	}
	var body service.PlaylistImportRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, 400, "Invalid import request.")
		return
	}
	result, err := s.media.Playlists.ImportM3U(r.Context(), userIDFromContext(r), body)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePlaylistSource(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Playlists) {
		return
	}
	result, err := s.media.Playlists.SyncPlaylist(r.Context(), userIDFromContext(r), r.PathValue("playlist_id"), r.Method == http.MethodDelete)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
