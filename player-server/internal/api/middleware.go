package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

type ctxKey int

var errUnauthorized = errors.New("unauthorized")

const (
	sessionCtxKey ctxKey = iota
	userCtxKey
)

// Middleware holds dependencies for middleware constructors.
//
// publicPaths and publicPrefixes form a route registry used by
// BootstrapRedirect to decide which requests bypass the redirect when no
// users exist. They are populated at route registration time (see
// Server.routes()) so that adding a new public route automatically updates
// the bypass set — no hidden hardcoded whitelist that silently 401s/redirects
// new routes the developer forgot to add.
type Middleware struct {
	authSvc        service.AuthService
	sm             auth.SessionManager
	publicPaths    map[string]bool
	publicPrefixes []string
}

// NewMiddleware creates middleware handlers.
// The public route registry starts empty; callers register public paths
// via RegisterPublic / RegisterPublicPrefix as routes are wired up.
func NewMiddleware(authSvc service.AuthService, sm auth.SessionManager) *Middleware {
	return &Middleware{
		authSvc:     authSvc,
		sm:          sm,
		publicPaths: make(map[string]bool),
	}
}

// RegisterPublic marks an exact path as public (bypasses BootstrapRedirect).
// Call this at route registration time so the middleware's view of "public"
// stays in sync with the actual route table.
func (mw *Middleware) RegisterPublic(path string) {
	if mw.publicPaths == nil {
		mw.publicPaths = make(map[string]bool)
	}
	mw.publicPaths[path] = true
}

// RegisterPublicPrefix marks a path prefix as public. Used for routes whose
// concrete paths contain wildcards (e.g. /s/{token}/...) or that serve a
// directory tree (e.g. /css/, /js/, /images/).
func (mw *Middleware) RegisterPublicPrefix(prefix string) {
	mw.publicPrefixes = append(mw.publicPrefixes, prefix)
}

// isPublic reports whether the given request path is registered as public.
func (mw *Middleware) isPublic(path string) bool {
	if mw.publicPaths[path] {
		return true
	}
	for _, p := range mw.publicPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// RequireSession validates the session cookie and injects the session into request context.
// For HTML page requests (Accept: text/html), redirects to /login.html instead of returning 401.
func (mw *Middleware) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := mw.authenticate(r)
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

func (mw *Middleware) authenticate(r *http.Request) (*model.Session, error) {
	if token, ok := bearerToken(r); ok {
		if mw.authSvc == nil {
			return nil, errUnauthorized
		}
		return mw.authSvc.AuthenticateBearer(r.Context(), token)
	}
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, errUnauthorized
	}
	if mw.sm == nil {
		return nil, errUnauthorized
	}
	return mw.sm.ValidateSession(r.Context(), cookie.Value)
}

func bearerToken(r *http.Request) (string, bool) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) == 0 || !strings.EqualFold(fields[0], "Bearer") {
		return "", false
	}
	if len(fields) != 2 {
		return "", true
	}
	return fields[1], true
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
//
// Public routes (the bootstrap page itself, login endpoints, health probes,
// static assets, public share routes, etc.) bypass the redirect so the user
// can complete the bootstrap flow. The set of public paths is consulted via
// the route registry on Middleware (populated at route declaration time)
// rather than a hardcoded list, so adding a new public route in server.go is
// all that's required — there is no separate whitelist to keep in sync.
func (mw *Middleware) BootstrapRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mw.isPublic(r.URL.Path) {
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
