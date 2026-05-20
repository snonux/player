package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
	"codeberg.org/snonux/player/internal/web"
)

// MediaServices groups the media-domain service dependencies used by route
// handlers. Keeping all media-related services in one sub-struct reduces the
// width of Server/ServerServices and makes it easy to see which concerns belong
// to the media vertical slice. If any service is nil its routes return 501.
type MediaServices struct {
	Browse        service.MediaBrowseService
	Write         service.MediaWriteService
	Share         service.MediaShareService
	Tag           service.MediaTagService
	Favorite      service.MediaFavoriteService
	Note          service.MediaNoteService
	Progress      service.ProgressService
	PlaybackHints service.PlaybackHintsService
}

// Server holds HTTP handlers and dependencies.
// Service dependencies are grouped into vertical slices (media, auth, admin,
// podcast) to reduce the width of the struct and clarify ownership boundaries.
type Server struct {
	store  repository.Store
	hasher auth.Hasher
	sm     auth.SessionManager
	cfg    *internal.Config
	// clk is the time source used for time-dependent handler logic (share
	// expiry, session cookie Expires, API token expiry). Injected so tests
	// can substitute a clock.MockClock and assert deterministic semantics
	// instead of racing the wall clock.
	clk    clock.Clock
	mux    *http.ServeMux
	handler http.Handler
	// media groups all media-domain services (browse, write, share, tags,
	// favorites, notes, progress, playback hints) into a single vertical slice.
	media         MediaServices
	authSvc       service.AuthService
	adminSvc      service.AdminService
	podcastSvc    service.PodcastEpisodeService
	streamer      service.MediaStreamer
	staticFS      http.FileSystem
	shareRenderer *web.SharePageRenderer
	logger        *slog.Logger
	mw            *Middleware
}

// ServerServices groups the optional service dependencies used by route handlers.
// Media-related services are collected into the Media sub-struct to reduce
// width and reflect the media vertical-slice boundary. Non-media services
// (Auth, Admin, Podcast) remain as direct fields.
// If any service is nil, its respective routes return 501.
type ServerServices struct {
	Media   MediaServices
	Auth    service.AuthService
	Admin   service.AdminService
	Podcast service.PodcastEpisodeService
}

// ServerDeps contains the dependencies needed to construct a Server.
type ServerDeps struct {
	Store          repository.Store
	Hasher         auth.Hasher
	SessionManager auth.SessionManager
	Config         *internal.Config
	Services       ServerServices
	StaticFS       http.FileSystem
	MediaStreamer  service.MediaStreamer
	// Clock is the time source for handler-level time arithmetic (share
	// expiry, session cookie Expires, API token expiry). If nil it defaults
	// to clock.RealClock{} so existing production callers and tests that
	// don't care about deterministic time keep working unchanged.
	Clock clock.Clock
}

// NewServer creates a Server with routes.
// It returns an error if required dependencies (e.g. Config) are missing
// so callers can handle invalid input gracefully instead of crashing.
func NewServer(deps ServerDeps) (*Server, error) {
	return NewServerWithLogger(deps, slog.Default())
}

