package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/snonux/player/internal/jukebox"
	"github.com/snonux/player/internal/service"
)

type jukeboxCommand struct {
	SessionID string  `json:"session_id"`
	Revision  int64   `json:"revision"`
	Action    string  `json:"action"`
	MediaID   int64   `json:"media_id"`
	MediaIDs  []int64 `json:"media_ids"`
	Index     int     `json:"index"`
	Position  float64 `json:"position"`
	Volume    int     `json:"volume"`
	OutputID  string  `json:"output_id"`
}

func (s *Server) routesJukebox() {
	s.handleBoth(http.MethodGet, "/api/jukebox", s.requireSession(s.handleJukeboxStatus))
	s.handleBoth(http.MethodPost, "/api/jukebox/control", s.requireSession(s.handleJukeboxControl))
}

func (s *Server) handleJukeboxStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !requireService(w, s.jukeboxSvc) {
		return
	}
	if !s.jukeboxSvc.Enabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Server audio is not enabled on this Player instance."})
		return
	}
	userID, isAdmin := s.jukeboxCaller(r)
	st, err := s.jukeboxSvc.Status(r.Context(), userID, isAdmin)
	if err != nil {
		writeJukeboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleJukeboxControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !requireService(w, s.jukeboxSvc) {
		return
	}
	if !s.jukeboxSvc.Enabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Server audio is not enabled on this Player instance."})
		return
	}
	var command jukeboxCommand
	command.Index = -1
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		badRequest(w, "invalid jukebox command")
		return
	}
	userID, isAdmin := s.jukeboxCaller(r)
	st, err := s.jukeboxSvc.Control(r.Context(), jukebox.Command{
		UserID:    userID,
		IsAdmin:   isAdmin,
		SessionID: command.SessionID,
		Revision:  command.Revision,
		Action:    command.Action,
		MediaID:   command.MediaID,
		MediaIDs:  command.MediaIDs,
		Index:     command.Index,
		Position:  command.Position,
		Volume:    command.Volume,
		OutputID:  command.OutputID,
	})
	if err != nil {
		writeJukeboxError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) jukeboxCaller(r *http.Request) (userID int64, isAdmin bool) {
	userID = userIDFromContext(r)
	if s.store == nil {
		return userID, false
	}
	user, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil || user == nil {
		return userID, false
	}
	return userID, user.IsAdmin
}

func writeJukeboxError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jukebox.ErrDisabled):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Server audio is not enabled on this Player instance."})
	case errors.Is(err, jukebox.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only an administrator can control this server's speakers."})
	case errors.Is(err, jukebox.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Server audio is controlled by another session. Return to this device there, or wait and retry."})
	case errors.Is(err, jukebox.ErrBadSession):
		badRequest(w, "invalid server audio session")
	case errors.Is(err, jukebox.ErrBadPath), errors.Is(err, jukebox.ErrEmptyQueue), errors.Is(err, jukebox.ErrBadIndex):
		badRequest(w, err.Error())
	case errors.Is(err, jukebox.ErrUnavailable):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Server speakers are unavailable. Check that audio output is configured on the host."})
	case errors.Is(err, service.ErrNotFound), errors.Is(err, service.ErrForbidden):
		writeFileError(w, err)
	default:
		if err != nil && err.Error() == "choose an audio track for server speakers" {
			badRequest(w, err.Error())
			return
		}
		writeFileError(w, err)
	}
}
