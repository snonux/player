package api

import (
	"context"
	"net/http"
	"time"

	"github.com/snonux/player/internal/service"
)

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Progress) {
		return
	}
	var req struct {
		ListeningSessionID string  `json:"listening_session_id"`
		MediaID            int64   `json:"media_id"`
		Position           float64 `json:"position_seconds"`
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
	if req.ListeningSessionID != "" {
		if err := s.media.Progress.BatchUpdateProgress(r.Context(), sessionID, userIDFromContext(r), []service.ProgressUpdate{{MediaID: req.MediaID, PositionSeconds: req.Position, ListeningSessionID: req.ListeningSessionID}}); err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		ExpectedUserID int64 `json:"expected_user_id"`
		Updates        []struct {
			ListeningSessionID string    `json:"listening_session_id"`
			MediaID            int64     `json:"media_id"`
			PositionSeconds    float64   `json:"position_seconds"`
			ObservedAt         time.Time `json:"observed_at"`
		} `json:"updates"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}

	if req.ExpectedUserID != 0 && req.ExpectedUserID != userIDFromContext(r) {
		writeError(w, http.StatusConflict, "Playback belongs to another account. Sign in again.")
		return
	}
	updates := make([]service.ProgressUpdate, len(req.Updates))
	for i, update := range req.Updates {
		if update.MediaID == 0 {
			badRequest(w, "media_id required")
			return
		}
		updates[i] = service.ProgressUpdate{
			ListeningSessionID: update.ListeningSessionID,
			MediaID:            update.MediaID,
			PositionSeconds:    update.PositionSeconds,
			ObservedAt:         update.ObservedAt,
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
		MediaID    int64     `json:"media_id"`
		Status     string    `json:"status"`
		ObservedAt time.Time `json:"observed_at"`
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
	if req.Status != "finished" && req.Status != "not_started" {
		badRequest(w, "invalid status")
		return
	}
	if !req.ObservedAt.IsZero() {
		timed, ok := s.media.Progress.(interface {
			SetProgressStatusAt(context.Context, int64, int64, string, time.Time) error
		})
		if !ok {
			handleError(w, service.ErrNotFound)
			return
		}
		err = timed.SetProgressStatusAt(r.Context(), userIDFromContext(r), req.MediaID, req.Status, req.ObservedAt)
	} else {
		switch req.Status {
		case "finished":
			err = s.media.Progress.MarkFinished(r.Context(), userIDFromContext(r), req.MediaID)
		case "not_started":
			err = s.media.Progress.MarkNotStarted(r.Context(), userIDFromContext(r), req.MediaID)
		default:
			badRequest(w, "invalid status")
			return
		}
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
	writeJSON(w, http.StatusOK, jsonArray(media))
}
