package api

import (
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleGetSavedQueue(w http.ResponseWriter, r *http.Request) {
	q, err := s.store.GetSavedQueue(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeSavedQueue(w, http.StatusOK, q)
}

func (s *Server) handlePutSavedQueue(w http.ResponseWriter, r *http.Request) {
	// No wildcard or omitted precondition: clients must observe a revision first.
	match := r.Header.Get("If-Match")
	revision, err := strconv.ParseInt(strings.Trim(match, "\""), 10, 64)
	if err != nil || revision < 0 || match != `"`+strconv.FormatInt(revision, 10)+`"` {
		badRequest(w, "If-Match must contain the quoted queue revision")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	defer r.Body.Close()
	var incoming model.SavedQueue
	if err := readJSON(r, &incoming); err != nil {
		badRequest(w, "invalid queue body")
		return
	}
	q, saved, err := service.PutSavedQueue(r.Context(), s.store, userIDFromContext(r), revision, incoming)
	if errors.Is(err, service.ErrInvalidQueue) {
		badRequest(w, "invalid saved queue")
		return
	}
	if err != nil {
		handleError(w, err)
		return
	}
	status := http.StatusOK
	if !saved {
		status = http.StatusConflict
	}
	writeSavedQueue(w, status, q)
}

func writeSavedQueue(w http.ResponseWriter, status int, q *model.SavedQueue) {
	w.Header().Set("ETag", `"`+strconv.FormatInt(q.Revision, 10)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, q)
}
