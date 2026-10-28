package api

import (
	"errors"
	"net/http"

	"github.com/snonux/player/internal/service"
)

func (s *Server) handlePodcastRefresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	svc, ok := s.podcastSvc.(service.PodcastRefreshService)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "podcast refresh unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		FeedKey string `json:"feed_key"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	result, err := svc.RefreshSubscribedFeed(r.Context(), userIDFromContext(r), req.FeedKey)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPodcastRefreshInvalid):
			badRequest(w, err.Error())
		case errors.Is(err, service.ErrPodcastRefreshMissing):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrPodcastRefreshBusy):
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, err.Error())
		case errors.Is(err, service.ErrPodcastRefreshFailed):
			writeError(w, http.StatusBadGateway, err.Error())
		default:
			handleError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}
