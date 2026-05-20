package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/service"
)

// ------------------------------------------------------------------
// Share routes
// ------------------------------------------------------------------

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	// Use the injected clock so tests can pin "now" and assert deterministic
	// share-expiry semantics (e.g. assert that expiresAt is exactly
	// ShareDefaultExpiryDays * 24h after the mock clock's T).
	expiresAt := s.clk.Now().Add(time.Duration(s.cfg.ShareDefaultExpiryDays) * 24 * time.Hour)
	share, err := s.shareSvc.CreateShare(r.Context(), userIDFromContext(r), id, expiresAt)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	shares, err := s.shareSvc.ListShares(r.Context(), id, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	token := r.PathValue("token")
	if token == "" {
		badRequest(w, "token required")
		return
	}
	if err := s.shareSvc.RevokeShare(r.Context(), token, userIDFromContext(r)); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	token := r.PathValue("token")
	res, err := s.shareSvc.GetSharedMedia(r.Context(), token)
	if err != nil || res == nil {
		if err != nil && errors.Is(err, service.ErrShareExpired) {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Accept")

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		writeJSON(w, http.StatusOK, res)
		return
	}

	// Render the HTML view via the dedicated renderer. This keeps the
	// handler focused on transport concerns (status codes, headers) and
	// keeps templating in the internal/web package.
	page, err := s.shareRenderer.Render(res)
	if err != nil {
		s.logger.Error("render share page", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, page.Name, page.ModTime, strings.NewReader(page.HTML))
}

func (s *Server) handleShareThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	token := r.PathValue("token")
	fr, err := s.shareSvc.GetSharedThumbnail(r.Context(), token)
	if err != nil {
		if errors.Is(err, service.ErrShareExpired) {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		if errors.Is(err, service.ErrShareNotFound) || errors.Is(err, service.ErrMediaNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if fr == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	s.serveFileResult(w, r, fr, false)
}

func (s *Server) handleShareStream(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	token := r.PathValue("token")
	res, err := s.shareSvc.StreamSharedMedia(r.Context(), token)
	if err != nil {
		if errors.Is(err, service.ErrShareExpired) {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		if errors.Is(err, service.ErrShareNotFound) || errors.Is(err, service.ErrMediaNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
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

func (s *Server) handleShareDownload(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	token := r.PathValue("token")
	fr, err := s.shareSvc.StreamSharedMedia(r.Context(), token)
	if err != nil {
		if errors.Is(err, service.ErrShareExpired) {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		if errors.Is(err, service.ErrShareNotFound) || errors.Is(err, service.ErrMediaNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if fr == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveFileResult(w, r, fr, true)
}

func (s *Server) handleMyShares(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.shareSvc) {
		return
	}
	shares, err := s.shareSvc.ListMyShares(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shares)
}
