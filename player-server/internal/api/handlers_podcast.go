package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
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

	writeJSON(w, http.StatusOK, jsonArray(feeds))
}

func (s *Server) handleSubscribePodcast(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}

	var req struct {
		FeedURL string `json:"feed_url"`
		SetName string `json:"set_name"`
	}
	if err := readJSON(w, r, &req); err != nil {
		if bodyTooLarge(w, err) {
			return
		}
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

	setID, err := pathID(r, "id")
	if err != nil || setID == 0 {
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

	writeJSON(w, http.StatusOK, jsonArray(episodes))
}

// handleListSubscribedEpisodes serves GET /api/podcasts/episodes and the
// /api/v1 twin (contract §6.3): one page of the caller's subscribed feeds,
// optionally limited to feed_key, filtered by completion. The static
// "episodes" path is registered beside /podcasts/{id}/episodes so a set id
// never shadows it.
func (s *Server) handleListSubscribedEpisodes(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}

	q := repository.PodcastEpisodeQuery{
		FeedKey: r.URL.Query().Get("feed_key"),
		Filter:  r.URL.Query().Get("filter"),
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		q.Limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil {
		q.Offset = v
	}

	views, err := s.podcastSvc.ListSubscribedEpisodes(r.Context(), userIDFromContext(r), q)
	if err != nil {
		if errors.Is(err, service.ErrInvalidEpisodeView) {
			badRequest(w, "invalid episode view")
			return
		}
		s.logger.Error("list subscribed episodes", "err", err)
		handleError(w, fmt.Errorf("failed to list subscribed episodes: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, jsonArray(views))
}

func (s *Server) handleDownloadEpisode(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSvc) {
		return
	}

	episodeID, err := pathID(r, "episode_id")
	if err != nil || episodeID == 0 {
		badRequest(w, "invalid episode id")
		return
	}

	// Saving may outlast the server's ordinary response deadline. The request
	// context still cancels the publisher transfer when the listener aborts it.
	response := http.NewResponseController(w)
	_ = response.SetWriteDeadline(time.Time{})
	media, err := s.podcastSvc.DownloadEpisode(r.Context(), episodeID, userIDFromContext(r))
	_ = response.SetWriteDeadline(time.Now().Add(30 * time.Second))
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
	s.podcastAction(w, r, "episode_id", "episode", "toggle complete", "failed to toggle completion",
		func(ctx context.Context, id, userID int64) error {
			return s.podcastSvc.ToggleEpisodeComplete(ctx, id, userID)
		})
}

// podcastAction runs one podcast mutation on the id in path variable param and
// answers 204. Access errors map to 403/404; anything else is logged as
// logMsg and answered wrapped in failMsg.
func (s *Server) podcastAction(w http.ResponseWriter, r *http.Request, param, label, logMsg, failMsg string, action func(ctx context.Context, id, userID int64) error) {
	if !requireService(w, s.podcastSvc) {
		return
	}
	id, err := pathID(r, param)
	if err != nil || id == 0 {
		badRequest(w, "invalid "+label+" id")
		return
	}
	if err := action(r.Context(), id, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "access denied")
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		s.logger.Error(logMsg, "err", err)
		handleError(w, fmt.Errorf("%s: %w", failMsg, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnsubscribePodcast removes a feed together with its downloaded
// episode media and folder. Admin only, like subscribing.
func (s *Server) handleUnsubscribePodcast(w http.ResponseWriter, r *http.Request) {
	s.podcastAction(w, r, "id", "feed", "unsubscribe podcast", "failed to unsubscribe",
		func(ctx context.Context, id, userID int64) error {
			return s.podcastSvc.UnsubscribeFeed(ctx, id, userID)
		})
}

// handleStreamPodcastEpisode proxies publisher bytes independently of a saved
// download, so cancelling that POST never interrupts the listener's stream.
func (s *Server) handleStreamPodcastEpisode(w http.ResponseWriter, r *http.Request) {
	streamer, ok := s.podcastSvc.(service.PodcastEpisodeStreamer)
	if !ok {
		http.Error(w, "Podcast streaming unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := pathID(r, "episode_id")
	if err != nil || id == 0 {
		badRequest(w, "invalid episode id")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	headers := time.AfterFunc(radioIdleTimeout, cancel)
	response, err := streamer.OpenEpisodeStream(ctx, id, userIDFromContext(r), r.Method, r.Header.Get("Range"), r.Header.Get("If-Range"))
	headers.Stop()
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "access denied")
			return
		}
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		http.Error(w, "Publisher audio unavailable. Retry playback.", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	if w.Header().Get("Content-Type") == "" || response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		w.Header().Set("Content-Length", "0")
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(response.StatusCode)
	if r.Method == http.MethodHead || response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return
	}
	relayStream(ctx, cancel, w, response.Body)
}
