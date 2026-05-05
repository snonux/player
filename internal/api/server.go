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
	podcastSvc  service.PodcastEpisodeService
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
	podcastSvc service.PodcastEpisodeService,
	staticFS http.FileSystem,
	remuxer probe.Remuxer,
) *Server {
	return NewServerWithLogger(store, hasher, sm, cfg, browseSvc, writeSvc, shareSvc, tagSvc, favSvc, noteSvc, adminSvc, progressSvc, authSvc, podcastSvc, staticFS, remuxer, slog.Default())
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
	podcastSvc service.PodcastEpisodeService,
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
		podcastSvc:  podcastSvc,
		staticFS:    staticFS,
		remuxer:     remuxer,
		logger:      logger,
		mw:          NewMiddleware(authSvc, sm),
	}
	s.routes()
	return s
}

// requireSession wraps a handler with the session requirement middleware.
func (s *Server) requireSession(h http.HandlerFunc) http.HandlerFunc {
	return s.mw.RequireSession(h).(http.HandlerFunc)
}

// requireAdmin wraps a handler with both session and admin middleware.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.mw.RequireSession(s.mw.RequireAdmin(h)).(http.HandlerFunc)
}

// publicMethod wraps a handler so only the given HTTP method is allowed;
// other methods yield 405 instead of falling through.
func publicMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

// routesPublic wires the fully-public API endpoints (bootstrap, login, probes).
func (s *Server) routesPublic() {
	s.mux.HandleFunc("/api/bootstrap", publicMethod(http.MethodPost, s.handleBootstrap))
	s.mux.HandleFunc("/api/login", publicMethod(http.MethodPost, s.handleLogin))
	s.mux.HandleFunc("/healthz", publicMethod(http.MethodGet, s.handleHealthz))
	s.mux.HandleFunc("/readyz", publicMethod(http.MethodGet, s.handleReadyz))
}

// routesSharePublic wires public share routes (no session required).
func (s *Server) routesSharePublic() {
	s.mux.HandleFunc("GET /s/{token}", s.handleSharePage)
	s.mux.HandleFunc("GET /s/{token}/stream", s.handleShareStream)
	s.mux.HandleFunc("GET /s/{token}/thumbnail", s.handleShareThumbnail)
	s.mux.HandleFunc("GET /s/{token}/download", s.handleShareDownload)
}

// routesStatic wires static CSS/JS asset serving.
func (s *Server) routesStatic() {
	staticHandler := http.FileServer(s.staticFS)
	s.mux.Handle("/css/", staticHandler)
	s.mux.Handle("/js/", staticHandler)
	s.mux.Handle("/logo.png", staticHandler)
	s.mux.Handle("/logo.svg", staticHandler)
	s.mux.Handle("/favicon.ico", staticHandler)
	s.mux.Handle("/favicon.svg", staticHandler)
}

// routesHTML wires the SPA HTML page routes.
func (s *Server) routesHTML() {
	s.mux.Handle("/login.html", http.HandlerFunc(s.serveLogin))
	s.mux.Handle("/bootstrap.html", http.HandlerFunc(s.serveBootstrap))
	s.mux.Handle("/", s.mw.RequireSession(http.HandlerFunc(s.serveIndex)))
	s.mux.Handle("GET /index.html", s.mw.RequireSession(http.HandlerFunc(s.serveIndex)))
	s.mux.Handle("GET /detach.html", s.mw.RequireSession(http.HandlerFunc(s.serveDetach)))
}

// routesAuth wires the logout route.
func (s *Server) routesAuth() {
	s.mux.Handle("POST /api/logout", s.requireSession(s.handleLogout))
}

// routesSets wires the set-related API routes.
func (s *Server) routesSets() {
	s.mux.Handle("GET /api/sets", s.requireSession(s.handleListSets))
	s.mux.Handle("GET /api/sets/{id}/browse", s.requireSession(s.handleBrowseSet))
	s.mux.Handle("GET /api/sets/{id}/cover", s.requireSession(s.handleGetSetCover))
	s.mux.Handle("POST /api/sets/{id}/cover", s.requireSession(s.handlePostSetCover))
	s.mux.Handle("POST /api/sets/{id}/upload", s.requireSession(s.handleUpload))
}

