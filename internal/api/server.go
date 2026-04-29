package api

import (
	"context"
	"net/http"

	"github.com/paul/kiss-media-player/internal"
	"github.com/paul/kiss-media-player/internal/auth"
	"github.com/paul/kiss-media-player/internal/repository"
)

// Server holds HTTP handlers and dependencies.
type Server struct {
	store  repository.Store
	hasher auth.Hasher
	sm     *auth.SessionManager
	cfg    *internal.Config
	mux    *http.ServeMux
	mw     *Middleware
}

// NewServer creates a Server with routes.
func NewServer(store repository.Store, hasher auth.Hasher, sm *auth.SessionManager, cfg *internal.Config) *Server {
	s := &Server{
		store:  store,
		hasher: hasher,
		sm:     sm,
		cfg:    cfg,
		mux:    http.NewServeMux(),
		mw:     NewMiddleware(store, sm),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/bootstrap", s.handleBootstrap)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)

	// logout requires a valid session
	s.mux.Handle("POST /api/logout", s.mw.RequireSession(http.HandlerFunc(s.handleLogout)))

	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	s.mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.pingStore(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (s *Server) pingStore(ctx context.Context) error {
	type pinger interface {
		Ping(ctx context.Context) error
	}
	if p, ok := s.store.(pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mw.BootstrapRedirect(s.mux).ServeHTTP(w, r)
}
