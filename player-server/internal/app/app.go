// Package app contains the application bootstrap, dependency wiring, and
// server lifecycle. It is the single place responsible for constructing all
// service/repository dependencies and starting/stopping the HTTP server.
// cmd/player/main.go is intentionally kept thin: it parses CLI flags, loads
// config, and delegates everything else to this package.
package app

import (
	"context"
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

// Deps bundles all wired service-layer dependencies assembled during
// application bootstrap. It is the single structure passed between the
// wiring stage and the server-start stage so that the two concerns remain
// clearly separated.
type Deps struct {
	Store           repository.Store
	Hasher          auth.Hasher
	SM              auth.SessionManager
	Cfg             *internal.Config
	Clk             clock.Clock
	MediaSvc        service.MediaService
	AdminSvc        service.AdminService
	ProgressSvc     service.ProgressService
	AuthSvc         service.AuthService
	PodcastSvc      service.PodcastEpisodeService
	PlaybackHintSvc service.PlaybackHintsService
	Scanner         scanner.Scanner
	GCWorker        *service.GCWorker
	Logger          *slog.Logger
	AppCtx          context.Context
	// WorkersStarted is an optional channel that receives a signal once all
	// background workers have been started. Tests use this to synchronise
	// without polling or sleeping.
	WorkersStarted chan<- struct{}
}

// BuildLogger creates a slog.Logger aligned with the named log level.
// Unknown levels fall back to INFO so the application always produces
// structured output even when misconfigured.
func BuildLogger(logLevel string) *slog.Logger {
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

// Wire constructs the full service-layer dependency graph from the provided
// config and store. It does NOT start any background goroutines; that is the
// responsibility of StartBackgroundWorkers. Separating construction from
// activation makes the wiring easy to test in isolation.
func Wire(cfg *internal.Config, store repository.Store, logger *slog.Logger, appCtx context.Context) *Deps {
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

	return &Deps{
		Store:           store,
		Hasher:          hasher,
		SM:              sm,
		Cfg:             cfg,
		Clk:             clk,
		MediaSvc:        mediaSvc,
		AdminSvc:        adminSvc,
		ProgressSvc:     progressSvc,
		AuthSvc:         authSvc,
		PodcastSvc:      podcastSvc,
		PlaybackHintSvc: playbackHintSvc,
		Scanner:         fsScanner,
		GCWorker:        gcWorker,
		Logger:          logger,
		AppCtx:          appCtx,
	}
}

// StartBackgroundWorkers launches background goroutines (GC worker, podcast
// feed checker). It must be called after Wire() and before RunServer().
// Workers are stopped either by cancelling AppCtx or by calling
// deps.GCWorker.Stop().
func StartBackgroundWorkers(deps *Deps) {
	deps.GCWorker.Start()

	// Start podcast feed background checker. It runs on a fixed ticker and
	// exits when the application context is cancelled.
	go func() {
		ticker := time.NewTicker(time.Duration(deps.Cfg.PodcastCheckMinutes) * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				func() {
					// Use the unified service.RecoverWorker helper so this
					// matches every other background-worker panic path in
					// the codebase (gc, rescan, podcast feed check).
					defer func() {
						service.RecoverWorker(deps.Logger, "podcast checker", recover())
					}()
					if err := deps.PodcastSvc.CheckFeeds(context.Background()); err != nil {
						deps.Logger.Error("podcast feed check failed", "err", err)
					}
				}()
			case <-deps.AppCtx.Done():
				return
			}
		}
	}()

	// Signal to callers (typically tests) that all workers are running.
	if deps.WorkersStarted != nil {
		select {
		case deps.WorkersStarted <- struct{}{}:
		default:
		}
	}
}

// ensureSignalChannel returns the provided channel or creates a new one wired
// to OS interrupt signals (SIGINT, SIGTERM). Production callers pass nil to
// get real OS-signal behaviour; tests inject a synthetic channel.
func ensureSignalChannel(sigCh <-chan os.Signal) <-chan os.Signal {
	if sigCh != nil {
		return sigCh
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return quit
}

// shutdownGracefully performs a timed graceful shutdown of the HTTP server.
// It allows up to five seconds for in-flight requests to complete before
// forcing the server to stop.
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

// RunServer starts the HTTP server and blocks until a shutdown signal is
// received or the server returns an error. It is the last step in the
// application lifecycle and returns only after a graceful shutdown attempt.
func RunServer(handler http.Handler, cfg *internal.Config, logger *slog.Logger, sigCh <-chan os.Signal) error {
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

// RunWithSignal is the primary application entry point after flag parsing and
// config loading. It opens the database, wires all dependencies, starts
// background workers, and runs the HTTP server until a shutdown signal
// arrives. sigCh may be nil for production use (OS signals are used); tests
// inject a synthetic channel to drive shutdown deterministically.
func RunWithSignal(cfg *internal.Config, logger *slog.Logger, sigCh <-chan os.Signal) error {
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

	deps := Wire(cfg, store, logger, appCtx)
	defer deps.GCWorker.Stop()
	StartBackgroundWorkers(deps)

	staticFS := http.Dir("web")
	remuxer := probe.NewFFRemuxer()
	streamer := service.NewMediaStreamer(remuxer, cfg.MediaRoot)
	server, err := api.NewServerWithLogger(api.ServerDeps{
		Store:          store,
		Hasher:         deps.Hasher,
		SessionManager: deps.SM,
		Config:         cfg,
		// Use the grouped MediaServices sub-struct to wire all media-domain
		// services in one block, reducing the width of the ServerServices literal.
		Services: api.ServerServices{
			Media: api.MediaServices{
				Browse:        deps.MediaSvc,
				Write:         deps.MediaSvc,
				Share:         deps.MediaSvc,
				Tag:           deps.MediaSvc,
				Favorite:      deps.MediaSvc,
				Note:          deps.MediaSvc,
				Progress:      deps.ProgressSvc,
				PlaybackHints: deps.PlaybackHintSvc,
			},
			Admin:   deps.AdminSvc,
			Auth:    deps.AuthSvc,
			Podcast: deps.PodcastSvc,
		},
		StaticFS:      staticFS,
		MediaStreamer: streamer,
		// Share the already-wired clock so handler-level time arithmetic
		// (share expiry, session cookie Expires, API token expiry) uses
		// the same source as the rest of the services (scanner, auth, etc).
		Clock: deps.Clk,
	}, logger)
	if err != nil {
		return fmt.Errorf("failed to create API server: %w", err)
	}

	return RunServer(server, cfg, logger, sigCh)
}
