package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"codeberg.org/snonux/play/internal"
	"codeberg.org/snonux/play/internal/api"
	"codeberg.org/snonux/play/internal/auth"
	"codeberg.org/snonux/play/internal/clock"
	"codeberg.org/snonux/play/internal/probe"
	"codeberg.org/snonux/play/internal/repository"
	"codeberg.org/snonux/play/internal/scanner"
	"codeberg.org/snonux/play/internal/service"
	"codeberg.org/snonux/play/internal/thumb"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(internal.Version)
		os.Exit(0)
	}

	cfg, err := internal.LoadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	store, err := repository.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("failed to close database: %v", err)
		}
	}()

	clk := clock.RealClock{}
	hasher := auth.NewBCryptHasher(12)
	sm := auth.NewSessionManager(store, clk, time.Duration(cfg.SessionTimeoutHours)*time.Hour)

	mediaSvc := service.NewMediaService(store, clk, cfg.MediaRoot)

	prober := probe.NewFFProber()
	thumbGen := thumb.NewFFmpegGenerator()
	fsScanner := scanner.NewFSScanner(store, prober, thumbGen, clk, cfg.MediaRoot)
	adminSvc := service.NewAdminService(store, clk, hasher, fsScanner, cfg.MediaRoot)

	progressSvc := service.NewProgressService(store, clk)

	staticFS := http.Dir("web")
	server := api.NewServer(store, hasher, sm, cfg, mediaSvc, adminSvc, progressSvc, staticFS)

	gs := api.NewGracefulServer(server, cfg)

	log.Printf("kiss-media-player %s starting on %s", internal.Version, gs.Server.Addr)

	go func() {
		if err := gs.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := gs.Server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("failed to shutdown server: %v", err)
	}
	log.Println("server stopped")
}
