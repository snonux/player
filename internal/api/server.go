package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

// Server holds HTTP handlers and dependencies.
type Server struct {
	store       repository.Store
	hasher      auth.Hasher
	sm          *auth.SessionManager
	cfg         *internal.Config
	mux         *http.ServeMux
	browseSvc   service.MediaBrowseService
	writeSvc    service.MediaWriteService
	shareSvc    service.MediaShareService
	tagSvc      service.MediaTagService
	favSvc      service.MediaFavoriteService
	noteSvc     service.MediaNoteService
	adminSvc    service.AdminService
	progressSvc service.ProgressService
	authSvc     service.AuthService
	staticFS    http.FileSystem
	remuxer     probe.Remuxer
	logger      *slog.Logger
	mw          *Middleware
}

// NewServer creates a Server with routes.
// If any service argument is nil, its respective routes return 501.
func NewServer(
	store repository.Store,
	hasher auth.Hasher,
	sm *auth.SessionManager,
	cfg *internal.Config,
	browseSvc service.MediaBrowseService,
	writeSvc service.MediaWriteService,
	shareSvc service.MediaShareService,
	tagSvc service.MediaTagService,
	favSvc service.MediaFavoriteService,
	noteSvc service.MediaNoteService,
	adminSvc service.AdminService,
	progressSvc service.ProgressService,
	authSvc service.AuthService,
	staticFS http.FileSystem,
	remuxer probe.Remuxer,
) *Server {
	return NewServerWithLogger(store, hasher, sm, cfg, browseSvc, writeSvc, shareSvc, tagSvc, favSvc, noteSvc, adminSvc, progressSvc, authSvc, staticFS, remuxer, slog.Default())
}

