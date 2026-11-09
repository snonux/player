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
	var incoming model.PlaybackSync
	if err := readJSONLimit(w, r, &incoming, 2<<20); err != nil {
		if bodyTooLarge(w, err) {
			return
		}
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
