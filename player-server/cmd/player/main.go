package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/api"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/scanner"
	"codeberg.org/snonux/player/internal/service"
	"codeberg.org/snonux/player/internal/thumb"
)

// appDeps bundles all wired service-layer dependencies.
type appDeps struct {
	store           repository.Store
	hasher          auth.Hasher
	sm              auth.SessionManager
	cfg             *internal.Config
	clk             clock.Clock
	mediaSvc        service.MediaService
	adminSvc        service.AdminService
	progressSvc     service.ProgressService
	authSvc         service.AuthService
	podcastSvc      service.PodcastEpisodeService
	playbackHintSvc service.PlaybackHintsService
	scanner         scanner.Scanner
	gcWorker        *service.GCWorker
	logger          *slog.Logger
	appCtx          context.Context
	workersStarted  chan<- struct{}
}

// parseVersionFlag parses CLI flags and returns whether --version was requested.
func parseVersionFlag(args []string) (bool, error) {
	fs := flag.NewFlagSet("player", flag.ContinueOnError)
	versionFlag := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	return *versionFlag, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithSignal(args, nil)
}

// buildLogger creates a slog.Logger aligned with the named log level.
func buildLogger(logLevel string) *slog.Logger {
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// wireDeps constructs the core service layer dependencies.
func wireDeps(cfg *internal.Config, store repository.Store, logger *slog.Logger, appCtx context.Context) *appDeps {
	clk := clock.RealClock{}
	hasher := auth.NewBCryptHasher(12)
	sm := auth.NewSessionManager(store, clk, time.Duration(cfg.SessionTimeoutHours)*time.Hour)
	tm := auth.NewTokenManager()

	prober := probe.NewFFProber()
	thumbGen := thumb.NewFFmpegGenerator()
	// Explicit filesystem thumbnail resolver: keeps service.GetThumbnail
	// free of direct os.Stat calls and makes the dependency easy to swap
	// out in tests or alternate deployments (e.g. object storage).
	thumbResolver := thumb.NewFSResolver()
	// thumb.FSMaker encapsulates the "create .thumbnails dir + invoke
	// generator + warn-on-failure" policy so the scanner only
	// orchestrates the scan and does not own thumbnail layout policy.
	thumbMaker := thumb.NewFSMaker(thumbGen, nil, logger)

	helper := service.NewAccessHelper(store)
	browser := service.NewPodcastBrowseService(store, cfg.MediaRoot)
	mediaSvc := service.NewMediaServiceWithDeps(store, clk, cfg.MediaRoot, thumbGen, prober, browser, thumbResolver)
	playbackHintSvc := service.NewPlaybackHintsService(helper)

	fsScanner := scanner.NewFSScannerWithMaker(store, prober, thumbMaker, clk, cfg.MediaRoot, logger)
	adminSvc := service.NewAdminServiceWithLogger(store, clk, hasher, fsScanner, cfg.MediaRoot, appCtx, logger)

	progressSvc := service.NewProgressService(store, clk)
	authSvc := service.NewAuthService(store, clk, hasher, sm, tm)

	podcastSvc := service.NewPodcastServiceWithLogger(store, clk, cfg.MediaRoot, helper, prober, thumbGen, &http.Client{Timeout: service.DefaultHTTPClientTimeout}, cfg.PodcastCheckMinutes, logger)

	gcWorker := service.NewGCWorker(store, clk, cfg.MediaRoot, time.Duration(cfg.GCIntervalMinutes)*time.Minute, logger)

	return &appDeps{
		store:           store,
		hasher:          hasher,
		sm:              sm,
		cfg:             cfg,
		clk:             clk,
		mediaSvc:        mediaSvc,
		adminSvc:        adminSvc,
		progressSvc:     progressSvc,
		authSvc:         authSvc,
		podcastSvc:      podcastSvc,
		playbackHintSvc: playbackHintSvc,
		scanner:         fsScanner,
		gcWorker:        gcWorker,
		logger:          logger,
		appCtx:          appCtx,
	}
}

// startBackgroundWorkers launches background goroutines (GC, podcast feed checker).
func startBackgroundWorkers(deps *appDeps) {
	deps.gcWorker.Start()

	// Start podcast feed background checker.
	go func() {
		ticker := time.NewTicker(time.Duration(deps.cfg.PodcastCheckMinutes) * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				func() {
					// Use the unified service.RecoverWorker helper so this
					// matches every other background-worker panic path in
					// the codebase (gc, rescan, podcast feed check).
					defer func() {
						service.RecoverWorker(deps.logger, "podcast checker", recover())
					}()
					if err := deps.podcastSvc.CheckFeeds(context.Background()); err != nil {
						deps.logger.Error("podcast feed check failed", "err", err)
					}
				}()
			case <-deps.appCtx.Done():
				return
			}
		}
	}()
	if deps.workersStarted != nil {
		select {
		case deps.workersStarted <- struct{}{}:
		default:
		}
	}
}

