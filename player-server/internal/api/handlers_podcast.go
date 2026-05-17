package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/service"
)

// ------------------------------------------------------------------
// Podcast Handlers
// ------------------------------------------------------------------

func (s *Server) handleListPodcasts(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}
	feeds, err := s.podcastSvc.ListFeeds(r.Context(), userIDFromContext(r))
	if err != nil {
		s.logger.Error("list podcasts", "err", err)
		handleError(w, fmt.Errorf("failed to list podcasts: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, feeds)
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
		badRequest(w, "invalid request")
		return
	}
	if req.FeedURL == "" {
		badRequest(w, "feed_url is required")
		return
	}

	feed, err := s.podcastSvc.SubscribeFeed(r.Context(), req.FeedURL, req.SetName, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "access denied")
			return
		}
		if errors.Is(err, service.ErrInvalidFeed) {
			badRequest(w, "invalid feed")
			return
		}
		s.logger.Error("subscribe podcast", "err", err)
		handleError(w, fmt.Errorf("failed to subscribe: %w", err))
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
		badRequest(w, "invalid set id")
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
			forbidden(w, "access denied")
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		s.logger.Error("list episodes", "err", err)
		handleError(w, fmt.Errorf("failed to list episodes: %w", err))
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
		badRequest(w, "invalid episode id")
		return
	}

	media, err := s.podcastSvc.DownloadEpisode(r.Context(), episodeID, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "access denied")
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		s.logger.Error("download episode", "err", err)
		handleError(w, fmt.Errorf("failed to download episode: %w", err))
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
		badRequest(w, "invalid episode id")
		return
	}

	if err := s.podcastSvc.ToggleEpisodeComplete(r.Context(), episodeID, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "access denied")
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		s.logger.Error("toggle complete", "err", err)
		handleError(w, fmt.Errorf("failed to toggle completion: %w", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
