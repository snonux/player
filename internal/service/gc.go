package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/paul/kiss-media-player/internal/clock"
	"github.com/paul/kiss-media-player/internal/repository"
)

// GCWorker is a background worker that hard-deletes soft-deleted media older than a threshold.
type GCWorker struct {
	store     repository.Store
	clock     clock.Clock
	interval  time.Duration
	age       time.Duration
	logger    *slog.Logger
	ticker    *time.Ticker
	stopCh    chan struct{}
	wg        sync.WaitGroup
	mediaRoot string
}

// NewGCWorker creates a GCWorker. Use WithAge and WithInterval to customise.
func NewGCWorker(store repository.Store, clk clock.Clock, mediaRoot string, interval time.Duration, logger *slog.Logger) *GCWorker {
	return &GCWorker{
		store:     store,
		clock:     clk,
		interval:  interval,
		age:       7 * 24 * time.Hour,
		logger:    logger,
		stopCh:    make(chan struct{}),
		mediaRoot: mediaRoot,
	}
}

// WithAge overrides the default 7-day deletion age.
func (w *GCWorker) WithAge(age time.Duration) *GCWorker {
	w.age = age
	return w
}

// WithInterval overrides the ticker interval (used in tests that need deterministic ticks).
func (w *GCWorker) WithInterval(interval time.Duration) *GCWorker {
	w.interval = interval
	return w
}

// Start launches the GC goroutine.
func (w *GCWorker) Start() {
	w.ticker = time.NewTicker(w.interval)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case <-w.ticker.C:
				w.run()
			case <-w.stopCh:
				return
			}
		}
	}()
}

// Stop stops the GC goroutine and waits for it to finish.
func (w *GCWorker) Stop() {
	if w.ticker != nil {
		w.ticker.Stop()
	}
	close(w.stopCh)
	w.wg.Wait()
}

func (w *GCWorker) run() {
	ctx := context.Background()
	items, err := w.store.ListDeletedMedia(ctx)
	if err != nil {
		if w.logger != nil {
			w.logger.Error("gc list deleted media", "err", err)
		}
		return
	}

	cutoff := w.clock.Now().Add(-w.age)
	for _, item := range items {
		if item.DeletedAt == nil || !item.DeletedAt.Before(cutoff) {
			continue
		}

		absPath := item.AbsPath
		if absPath == "" {
			absPath = filepath.Join(w.mediaRoot, item.RelPath)
		}

		if absPath != "" {
			if err := os.Remove(absPath); err != nil {
				if w.logger != nil {
					w.logger.Warn("gc remove file", "path", absPath, "err", err)
				}
				continue
			}
		}

		if err := w.store.HardDeleteMedia(ctx, item.ID); err != nil {
			if w.logger != nil {
				w.logger.Error("gc hard delete", "id", item.ID, "err", err)
			}
			continue
		}

		if w.logger != nil {
			w.logger.Info("gc deleted media", "id", item.ID, "path", absPath)
		}
	}
}

// RunOnce performs a single GC run synchronously. Useful for tests.
func (w *GCWorker) RunOnce() error {
	if w.interval == 0 {
		return fmt.Errorf("worker not started")
	}
	w.run()
	return nil
}