// NewServerWithLogger creates a Server with routes and an injected logger.
// It returns an error if required dependencies (Config or MediaStreamer) are
// nil; previously these cases either panicked or were silently filled in at
// request time with a default streamer, which hid wiring mistakes and
// violated the Dependency Inversion Principle. Returning an error lets the
// caller (e.g. cmd/player/main.go) report the failure cleanly and exit with
// a useful message rather than crashing or quietly degrading.
func NewServerWithLogger(deps ServerDeps, logger *slog.Logger) (*Server, error) {
	if deps.Config == nil {
		return nil, errors.New("api.NewServerWithLogger: Config is nil")
	}
	// MediaStreamer is required: every file/stream/download/share handler
	// dispatches through Server.serveFileResult, which uses s.streamer
	// directly. Callers must inject one (production wiring builds a
	// service.NewMediaStreamer(remuxer)). This mirrors the explicit-deps
	// pattern set in commits 622827c (http.Client) and 92edb83 (TokenManager).
	if deps.MediaStreamer == nil {
		return nil, errors.New("api.NewServerWithLogger: MediaStreamer is nil")
	}
	if deps.StaticFS == nil {
		deps.StaticFS = http.Dir("web")
	}
	if logger == nil {
		logger = slog.Default()
	}
	// Default to the real wall-clock when no clock is injected so production
	// wiring stays simple and tests that don't pin time keep working.
	if deps.Clock == nil {
		deps.Clock = clock.RealClock{}
	}
	// Populate the media vertical-slice sub-struct directly from the nested
	// ServerServices.Media group so the Server never sees the flat list.
	s := &Server{
		store:   deps.Store,
		hasher:  deps.Hasher,
		sm:      deps.SessionManager,
		cfg:     deps.Config,
		clk:     deps.Clock,
		mux:     http.NewServeMux(),
		media:   deps.Services.Media,
		authSvc: deps.Services.Auth,
		adminSvc: deps.Services.Admin,
		podcastSvc: deps.Services.Podcast,
		streamer:      deps.MediaStreamer,
		staticFS:      deps.StaticFS,
		shareRenderer: web.NewSharePageRenderer(deps.StaticFS),
		logger:        logger,
		mw:            NewMiddleware(deps.Services.Auth, deps.SessionManager),
	}
	s.routes()
	s.handler = withCORS(s.cfg.CORSAllowedOrigins, s.mw.BootstrapRedirect(s.mux))
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
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

func (s *Server) handleBoth(method, path string, h http.Handler) {
	s.mux.Handle(method+" "+path, h)
	s.mux.Handle(method+" "+apiV1Path(path), h)
}

// handlePublic registers a path with the mux and marks it public in the
// middleware route registry so BootstrapRedirect lets it through without
// requiring an existing user account. Use for endpoints reachable before
// bootstrap completes (login, bootstrap, health probes, public HTML pages,
// individual static files).
func (s *Server) handlePublic(path string, h http.Handler) {
	s.mux.Handle(path, h)
	s.mw.RegisterPublic(path)
}

// handlePublicFunc is the http.HandlerFunc variant of handlePublic.
func (s *Server) handlePublicFunc(path string, h http.HandlerFunc) {
	s.mux.HandleFunc(path, h)
	s.mw.RegisterPublic(path)
}

// handlePublicPrefix registers a "directory" handler (e.g. /css/) and
// records the prefix in the middleware registry so anything served under it
// is treated as public.
func (s *Server) handlePublicPrefix(prefix string, h http.Handler) {
	s.mux.Handle(prefix, h)
	s.mw.RegisterPublicPrefix(prefix)
}

func apiV1Path(path string) string {
	const apiPrefix = "/api/"
	if !strings.HasPrefix(path, apiPrefix) {
		panic("apiV1Path: path must start with /api/")
	}
	return "/api/v1/" + strings.TrimPrefix(path, apiPrefix)
}

// routesPublic wires the fully-public API endpoints (bootstrap, login, probes).
// Each route is registered through handlePublic* helpers so the middleware
// route registry stays in sync — no separate whitelist to maintain.
//
// Note: the v1 aliases live under /api/v1/auth/... rather than the
// straight /api/v1/<rest> shape that apiV1Path() generates, so they are
// registered explicitly here instead of via a "both" helper.
func (s *Server) routesPublic() {
	s.handlePublicFunc("/api/bootstrap", publicMethod(http.MethodPost, s.handleBootstrap))
	s.handlePublicFunc("/api/v1/auth/bootstrap", publicMethod(http.MethodPost, s.handleBootstrap))
	s.handlePublicFunc("/api/login", publicMethod(http.MethodPost, s.handleLogin))
	s.handlePublicFunc("/api/v1/auth/login", publicMethod(http.MethodPost, s.handleLogin))
	s.handlePublicFunc("/healthz", publicMethod(http.MethodGet, s.handleHealthz))
	s.handlePublicFunc("/readyz", publicMethod(http.MethodGet, s.handleReadyz))
}

// routesSharePublic wires public share routes (no session required).
// Share routes are dynamic (/s/{token}/...), so we register both the
// specific mux patterns and a /s/ prefix in the public route registry to
// cover every concrete token-bearing URL.
func (s *Server) routesSharePublic() {
	s.mux.HandleFunc("GET /s/{token}", s.handleSharePage)
	s.mux.HandleFunc("GET /s/{token}/stream", s.handleShareStream)
	s.mux.HandleFunc("GET /s/{token}/thumbnail", s.handleShareThumbnail)
	s.mux.HandleFunc("GET /s/{token}/download", s.handleShareDownload)
	s.mw.RegisterPublicPrefix("/s/")
}

// routesStatic wires static CSS/JS asset serving.
// Both the directory prefixes (/css/, /js/, /images/) and the individual
// top-level asset files are registered public so the bootstrap page can
// load its resources before any user exists.
func (s *Server) routesStatic() {
	staticHandler := http.FileServer(s.staticFS)
	s.handlePublicPrefix("/css/", staticHandler)
	s.handlePublicPrefix("/js/", staticHandler)
	// /images/ isn't a mux-registered tree but is referenced by some HTML
	// pages; mark its prefix public so future asset additions just work.
	s.mw.RegisterPublicPrefix("/images/")
	s.handlePublic("/logo.png", staticHandler)
	s.handlePublic("/logo.svg", staticHandler)
	s.handlePublic("/favicon.ico", staticHandler)
	s.handlePublic("/favicon.svg", staticHandler)
	s.handlePublic("/manifest.json", staticHandler)
	s.handlePublic("/sw.js", staticHandler)
}

// routesHTML wires the SPA HTML page routes.
// /login.html and /bootstrap.html are public (the user reaches them before
// authenticating); the rest sit behind RequireSession.
func (s *Server) routesHTML() {
	s.handlePublic("/login.html", http.HandlerFunc(s.serveLogin))
	s.handlePublic("/bootstrap.html", http.HandlerFunc(s.serveBootstrap))
	s.mux.Handle("/", s.mw.RequireSession(http.HandlerFunc(s.serveIndex)))
	s.mux.Handle("GET /index.html", s.mw.RequireSession(http.HandlerFunc(s.serveIndex)))
	s.mux.Handle("GET /detach.html", s.mw.RequireSession(http.HandlerFunc(s.serveDetach)))
}

// routesAuth wires the logout route.
func (s *Server) routesAuth() {
	s.handleBoth(http.MethodPost, "/api/logout", s.requireSession(s.handleLogout))
	s.handleBoth(http.MethodPost, "/api/auth/tokens", s.requireSession(s.handleCreateAPIToken))
	s.handleBoth(http.MethodGet, "/api/auth/tokens", s.requireSession(s.handleListAPITokens))
	s.handleBoth(http.MethodDelete, "/api/auth/tokens/{id}", s.requireSession(s.handleRevokeAPIToken))
}

// routesConfig wires authenticated client configuration.
func (s *Server) routesConfig() {
	s.handleBoth(http.MethodGet, "/api/config", s.requireSession(s.handleConfig))
}

// routesSets wires the set-related API routes.
func (s *Server) routesSets() {
	s.handleBoth(http.MethodGet, "/api/sets", s.requireSession(s.handleListSets))
	s.handleBoth(http.MethodGet, "/api/sets/{id}/browse", s.requireSession(s.handleBrowseSet))
	s.handleBoth(http.MethodGet, "/api/sets/{id}/cover", s.requireSession(s.handleGetSetCover))
	s.handleBoth(http.MethodPost, "/api/sets/{id}/cover", s.requireSession(s.handlePostSetCover))
	s.handleBoth(http.MethodPost, "/api/sets/{id}/upload", s.requireSession(s.handleUpload))
}

// routesMedia wires the media-related API routes.
func (s *Server) routesMedia() {
	s.handleBoth(http.MethodGet, "/api/media", s.requireSession(s.handleListMedia))
	s.handleBoth(http.MethodGet, "/api/media/{id}", s.requireSession(s.handleGetMedia))
	s.handleBoth(http.MethodGet, "/api/media/{id}/stream", s.requireSession(s.handleStream))
	s.handleBoth(http.MethodGet, "/api/media/{id}/download", s.requireSession(s.handleDownload))
	s.handleBoth(http.MethodGet, "/api/media/{id}/thumbnail", s.requireSession(s.handleThumbnail))
	s.handleBoth(http.MethodPost, "/api/media/{id}/thumbnail", s.requireSession(s.handleRegenThumbnail))
	s.handleBoth(http.MethodPost, "/api/media/{id}/favorite", s.requireSession(s.handleFavorite))
	s.handleBoth(http.MethodGet, "/api/tags", s.requireSession(s.handleListTags))
	s.handleBoth(http.MethodPost, "/api/media/{id}/tags", s.requireSession(s.handleAddTag))
	s.handleBoth(http.MethodDelete, "/api/media/{id}/tags/{tag}", s.requireSession(s.handleRemoveTag))
	s.handleBoth(http.MethodDelete, "/api/media/{id}", s.requireSession(s.handleSoftDelete))
	s.handleBoth(http.MethodPost, "/api/media/{id}/restore", s.requireSession(s.handleRestore))
	s.handleBoth(http.MethodPost, "/api/media/{id}/shares", s.requireSession(s.handleCreateShare))
	s.handleBoth(http.MethodGet, "/api/media/{id}/shares", s.requireSession(s.handleListShares))
	s.handleBoth(http.MethodGet, "/api/media/{id}/playback", s.requireSession(s.handlePlaybackHints))
}

// routesNotes wires the notes API routes.
func (s *Server) routesNotes() {
	s.handleBoth(http.MethodGet, "/api/media/{id}/notes", s.requireSession(s.handleGetNote))
	s.handleBoth(http.MethodPost, "/api/media/{id}/notes", s.requireSession(s.handleUpsertNote))
	s.handleBoth(http.MethodDelete, "/api/media/{id}/notes", s.requireSession(s.handleDeleteNote))
}

// routesProgress wires the progress API routes.
func (s *Server) routesProgress() {
	s.handleBoth(http.MethodPost, "/api/progress", s.requireSession(s.handleProgress))
	s.handleBoth(http.MethodPost, "/api/progress/batch", s.requireSession(s.handleBatchProgress))
	s.handleBoth(http.MethodPost, "/api/progress/status", s.requireSession(s.handleProgressStatus))
	s.handleBoth(http.MethodGet, "/api/in-progress", s.requireSession(s.handleInProgress))
}

// routesShares wires the share-management API routes.
func (s *Server) routesShares() {
	s.handleBoth(http.MethodDelete, "/api/shares/{token}", s.requireSession(s.handleRevokeShare))
	s.handleBoth(http.MethodGet, "/api/shares", s.requireSession(s.handleMyShares))
}

// routesAdmin wires the admin-only API routes.
func (s *Server) routesAdmin() {
	s.handleBoth(http.MethodGet, "/api/admin/trash", s.requireAdmin(s.handleListTrash))
	s.handleBoth(http.MethodPost, "/api/admin/rescan", s.requireAdmin(s.handleRescan))
	s.handleBoth(http.MethodGet, "/api/admin/scan-progress", s.requireAdmin(s.handleScanProgress))
	s.handleBoth(http.MethodGet, "/api/admin/users", s.requireAdmin(s.handleListUsers))
	s.handleBoth(http.MethodPost, "/api/admin/users", s.requireAdmin(s.handleCreateUser))
	s.handleBoth(http.MethodDelete, "/api/admin/users/{id}", s.requireAdmin(s.handleDeleteUser))
	s.handleBoth(http.MethodGet, "/api/admin/permissions", s.requireAdmin(s.handleListPermissions))
	s.handleBoth(http.MethodPost, "/api/admin/permissions", s.requireAdmin(s.handleGrantPermission))
	s.handleBoth(http.MethodDelete, "/api/admin/permissions", s.requireAdmin(s.handleRevokePermission))
}

func (s *Server) routes() {
	s.routesPublic()
	s.routesSharePublic()
	s.routesStatic()
	s.routesHTML()
	s.routesAuth()
	s.routesConfig()
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
	s.handleBoth(http.MethodGet, "/api/podcasts", s.requireSession(s.handleListPodcasts))
	s.handleBoth(http.MethodPost, "/api/podcasts", s.requireAdmin(s.handleSubscribePodcast))
	s.handleBoth(http.MethodGet, "/api/podcasts/{id}/episodes", s.requireSession(s.handleListEpisodes))
	s.handleBoth(http.MethodPost, "/api/podcasts/episodes/{episode_id}/download", s.requireSession(s.handleDownloadEpisode))
	s.handleBoth(http.MethodPost, "/api/podcasts/episodes/{episode_id}/complete", s.requireSession(s.handleToggleComplete))
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
