package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"codeberg.org/snonux/player/internal/service"
)

type bootstrapRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ------------------------------------------------------------------
// Bootstrap & Auth
// ------------------------------------------------------------------

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.authSvc) {
		return
	}
	var req bootstrapRequest
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		badRequest(w, "username and password required")
		return
	}

	res, err := s.authSvc.Bootstrap(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrAlreadyBootstrapped) {
			forbidden(w, "bootstrap already complete")
			return
		}
		handleError(w, fmt.Errorf("internal server error: %w", err))
		return
	}

	s.setSessionCookie(w, res.SessionID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": res.User.ID, "username": res.User.Username, "is_admin": res.User.IsAdmin})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.authSvc) {
		return
	}
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		badRequest(w, "username and password required")
		return
	}

	res, err := s.authSvc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		handleError(w, fmt.Errorf("internal server error: %w", err))
		return
	}

	s.setSessionCookie(w, res.SessionID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": res.User.ID, "username": res.User.Username, "is_admin": res.User.IsAdmin})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil && cookie.Value != "" {
		_ = s.sm.DeleteSession(r.Context(), cookie.Value)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.pingStore(r.Context()); err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(time.Duration(s.cfg.SessionTimeoutHours) * time.Hour),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
