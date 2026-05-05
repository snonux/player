package api

import (
	"errors"
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/service"
)

// ------------------------------------------------------------------
// Podcast Handlers
// ------------------------------------------------------------------

func (s *Server) handleListPodcasts(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	userID := userIDFromContext(r)
	sets, err := s.browseSvc.ListSets(r.Context(), userID)
	if err != nil {
		s.logger.Error("list podcasts", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list podcasts"})
		return
	}

	// Filter to podcast sets only.
	var podcasts []interface{}
	for _, set := range sets {
		if set.IsPodcast {
			podcasts = append(podcasts, set)
		}
	}
	writeJSON(w, http.StatusOK, podcasts)
}

func (s *Server) handleSubscribePodcast(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}

	var req struct {
		FeedURL string `json:"feed_url"`
		SetName string `json:"set_name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.FeedURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "feed_url is required"})
		return
	}

	feed, err := s.podcastSvc.SubscribeFeed(r.Context(), req.FeedURL, req.SetName, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
			return
		}
		s.logger.Error("subscribe podcast", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to subscribe"})
		return
	}

	writeJSON(w, http.StatusOK, feed)
}

func (s *Server) handleListEpisodes(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}

	setID := pathID(r, "id")
	if setID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid set id"})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	limit := 50
	offset := 0
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
		offset = v
	}

	episodes, err := s.podcastSvc.ListEpisodes(r.Context(), setID, userIDFromContext(r), limit, offset)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.logger.Error("list episodes", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list episodes"})
		return
	}

	writeJSON(w, http.StatusOK, episodes)
}

func (s *Server) handleDownloadEpisode(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}

	episodeID := pathID(r, "episode_id")
	if episodeID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid episode id"})
		return
	}

	media, err := s.podcastSvc.DownloadEpisode(r.Context(), episodeID, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.logger.Error("download episode", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to download episode"})
		return
	}

	writeJSON(w, http.StatusOK, media)
}

func (s *Server) handleToggleComplete(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}

	episodeID := pathID(r, "episode_id")
	if episodeID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid episode id"})
		return
	}

	if err := s.podcastSvc.ToggleEpisodeComplete(r.Context(), episodeID, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.logger.Error("toggle complete", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to toggle completion"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
