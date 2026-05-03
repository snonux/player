package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/scanner"
)

// scanService handles triggering and tracking media library scans.
type scanService struct {
	scanner    scanner.Scanner
	mediaRoot  string
	clock      clock.Clock
	logger     *slog.Logger
	mu         sync.Mutex
	scanCancel context.CancelFunc
	progress   *model.ScanProgress
	appCtx     context.Context // application-level context used to propagate shutdown cancellation
}

// NewScanService creates a ScanService.
func NewScanService(appCtx context.Context, sc scanner.Scanner, mediaRoot string, clk clock.Clock, logger *slog.Logger) *scanService {
	if logger == nil {
		logger = slog.Default()
	}
	return &scanService{
		scanner:   sc,
		mediaRoot: mediaRoot,
		clock:     clk,
		logger:    logger,
		appCtx:    appCtx,
	}
}

func (s *scanService) TriggerRescan(ctx context.Context) error {
	if s.scanner == nil {
		return fmt.Errorf("scanner not configured")
	}

	s.mu.Lock()
	if s.scanCancel != nil {
		s.scanCancel()
	}
	// Derive the scan context from the application-level context so that
	// cancellation propagates on server exit, while still applying a 30-minute timeout.
	scanCtx, cancel := context.WithTimeout(s.appCtx, 30*time.Minute)
	s.scanCancel = cancel
	progress := &model.ScanProgress{}
	s.progress = progress
	s.mu.Unlock()

	go func() {
		defer cancel()
		if err := s.scanner.Scan(scanCtx, s.mediaRoot, progress); err != nil {
			progress.Done(err)
			s.logger.Error("rescan failed", "err", err)
		} else {
			progress.Done(nil)
			s.logger.Info("rescan completed")
		}
	}()
	return nil
}

func (s *scanService) ScanProgress(ctx context.Context) model.ScanProgress {
	s.mu.Lock()
	progress := s.progress
	s.mu.Unlock()
	if progress == nil {
		return model.ScanProgress{}
	}
	return progress.Copy()
}
