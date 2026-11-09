package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// FeedChecker refreshes the podcast feeds that are due.
type FeedChecker interface {
	CheckFeeds(ctx context.Context) error
}

// PodcastCheckWorker runs the background podcast feed check inside the
// worker lifecycle: Stop cancels a running check (in-flight fetches see the
// cancelled context) and waits for it, so the database can be closed safely
// afterwards. The first check runs shortly after Start, so frequent
// restarts do not keep postponing it by a full interval.
type PodcastCheckWorker struct {
	checker      FeedChecker
	interval     time.Duration
	initialDelay time.Duration
	logger       *slog.Logger

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once
}

// DefaultPodcastCheckDelay is how long after start the first check runs.
const DefaultPodcastCheckDelay = time.Minute

// NewPodcastCheckWorker creates a worker checking every interval, the first
// time after initialDelay. parent bounds its lifetime (the app context).
func NewPodcastCheckWorker(parent context.Context, checker FeedChecker, interval, initialDelay time.Duration, logger *slog.Logger) *PodcastCheckWorker {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(parent)
	return &PodcastCheckWorker{checker: checker, interval: interval, initialDelay: initialDelay, logger: logger, ctx: ctx, cancel: cancel}
}

// Start launches the check loop once.
func (w *PodcastCheckWorker) Start() {
	if w == nil || w.checker == nil || w.interval <= 0 {
		return
	}
	w.startOnce.Do(func() {
		w.wg.Add(1)
		go w.loop()
	})
}

// Stop cancels the running check and waits for it to return.
func (w *PodcastCheckWorker) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		w.cancel()
		w.wg.Wait()
	})
}

func (w *PodcastCheckWorker) loop() {
	defer w.wg.Done()
	timer := time.NewTimer(w.initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-timer.C:
		}
		w.runOnce()
		timer.Reset(w.interval)
	}
}

func (w *PodcastCheckWorker) runOnce() {
	defer func() {
		RecoverWorker(w.logger, "podcast checker", recover())
	}()
	if err := w.checker.CheckFeeds(w.ctx); err != nil && w.ctx.Err() == nil {
		w.logger.Error("podcast feed check failed", "err", err)
	}
}
