package api

import (
	"context"
	"net/http"
	"strings"

	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

type ctxKey int

const (
	sessionCtxKey ctxKey = iota
	userCtxKey
)

// Middleware holds dependencies for middleware constructors.
type Middleware struct {
	authSvc service.AuthService
	sm      auth.SessionManager
}

// NewMiddleware creates middleware handlers.
func NewMiddleware(authSvc service.AuthService, sm auth.SessionManager) *Middleware {
	return &Middleware{authSvc: authSvc, sm: sm}
}

// RequireSession validates the session cookie and injects the session into request context.
// For HTML page requests (Accept: text/html), redirects to /login.html instead of returning 401.
func (mw *Middleware) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			if wantsHTML(r) {
				http.Redirect(w, r, "/login.html", http.StatusTemporaryRedirect)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, err := mw.sm.ValidateSession(r.Context(), cookie.Value)
		if err != nil || sess == nil {
			if wantsHTML(r) {
				http.Redirect(w, r, "/login.html", http.StatusTemporaryRedirect)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), sessionCtxKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// wantsHTML returns true if the request appears to be from a browser expecting an HTML page.
func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}

// RequireAdmin ensures the authenticated user is an admin.
func (mw *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := r.Context().Value(sessionCtxKey).(*model.Session)
		if !ok || sess == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := mw.authSvc.GetUserByID(r.Context(), sess.UserID)
		if err != nil || user == nil || !user.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BootstrapRedirect redirects all requests to /bootstrap.html when no users exist.
func (mw *Middleware) BootstrapRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isBootstrapPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if mw.authSvc == nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		count, err := mw.authSvc.CountUsers(r.Context())
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if count == 0 {
			http.Redirect(w, r, "/bootstrap.html", http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isBootstrapPublic(path string) bool {
	switch path {
	case "/bootstrap.html", "/api/bootstrap", "/login.html", "/api/login", "/healthz", "/readyz",
		"/favicon.svg", "/favicon.ico", "/logo.svg", "/logo.png", "/manifest.json", "/sw.js":
		return true
	}
	if strings.HasPrefix(path, "/css/") || strings.HasPrefix(path, "/js/") || strings.HasPrefix(path, "/images/") {
		return true
	}
	return false
}
