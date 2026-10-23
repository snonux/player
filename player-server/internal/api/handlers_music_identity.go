package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

func (s *Server) handleAdminMusicIdentityConflicts(w http.ResponseWriter, r *http.Request) {
	if s.musicIdentity == nil {
		writeError(w, http.StatusNotImplemented, "Music identity recovery is unavailable.")
		return
	}
	list, err := s.musicIdentity.ListConflicts(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	if list == nil {
		list = []model.MusicIdentityConflict{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAdminMusicIdentityResolve(w http.ResponseWriter, r *http.Request) {
	if s.musicIdentity == nil {
		writeError(w, http.StatusNotImplemented, "Music identity recovery is unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		badRequest(w, "invalid conflict id")
		return
	}
	var body struct {
		Action           string `json:"action"`
		CandidateMediaID int64  `json:"candidate_media_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	err = s.musicIdentity.Resolve(r.Context(), id, body.Action, body.CandidateMediaID)
	if errors.Is(err, service.ErrIdentityConflictNotFound) {
		writeError(w, http.StatusNotFound, "Conflict not found.")
		return
	}
	if errors.Is(err, service.ErrIdentityConflictBadAction) {
		badRequest(w, err.Error())
		return
	}
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
