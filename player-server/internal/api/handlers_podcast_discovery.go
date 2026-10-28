package api

import (
	"errors"
	"github.com/snonux/player/internal/service"
	"net/http"
)

func (s *Server) handlePodcastDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var results []service.PodcastDiscoveryResult
	var err error
	if r.URL.Query().Get("popular") == "true" {
		results, err = s.podcastDiscovery.Popular(r.Context(), r.URL.Query().Get("country"))
	} else {
		results, err = s.podcastDiscovery.Search(r.Context(), r.URL.Query().Get("q"))
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, service.ErrDiscoveryQuery) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, service.ErrDiscoveryBusy) {
			status = http.StatusTooManyRequests
			w.Header().Set("Retry-After", "3")
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, results)
}
