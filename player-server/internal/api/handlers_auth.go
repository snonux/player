package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/model"
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

type createAPITokenRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expires_in_days"`
}

type createAPITokenResponse struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

type apiTokenResponse struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
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

// handleCountUsers returns the total number of registered users as a public
// JSON endpoint.  Mobile clients use this to decide whether to redirect to
// the bootstrap screen (count == 0) or the login screen (count > 0) on
// first launch, without requiring a session or credentials.
//
// GET /api/v1/auth/count  →  200 {"count": N}
func (s *Server) handleCountUsers(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.authSvc) {
		return
	}
	count, err := s.authSvc.CountUsers(r.Context())
	if err != nil {
		handleError(w, fmt.Errorf("count users: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil && cookie.Value != "" {
		_ = s.sm.DeleteSession(r.Context(), cookie.Value)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.authSvc) {
		return
	}

	var req createAPITokenRequest
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		badRequest(w, "name required")
		return
	}

	expiresAt, ok := s.apiTokenExpiresAt(req.ExpiresInDays)
	if !ok {
		badRequest(w, "expires_in_days must be greater than zero")
		return
	}

	result, err := s.authSvc.CreateAPIToken(r.Context(), userIDFromContext(r), name, expiresAt)
	if err != nil {
		handleError(w, err)
		return
	}
	if result == nil || result.Token == nil {
		handleError(w, fmt.Errorf("create api token returned nil result"))
		return
	}

	writeJSON(w, http.StatusOK, createAPITokenResponse{
		ID:    result.Token.ID,
		Name:  result.Token.Name,
		Token: result.Plaintext,
	})
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.authSvc) {
		return
	}

	tokens, err := s.authSvc.ListAPITokens(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiTokenResponses(tokens))
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.authSvc) {
		return
	}

	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid token id")
		return
	}
	if err := s.authSvc.RevokeAPIToken(r.Context(), userIDFromContext(r), id); err != nil {
		handleError(w, err)
		return
	}
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
	sessionDuration := time.Duration(s.cfg.SessionTimeoutHours) * time.Hour
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		// SameSite=Lax allows cross-context navigation (e.g. mobile webviews,
		// embedded players following a link) while still blocking most CSRF
		// vectors. SameSite=Strict would drop the cookie on any cross-site
		// top-level navigation, causing unnecessary session loss.
		SameSite: http.SameSiteLaxMode,
		// MaxAge is the authoritative persistence signal in modern browsers;
		// Expires is the legacy fallback. Both are set to the same session
		// duration so the cookie persists across browser restarts regardless
		// of which attribute the client honours.
		MaxAge: int(sessionDuration.Seconds()),
		// Use the injected clock so tests can assert the cookie Expires
		// value deterministically (no flakiness from time.Now()).
		Expires: s.clk.Now().Add(sessionDuration),
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

// apiTokenExpiresAt is a method on Server so it can use the injected clock
// (s.clk) instead of the wall clock. Returns nil expiry when expiresInDays is
// nil (i.e. token never expires); false when the value is non-positive (so
// the caller can return 400).
func (s *Server) apiTokenExpiresAt(expiresInDays *int) (*time.Time, bool) {
	if expiresInDays == nil {
		return nil, true
	}
	if *expiresInDays <= 0 {
		return nil, false
	}
	expiresAt := s.clk.Now().Add(time.Duration(*expiresInDays) * 24 * time.Hour)
	return &expiresAt, true
}

func apiTokenResponses(tokens []model.APIToken) []apiTokenResponse {
	responses := make([]apiTokenResponse, 0, len(tokens))
	for _, token := range tokens {
		responses = append(responses, apiTokenResponse{
			ID:         token.ID,
			Name:       token.Name,
			LastUsedAt: token.LastUsedAt,
			ExpiresAt:  token.ExpiresAt,
			CreatedAt:  token.CreatedAt,
		})
	}
	return responses
}