// ensureSignalChannel returns the provided channel or creates a new one wired
// to OS interrupt signals.
func ensureSignalChannel(sigCh <-chan os.Signal) <-chan os.Signal {
	if sigCh != nil {
		return sigCh
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return quit
}

// shutdownGracefully performs a timed graceful shutdown of the server.
func shutdownGracefully(gs *api.GracefulServer, logger *slog.Logger) error {
	logger.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := gs.Server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	logger.Info("server stopped")
	return nil
}

// runServer starts the HTTP server and blocks until shutdown.
func runServer(handler http.Handler, cfg *internal.Config, logger *slog.Logger, sigCh <-chan os.Signal) error {
	gs := api.NewGracefulServer(handler, cfg)

	logger.Info("player starting", "version", internal.Version, "addr", gs.Server.Addr)

	errCh := make(chan error, 1)
	go func() {
		if err := gs.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("failed to start server: %w", err)
		}
	}()

	sigCh = ensureSignalChannel(sigCh)

	select {
	case <-sigCh:
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	return shutdownGracefully(gs, logger)
}

func runWithSignal(args []string, sigCh <-chan os.Signal) error {
	showVersion, err := parseVersionFlag(args)
	if err != nil {
		return err
	}
	if showVersion {
		fmt.Println(internal.Version)
		return nil
	}

	cfg, err := internal.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger := buildLogger(cfg.LogLevel)

	store, err := repository.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("failed to close database", "err", err)
		}
	}()

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	deps := wireDeps(cfg, store, logger, appCtx)
	defer deps.gcWorker.Stop()
	startBackgroundWorkers(deps)

	staticFS := http.Dir("web")
	remuxer := probe.NewFFRemuxer()
	streamer := service.NewMediaStreamer(remuxer, cfg.MediaRoot)
	server, err := api.NewServerWithLogger(api.ServerDeps{
		Store:          store,
		Hasher:         deps.hasher,
		SessionManager: deps.sm,
		Config:         cfg,
		Services: api.ServerServices{
			Browse:        deps.mediaSvc,
			Write:         deps.mediaSvc,
			Share:         deps.mediaSvc,
			Tag:           deps.mediaSvc,
			Favorite:      deps.mediaSvc,
			Note:          deps.mediaSvc,
			Admin:         deps.adminSvc,
			Progress:      deps.progressSvc,
			Auth:          deps.authSvc,
			Podcast:       deps.podcastSvc,
			PlaybackHints: deps.playbackHintSvc,
		},
		StaticFS:      staticFS,
		MediaStreamer: streamer,
		// Share the already-wired clock so handler-level time arithmetic
		// (share expiry, session cookie Expires, API token expiry) uses
		// the same source as the rest of the services (scanner, auth, etc).
		Clock: deps.clk,
	}, logger)
	if err != nil {
		return fmt.Errorf("failed to create API server: %w", err)
	}

	return runServer(server, cfg, logger, sigCh)
}
