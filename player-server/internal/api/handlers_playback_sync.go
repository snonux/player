package api

import (
	"net/http"
	"strconv"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

func (s *Server) handlePlaybackSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "invalid media id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	defer r.Body.Close()
	var incoming model.PlaybackSync
	if err := readJSON(r, &incoming); err != nil {
		badRequest(w, "invalid body")
		return
	}
	store, ok := s.store.(service.PlaybackSyncStore)
	if !ok {
		http.Error(w, "not implemented", http.StatusNotImplemented)
		return
	}
	result, err := service.SyncPlayback(r.Context(), store, userIDFromContext(r), id, incoming)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