// NewServerWithLogger creates a Server with routes and an injected logger.
func NewServerWithLogger(
	store repository.Store,
	hasher auth.Hasher,
	sm *auth.SessionManager,
	cfg *internal.Config,
	browseSvc service.MediaBrowseService,
	writeSvc service.MediaWriteService,
	shareSvc service.MediaShareService,
	tagSvc service.MediaTagService,
	favSvc service.MediaFavoriteService,
	noteSvc service.MediaNoteService,
	adminSvc service.AdminService,
	progressSvc service.ProgressService,
	authSvc service.AuthService,
	staticFS http.FileSystem,
	remuxer probe.Remuxer,
	logger *slog.Logger,
) *Server {
	if staticFS == nil {
		staticFS = http.Dir("web")
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		store:       store,
		hasher:      hasher,
		sm:          sm,
		cfg:         cfg,
		mux:         http.NewServeMux(),
		browseSvc:   browseSvc,
		writeSvc:    writeSvc,
		shareSvc:    shareSvc,
		tagSvc:      tagSvc,
		favSvc:      favSvc,
		noteSvc:     noteSvc,
		adminSvc:    adminSvc,
		progressSvc: progressSvc,
		authSvc:     authSvc,
		staticFS:    staticFS,
		remuxer:     remuxer,
		logger:      logger,
		mw:          NewMiddleware(authSvc, sm),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Public routes — use plain path so wrong method returns 405 instead of falling through to /
	s.mux.HandleFunc("/api/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleBootstrap(w, r)
	})
	s.mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleLogin(w, r)
	})
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleHealthz(w, r)
	})
	s.mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleReadyz(w, r)
	})

	// Public share routes
	s.mux.HandleFunc("GET /s/{token}", s.handleSharePage)
	s.mux.HandleFunc("GET /s/{token}/stream", s.handleShareStream)
	s.mux.HandleFunc("GET /s/{token}/thumbnail", s.handleShareThumbnail)
	s.mux.HandleFunc("GET /s/{token}/download", s.handleShareDownload)

	// Static assets (public)
	staticHandler := http.FileServer(s.staticFS)
	s.mux.Handle("/css/", staticHandler)
	s.mux.Handle("/js/", staticHandler)

	// HTML pages
	s.mux.Handle("/login.html", http.HandlerFunc(s.serveLogin))
	s.mux.Handle("/bootstrap.html", http.HandlerFunc(s.serveBootstrap))
	s.mux.Handle("/", s.mw.RequireSession(http.HandlerFunc(s.serveIndex)))
	s.mux.Handle("GET /index.html", s.mw.RequireSession(http.HandlerFunc(s.serveIndex)))
	s.mux.Handle("GET /detach.html", s.mw.RequireSession(http.HandlerFunc(s.serveDetach)))

	// Session-required routes
	s.mux.Handle("POST /api/logout", s.mw.RequireSession(http.HandlerFunc(s.handleLogout)))

	// Sets
	s.mux.Handle("GET /api/sets", s.mw.RequireSession(http.HandlerFunc(s.handleListSets)))
	s.mux.Handle("GET /api/sets/{id}/browse", s.mw.RequireSession(http.HandlerFunc(s.handleBrowseSet)))
	s.mux.Handle("GET /api/sets/{id}/cover", s.mw.RequireSession(http.HandlerFunc(s.handleGetSetCover)))
	s.mux.Handle("POST /api/sets/{id}/cover", s.mw.RequireSession(http.HandlerFunc(s.handlePostSetCover)))
	s.mux.Handle("POST /api/sets/{id}/upload", s.mw.RequireSession(http.HandlerFunc(s.handleUpload)))

	// Media
	s.mux.Handle("GET /api/media", s.mw.RequireSession(http.HandlerFunc(s.handleListMedia)))
	s.mux.Handle("GET /api/media/{id}", s.mw.RequireSession(http.HandlerFunc(s.handleGetMedia)))
	s.mux.Handle("GET /api/media/{id}/stream", s.mw.RequireSession(http.HandlerFunc(s.handleStream)))
	s.mux.Handle("GET /api/media/{id}/download", s.mw.RequireSession(http.HandlerFunc(s.handleDownload)))
	s.mux.Handle("GET /api/media/{id}/thumbnail", s.mw.RequireSession(http.HandlerFunc(s.handleThumbnail)))
	s.mux.Handle("POST /api/media/{id}/thumbnail", s.mw.RequireSession(http.HandlerFunc(s.handleRegenThumbnail)))
	s.mux.Handle("POST /api/media/{id}/favorite", s.mw.RequireSession(http.HandlerFunc(s.handleFavorite)))
	s.mux.Handle("POST /api/media/{id}/tags", s.mw.RequireSession(http.HandlerFunc(s.handleAddTag)))
	s.mux.Handle("DELETE /api/media/{id}/tags/{tag}", s.mw.RequireSession(http.HandlerFunc(s.handleRemoveTag)))
	s.mux.Handle("DELETE /api/media/{id}", s.mw.RequireSession(http.HandlerFunc(s.handleSoftDelete)))
	s.mux.Handle("POST /api/media/{id}/restore", s.mw.RequireSession(http.HandlerFunc(s.handleRestore)))
	s.mux.Handle("POST /api/media/{id}/shares", s.mw.RequireSession(http.HandlerFunc(s.handleCreateShare)))
	s.mux.Handle("GET /api/media/{id}/shares", s.mw.RequireSession(http.HandlerFunc(s.handleListShares)))

	// Notes
	s.mux.Handle("GET /api/media/{id}/notes", s.mw.RequireSession(http.HandlerFunc(s.handleGetNote)))
	s.mux.Handle("POST /api/media/{id}/notes", s.mw.RequireSession(http.HandlerFunc(s.handleUpsertNote)))
	s.mux.Handle("DELETE /api/media/{id}/notes", s.mw.RequireSession(http.HandlerFunc(s.handleDeleteNote)))

	// Progress
	s.mux.Handle("POST /api/progress", s.mw.RequireSession(http.HandlerFunc(s.handleProgress)))

	// Shares
	s.mux.Handle("DELETE /api/shares/{token}", s.mw.RequireSession(http.HandlerFunc(s.handleRevokeShare)))
	s.mux.Handle("GET /api/shares", s.mw.RequireSession(http.HandlerFunc(s.handleMyShares)))

	// Admin routes
	s.mux.Handle("GET /api/admin/trash", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleListTrash))))
	s.mux.Handle("POST /api/admin/rescan", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleRescan))))
	s.mux.Handle("GET /api/admin/scan-progress", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleScanProgress))))
	s.mux.Handle("GET /api/admin/users", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleListUsers))))
	s.mux.Handle("POST /api/admin/users", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleCreateUser))))
	s.mux.Handle("DELETE /api/admin/users/{id}", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleDeleteUser))))
	s.mux.Handle("GET /api/admin/permissions", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleListPermissions))))
	s.mux.Handle("POST /api/admin/permissions", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleGrantPermission))))
	s.mux.Handle("DELETE /api/admin/permissions", s.mw.RequireSession(s.mw.RequireAdmin(http.HandlerFunc(s.handleRevokePermission))))
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

// GracefulServer wraps an http.Server with graceful shutdown support.
type GracefulServer struct {
	Server *http.Server
}

// NewGracefulServer creates a GracefulServer with the given handler and config.
func NewGracefulServer(handler http.Handler, cfg *internal.Config) *GracefulServer {
	return &GracefulServer{
		Server: &http.Server{
			Addr:         addrFromPort(cfg.Port),
			Handler:      handler,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
}

func addrFromPort(port int) string {
	return ":" + strconv.Itoa(port)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mw.BootstrapRedirect(s.mux).ServeHTTP(w, r)
}
