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
		id, err := pathID(r, "id")
		if err != nil || id == 0 {
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
	if !requireService(w, s.media.Browse) {
		return
	}
	s.fileHandler(s.media.Browse.StreamMedia)(w, r)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	res, err := s.media.Browse.DownloadMedia(r.Context(), id, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		handleError(w, err)
		return
	}
	if res == nil {
		notFound(w)
		return
	}
	s.serveFileResult(w, r, res, true)
}

func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	s.fileHandler(s.media.Browse.GetThumbnail)(w, r)
}

func (s *Server) handleRegenThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Write) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	if err := s.media.Write.RegenerateThumbnail(r.Context(), id, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
