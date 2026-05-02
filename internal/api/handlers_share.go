package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/service"
)

// ------------------------------------------------------------------
// Share routes
// ------------------------------------------------------------------

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	expiresAt := time.Now().Add(time.Duration(s.cfg.ShareDefaultExpiryDays) * 24 * time.Hour)
	share, err := s.mediaSvc.CreateShare(r.Context(), userIDFromContext(r), id, expiresAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	shares, err := s.mediaSvc.ListShares(r.Context(), id, userIDFromContext(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	token := r.PathValue("token")
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token required"})
		return
	}
	if err := s.mediaSvc.RevokeShare(r.Context(), token, userIDFromContext(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	token := r.PathValue("token")
	res, err := s.mediaSvc.GetSharedMedia(r.Context(), token)
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

	// Serve HTML page with media metadata injected.
	f, err := s.staticFS.Open("share.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var buf strings.Builder
	if _, err := io.Copy(&buf, f); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	html := buf.String()
	data, _ := json.Marshal(res)
	html = strings.Replace(html, "<!--SHARE_MEDIA-->", string(data), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "share.html", stat.ModTime(), strings.NewReader(html))
}

func (s *Server) handleShareThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	token := r.PathValue("token")
	res, err := s.mediaSvc.GetSharedMedia(r.Context(), token)
	if err != nil || res == nil {
		if err != nil && errors.Is(err, service.ErrShareExpired) {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !res.HasThumb || res.Media == nil || res.Media.ThumbnailPath == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fr := &service.FileResult{
		Path:     res.Media.ThumbnailPath,
		FileName: filepath.Base(res.Media.ThumbnailPath),
	}
	s.serveFileResult(w, r, fr, false)
}

func (s *Server) handleShareStream(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	token := r.PathValue("token")
	res, err := s.mediaSvc.StreamSharedMedia(r.Context(), token)
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
