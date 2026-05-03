package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	return runWithSignal(args, nil)
}

func runWithSignal(args []string, sigCh <-chan os.Signal) error {
	fs := flag.NewFlagSet("mediaplayer", flag.ContinueOnError)
	versionFlag := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *versionFlag {
		fmt.Println(internal.Version)
		return nil
	}

	cfg, err := internal.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, err := repository.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("failed to close database: %v", err)
		}
	}()

	// Build logger aligned with the configured log level.
	var level slog.Level
	switch cfg.LogLevel {
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	clk := clock.RealClock{}
	hasher := auth.NewBCryptHasher(12)
	sm := auth.NewSessionManager(store, clk, time.Duration(cfg.SessionTimeoutHours)*time.Hour)

	prober := probe.NewFFProber()
	thumbGen := thumb.NewFFmpegGenerator()
	mediaSvc := service.NewMediaService(store, clk, cfg.MediaRoot, thumbGen, prober)

	fsScanner := scanner.NewFSScanner(store, prober, thumbGen, clk, cfg.MediaRoot)
	adminSvc := service.NewAdminService(store, clk, hasher, fsScanner, cfg.MediaRoot)

	progressSvc := service.NewProgressService(store, clk)
	authSvc := service.NewAuthService(store, clk, hasher, sm)

	// Start the background GC worker that hard-deletes soft-deleted media.
	gcWorker := service.NewGCWorker(store, clk, cfg.MediaRoot, time.Duration(cfg.GCIntervalMinutes)*time.Minute, logger)
	gcWorker.Start()
	defer gcWorker.Stop()

	staticFS := http.Dir("web")
	server := api.NewServer(store, hasher, sm, cfg, mediaSvc, adminSvc, progressSvc, authSvc, staticFS)

	gs := api.NewGracefulServer(server, cfg)

	log.Printf("player %s starting on %s", internal.Version, gs.Server.Addr)

	errCh := make(chan error, 1)
	go func() {
		if err := gs.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("failed to start server: %w", err)
		}
	}()

	if sigCh == nil {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sigCh = quit
	}

	select {
	case <-sigCh:
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	log.Println("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := gs.Server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	log.Println("server stopped")
	return nil
}
