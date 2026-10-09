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
	"codeberg.org/snonux/player/internal/transcode"
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
	CompatSvc       service.CompatStreamService
	// Transcodes is the rendition cache behind CompatSvc. RunWithSignal
	// uses it for the startup check and to wait for running ffmpeg jobs on
	// shutdown.
	Transcodes *transcode.Cache
	Scanner    scanner.Scanner
	GCWorker   *service.GCWorker
	Logger     *slog.Logger
	AppCtx     context.Context
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
	return WireWithRunner(cfg, store, logger, appCtx, transcode.NewFFmpegRunner(cfg.TranscodeMaxJobs))
}

// WireWithRunner is Wire with an injectable transcode runner, so tests can
// exercise the complete production wiring (real store, services, routes)
// without running ffmpeg.
func WireWithRunner(cfg *internal.Config, store repository.Store, logger *slog.Logger, appCtx context.Context, runner transcode.Runner) *Deps {
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

	deps := &Deps{
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
		Logger:          logger,
		AppCtx:          appCtx,
	}
	wireTranscoding(deps, runner)
	return deps
}

// wireTranscoding adds the compatibility stream to deps: the rendition
// cache, the service in front of it, and a GC worker that maintains the
// cache. It is split from WireWithRunner to keep both readable.
func wireTranscoding(deps *Deps, runner transcode.Runner) {
	cfg := deps.Cfg
	// The access helper is stateless, so a second instance applies exactly
	// the same permission rules as the one the other services share.
	helper := service.NewAccessHelper(deps.Store)
	// Renditions for formats the clients cannot decode (AVI/WMV/FLV/WMA).
	// Transcodes are bound to AppCtx, not to the triggering request, so
	// they finish in the background and stop on shutdown.
	deps.Transcodes = transcode.NewCache(deps.AppCtx, runner, deps.Clk, deps.Logger, transcode.Options{
		Dir:           cfg.TranscodeCacheDir,
		MaxBytes:      int64(cfg.TranscodeCacheMaxMB) * 1024 * 1024,
		MaxConcurrent: cfg.TranscodeMaxJobs,
	})
	// A dedicated share service instance provides the narrow
	// SharedMediaAccess interface; it is stateless, so it behaves exactly
	// like the one inside MediaSvc.
	shares := service.NewShareService(deps.Store, deps.Clk, helper)
	deps.CompatSvc = service.NewCompatStreamService(helper, shares, deps.Transcodes, cfg.MediaRoot)

	// The GC tick also bounds the transcode cache and drops renditions of
	// hard-deleted media.
	deps.GCWorker = service.NewGCWorker(deps.Store, deps.Clk, cfg.MediaRoot, time.Duration(cfg.GCIntervalMinutes)*time.Minute, deps.Logger).
		WithRenditionCache(deps.Transcodes)
}

// checkTranscoding reports transcoding misconfiguration at startup. Problems
// are logged, not fatal: everything except the compatibility stream works
// without ffmpeg or a writable cache, and the log line saves the operator
// from finding out through a failed playback.
func checkTranscoding(deps *Deps, runner *transcode.FFmpegRunner) {
	if err := runner.Available(); err != nil {
		deps.Logger.Warn("compatibility stream unavailable", "err", err)
	}
	if err := deps.Transcodes.Preflight(); err != nil {
		deps.Logger.Warn("compatibility stream unavailable", "dir", deps.Cfg.TranscodeCacheDir, "err", err)
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
	return runServer(handler, cfg, logger, sigCh, nil)
}

// runServer is RunServer with a hook that runs when shutdown begins, before
// the HTTP server is asked to drain. Requests that block on something other
// than I/O (a compat request waits up to 20 s for a transcode) must be
// released there, otherwise they outlast the 5 s drain window.
func runServer(handler http.Handler, cfg *internal.Config, logger *slog.Logger, sigCh <-chan os.Signal, beforeShutdown func()) error {
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

	if beforeShutdown != nil {
		beforeShutdown()
	}
	return shutdownGracefully(gs, logger)
}

// NewAPIServer builds the HTTP API server from the wired dependencies. It is
// separate from RunWithSignal so the service-to-route wiring (which the
// compiler cannot check: an unset service silently turns its routes into 501)
// can be exercised by a test without starting a listener.
func NewAPIServer(deps *Deps, staticFS http.FileSystem, logger *slog.Logger) (*api.Server, error) {
	cfg := deps.Cfg
	streamer := service.NewMediaStreamer(probe.NewFFRemuxer(), cfg.MediaRoot)
	return api.NewServerWithLogger(api.ServerDeps{
		Store:          deps.Store,
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
				Compat:        deps.CompatSvc,
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

	runner := transcode.NewFFmpegRunner(cfg.TranscodeMaxJobs)
	deps := WireWithRunner(cfg, store, logger, appCtx, runner)
	defer deps.GCWorker.Stop()
	defer stopTranscoding(deps)
	checkTranscoding(deps, runner)
	StartBackgroundWorkers(deps)

	server, err := NewAPIServer(deps, http.Dir("web"), logger)
	if err != nil {
		return fmt.Errorf("failed to create API server: %w", err)
	}

	// Shutdown order: first close the transcode cache, which releases
	// waiting compat requests (they answer 503) and kills ffmpeg; then
	// drain the HTTP server; then stopTranscoding waits a bounded time for
	// the job goroutines; finally the deferred store.Close runs in any case.
	return runServer(server, cfg, logger, sigCh, deps.Transcodes.Close)
}

// transcodeStopTimeout bounds how long shutdown waits for transcode jobs
// after their ffmpeg processes were killed. A job stuck in filesystem I/O
// (a hung network mount) must not keep the process — and the database —
// open until the supervisor resorts to SIGKILL.
const transcodeStopTimeout = 10 * time.Second

// stopTranscoding closes the transcode cache and waits, bounded, for its
// jobs, so that normally no ffmpeg process or temporary file outlives the
// server. It is safe to call when the cache was closed already.
func stopTranscoding(deps *Deps) {
	deps.Transcodes.Close()
	if !deps.Transcodes.WaitTimeout(transcodeStopTimeout) {
		deps.Logger.Warn("transcode jobs still running at shutdown; continuing", "waited", transcodeStopTimeout)
	}
}
