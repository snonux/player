package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

func (s *Server) handleMusicPresence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.media.Presence == nil {
		writeError(w, 503, "Listeners unavailable.")
		return
	}
	session, _ := r.Context().Value(sessionCtxKey).(*model.Session)
	if session == nil {
		writeError(w, 401, "Unauthorized")
		return
	}
	expected, err := strconv.ParseInt(r.URL.Query().Get("expected_user_id"), 10, 64)
	if err != nil || expected != session.UserID {
		writeError(w, 409, "Account changed. Reopen Listeners.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := s.media.Presence.List(r.Context(), session.UserID)
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, 200, result)
	case http.MethodPut, http.MethodDelete:
		var body service.PresenceUpdate
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
			badRequest(w, "Invalid listener request.")
			return
		}
		if r.Method == http.MethodDelete {
			err = s.media.Presence.Remove(*session, body.ClientID, body.Sequence)
		} else {
			err = s.media.Presence.Update(r.Context(), *session, body)
		}
		if err != nil {
			handleError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
