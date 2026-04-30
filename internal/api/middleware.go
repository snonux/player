package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/paul/kiss-media-player/internal/auth"
	"github.com/paul/kiss-media-player/internal/model"
	"github.com/paul/kiss-media-player/internal/repository"
)

type ctxKey int

const (
	sessionCtxKey ctxKey = iota
	userCtxKey
)

// Middleware holds dependencies for middleware constructors.
type Middleware struct {
	store repository.Store
	sm    *auth.SessionManager
}

// NewMiddleware creates middleware handlers.
func NewMiddleware(store repository.Store, sm *auth.SessionManager) *Middleware {
	return &Middleware{store: store, sm: sm}
}

// RequireSession validates the session cookie and injects the session into request context.
func (mw *Middleware) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, err := mw.sm.ValidateSession(r.Context(), cookie.Value)
		if err != nil || sess == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), sessionCtxKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin ensures the authenticated user is an admin.
func (mw *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := r.Context().Value(sessionCtxKey).(*model.Session)
		if !ok || sess == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := mw.store.GetUserByID(r.Context(), sess.UserID)
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
		count, err := mw.store.CountUsers(r.Context())
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
		"/favicon.svg", "/manifest.json", "/sw.js":
		return true
	}
	if strings.HasPrefix(path, "/css/") || strings.HasPrefix(path, "/js/") || strings.HasPrefix(path, "/images/") {
		return true
	}
	return false
}
