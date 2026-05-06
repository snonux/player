package api

import (
	"context"
	"errors"
	"net/http"

	"codeberg.org/snonux/player/internal/service"
)

// ------------------------------------------------------------------
// File serving handlers
// ------------------------------------------------------------------

func (s *Server) fileHandler(fn func(context.Context, int64, int64) (*service.FileResult, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		if id == 0 {
			http.Error(w, "invalid media id", http.StatusBadRequest)
			return
		}
		res, err := fn(r.Context(), id, userIDFromContext(r))
		if err != nil {
			if errors.Is(err, service.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, service.ErrForbidden) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if res == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.serveFileResult(w, r, res, false)
	}
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	s.fileHandler(s.browseSvc.StreamMedia)(w, r)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	res, err := s.browseSvc.DownloadMedia(r.Context(), id, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if res == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.serveFileResult(w, r, res, true)
}

func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	s.fileHandler(s.browseSvc.GetThumbnail)(w, r)
}

func (s *Server) handleRegenThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.writeSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.writeSvc.RegenerateThumbnail(r.Context(), id, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
