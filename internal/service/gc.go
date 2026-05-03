package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/repository"
)

// GCWorker is a background worker that hard-deletes soft-deleted media older than a threshold.
type GCWorker struct {
	store     repository.GCStore
	clock     clock.Clock
	interval  time.Duration
	age       time.Duration
	logger    *slog.Logger
	ticker    *time.Ticker
	tickCh    <-chan time.Time
	runDoneCh chan struct{}
	stopCh    chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
	mediaRoot string
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewGCWorker creates a GCWorker. Use WithAge and WithInterval to customise.
func NewGCWorker(store repository.GCStore, clk clock.Clock, mediaRoot string, interval time.Duration, logger *slog.Logger) *GCWorker {
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
	w.ctx, w.cancel = context.WithCancel(context.Background())
	tickCh := w.tickCh
	if tickCh == nil {
		w.ticker = time.NewTicker(w.interval)
		tickCh = w.ticker.C
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case <-tickCh:
				w.run(w.ctx)
				w.notifyRunDone()
			case <-w.stopCh:
				return
			}
		}
	}()
}

// Stop stops the GC goroutine and waits for it to finish.
// Safe to call multiple times or before Start() (idempotent, no-op).
func (w *GCWorker) Stop() {
	w.stopOnce.Do(func() {
		if w.ticker != nil {
			w.ticker.Stop()
		}
		if w.cancel != nil {
			w.cancel()
		}
		close(w.stopCh)
	})
	w.wg.Wait()
}

func (w *GCWorker) run(ctx context.Context) {
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
			absPath = filepath.Clean(filepath.Join(w.mediaRoot, item.RelPath))
		}

		if err := w.store.HardDeleteMedia(ctx, item.ID); err != nil {
			if w.logger != nil {
				w.logger.Error("gc hard delete", "id", item.ID, "err", err)
			}
			continue
		}

		if absPath != "" {
			if err := os.Remove(absPath); err != nil {
				if w.logger != nil {
					w.logger.Warn("gc remove file", "path", absPath, "err", err)
				}
				continue
			}
		}

		if w.logger != nil {
			w.logger.Info("gc deleted media", "id", item.ID, "path", absPath)
		}
	}
}

func (w *GCWorker) notifyRunDone() {
	if w.runDoneCh == nil {
		return
	}
	select {
	case w.runDoneCh <- struct{}{}:
	default:
	}
}

// RunOnce performs a single GC run synchronously. Useful for tests.
func (w *GCWorker) RunOnce() error {
	if w.interval == 0 {
		return fmt.Errorf("worker not started")
	}
	ctx := w.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	w.run(ctx)
	return nil
}
