package api

import (
	"context"
	"net/http"

	"github.com/snonux/player/internal/service"
)

func (s *Server) handleChangeLyrics(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	changer, ok := s.media.Browse.(interface {
		ChangeLyrics(context.Context, int64, int64, string) (*service.Lyrics, error)
	})
	if !ok {
		http.Error(w, "Lyric lookup unavailable", http.StatusServiceUnavailable)
		return
	}
	action := "fetch"
	if r.Method == http.MethodDelete {
		action = "remove"
	}
	result, err := changer.ChangeLyrics(r.Context(), id, userIDFromContext(r), action)
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleLyrics(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	result, err := s.media.Browse.GetLyrics(r.Context(), id, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleShareLyrics(w http.ResponseWriter, r *http.Request) {
	preparePublicShare(w)
	if !requireService(w, s.media.Share) {
		return
	}
	access := shareAccess(r)
	result, err := s.media.Share.GetSharedLyrics(r.Context(), access.Token, access.Credentials...)
	if err != nil {
		s.writeShareError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAudioFormat(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	result, err := s.media.Browse.GetAudioFormat(r.Context(), id, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleShareAudioFormat(w http.ResponseWriter, r *http.Request) {
	preparePublicShare(w)
	if !requireService(w, s.media.Share) {
		return
	}
	access := shareAccess(r)
	result, err := s.media.Share.GetSharedAudioFormat(r.Context(), access.Token, access.Credentials...)
	if err != nil {
		s.writeShareError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