// routesMedia wires the media-related API routes.
func (s *Server) routesMedia() {
	s.mux.Handle("GET /api/media", s.requireSession(s.handleListMedia))
	s.mux.Handle("GET /api/media/{id}", s.requireSession(s.handleGetMedia))
	s.mux.Handle("GET /api/media/{id}/stream", s.requireSession(s.handleStream))
	s.mux.Handle("GET /api/media/{id}/download", s.requireSession(s.handleDownload))
	s.mux.Handle("GET /api/media/{id}/thumbnail", s.requireSession(s.handleThumbnail))
	s.mux.Handle("POST /api/media/{id}/thumbnail", s.requireSession(s.handleRegenThumbnail))
	s.mux.Handle("POST /api/media/{id}/favorite", s.requireSession(s.handleFavorite))
	s.mux.Handle("POST /api/media/{id}/tags", s.requireSession(s.handleAddTag))
	s.mux.Handle("DELETE /api/media/{id}/tags/{tag}", s.requireSession(s.handleRemoveTag))
	s.mux.Handle("DELETE /api/media/{id}", s.requireSession(s.handleSoftDelete))
	s.mux.Handle("POST /api/media/{id}/restore", s.requireSession(s.handleRestore))
	s.mux.Handle("POST /api/media/{id}/shares", s.requireSession(s.handleCreateShare))
	s.mux.Handle("GET /api/media/{id}/shares", s.requireSession(s.handleListShares))
}

// routesNotes wires the notes API routes.
func (s *Server) routesNotes() {
	s.mux.Handle("GET /api/media/{id}/notes", s.requireSession(s.handleGetNote))
	s.mux.Handle("POST /api/media/{id}/notes", s.requireSession(s.handleUpsertNote))
	s.mux.Handle("DELETE /api/media/{id}/notes", s.requireSession(s.handleDeleteNote))
}

// routesProgress wires the progress API routes.
func (s *Server) routesProgress() {
	s.mux.Handle("POST /api/progress", s.requireSession(s.handleProgress))
}

// routesShares wires the share-management API routes.
func (s *Server) routesShares() {
	s.mux.Handle("DELETE /api/shares/{token}", s.requireSession(s.handleRevokeShare))
	s.mux.Handle("GET /api/shares", s.requireSession(s.handleMyShares))
}

// routesAdmin wires the admin-only API routes.
func (s *Server) routesAdmin() {
	s.mux.Handle("GET /api/admin/trash", s.requireAdmin(s.handleListTrash))
	s.mux.Handle("POST /api/admin/rescan", s.requireAdmin(s.handleRescan))
	s.mux.Handle("GET /api/admin/scan-progress", s.requireAdmin(s.handleScanProgress))
	s.mux.Handle("GET /api/admin/users", s.requireAdmin(s.handleListUsers))
	s.mux.Handle("POST /api/admin/users", s.requireAdmin(s.handleCreateUser))
	s.mux.Handle("DELETE /api/admin/users/{id}", s.requireAdmin(s.handleDeleteUser))
	s.mux.Handle("GET /api/admin/permissions", s.requireAdmin(s.handleListPermissions))
	s.mux.Handle("POST /api/admin/permissions", s.requireAdmin(s.handleGrantPermission))
	s.mux.Handle("DELETE /api/admin/permissions", s.requireAdmin(s.handleRevokePermission))
}

func (s *Server) routes() {
	s.routesPublic()
	s.routesSharePublic()
	s.routesStatic()
	s.routesHTML()
	s.routesAuth()
	s.routesSets()
	s.routesMedia()
	s.routesNotes()
	s.routesProgress()
	s.routesShares()
	s.routesAdmin()
	s.routesPodcast()
}

// routesPodcast wires the podcast API routes.
func (s *Server) routesPodcast() {
	s.mux.Handle("GET /api/podcasts", s.requireSession(s.handleListPodcasts))
	s.mux.Handle("POST /api/podcasts", s.requireAdmin(s.handleSubscribePodcast))
	s.mux.Handle("GET /api/podcasts/{id}/episodes", s.requireSession(s.handleListEpisodes))
	s.mux.Handle("POST /api/podcasts/episodes/{episode_id}/download", s.requireSession(s.handleDownloadEpisode))
	s.mux.Handle("POST /api/podcasts/episodes/{episode_id}/complete", s.requireSession(s.handleToggleComplete))
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
