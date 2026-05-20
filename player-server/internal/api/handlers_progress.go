package api

import (
	"net/http"
	"time"

	"codeberg.org/snonux/player/internal/service"
)

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Progress) {
		return
	}
	var req struct {
		MediaID  int64   `json:"media_id"`
		Position float64 `json:"position_seconds"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	if req.MediaID == 0 {
		badRequest(w, "media_id required")
		return
	}
	sessionID := sessionIDFromContext(r)
	if sessionID == "" {
		badRequest(w, "session required")
		return
	}
	err := s.media.Progress.UpdateProgress(
		r.Context(),
		sessionID,
		userIDFromContext(r),
		req.MediaID,
		req.Position,
	)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBatchProgress(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Progress) {
		return
	}
	var req struct {
		Updates []struct {
			MediaID         int64     `json:"media_id"`
			PositionSeconds float64   `json:"position_seconds"`
			ObservedAt      time.Time `json:"observed_at"`
		} `json:"updates"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}

	updates := make([]service.ProgressUpdate, len(req.Updates))
	for i, update := range req.Updates {
		if update.MediaID == 0 {
			badRequest(w, "media_id required")
			return
		}
		updates[i] = service.ProgressUpdate{
			MediaID:         update.MediaID,
			PositionSeconds: update.PositionSeconds,
			ObservedAt:      update.ObservedAt,
		}
	}

	sessionID := sessionIDFromContext(r)
	if sessionID == "" {
		badRequest(w, "session required")
		return
	}
	if err := s.media.Progress.BatchUpdateProgress(r.Context(), sessionID, userIDFromContext(r), updates); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleProgressStatus(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Progress) {
		return
	}
	var req struct {
		MediaID int64  `json:"media_id"`
		Status  string `json:"status"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	if req.MediaID == 0 {
		badRequest(w, "media_id required")
		return
	}

	var err error
	switch req.Status {
	case "finished":
		err = s.media.Progress.MarkFinished(r.Context(), userIDFromContext(r), req.MediaID)
	case "not_started":
		err = s.media.Progress.MarkNotStarted(r.Context(), userIDFromContext(r), req.MediaID)
	default:
		badRequest(w, "invalid status")
		return
	}
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleInProgress(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Progress) {
		return
	}
	media, err := s.media.Progress.ListInProgress(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, media)
}
