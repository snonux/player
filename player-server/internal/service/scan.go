package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/scanner"
)

// ScanLifecycle is the application-owned scan worker lifecycle.
type ScanLifecycle interface {
	StartScanAutomation()
	StopScans()
}

// scanService owns one active scan and at most one coalesced follow-up.
type scanService struct {
	scanner                            scanner.Scanner
	mediaRoot                          string
	clock                              clock.Clock
	logger                             *slog.Logger
	mu                                 sync.Mutex
	scanCancel                         context.CancelFunc
	progress                           *model.ScanProgress
	appCtx                             context.Context
	doneCh                             chan<- struct{}
	active, stopped, automationStarted bool
	pending, trigger, state            string
	wg                                 sync.WaitGroup
	autoCancel                         context.CancelFunc
	watchSeconds, intervalMinutes      int
	signature                          func(context.Context) (string, error)
	watchError                         string
}

type scanAutomationState struct {
	nextWatch, nextScan time.Time
	previous            string
	baseline            bool
}

// NewScanService creates a ScanService.
func NewScanService(appCtx context.Context, sc scanner.Scanner, mediaRoot string, clk clock.Clock, logger *slog.Logger) *scanService {
	if logger == nil {
		logger = slog.Default()
	}
	return &scanService{scanner: sc, mediaRoot: mediaRoot, clock: clk, logger: logger, appCtx: appCtx}
}

// WithScanAutomation configures optional automation before workers are started.
func (s *scanService) WithScanAutomation(watchSeconds, intervalMinutes int, signature func(context.Context) (string, error)) {
	s.watchSeconds, s.intervalMinutes, s.signature = watchSeconds, intervalMinutes, signature
}

// TriggerRescan replaces in-flight work with one manual scan, never overlapping it.
func (s *scanService) TriggerRescan(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.requestScan("manual")
}

// CancelScan clears queued work and requests cancellation of the current scan.
func (s *scanService) CancelScan(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = ""
	if s.active {
		s.state = "cancelling"
		s.scanCancel()
	}
	return nil
}

// ScanProgress returns a race-safe snapshot, including operator configuration.
func (s *scanService) ScanProgress(ctx context.Context) model.ScanProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p model.ScanProgress
	if s.progress != nil {
		p = s.progress.Copy()
	}
	p.State, p.Trigger, p.Pending = s.state, s.trigger, s.pending != ""
	p.WatchSeconds, p.IntervalMinutes, p.WatchError = s.watchSeconds, s.intervalMinutes, s.watchError
	return p
}

// StartScanAutomation starts at most one observer/scheduler on the app lifecycle.
func (s *scanService) StartScanAutomation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.automationStarted || s.stopped || (s.watchSeconds == 0 && s.intervalMinutes == 0) {
		return
	}
	s.automationStarted = true
	ctx, cancel := context.WithCancel(s.appCtx)
	s.autoCancel = cancel
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.automate(ctx) }()
}

// StopScans prevents new work, cancels workers, and joins them before DB close.
func (s *scanService) StopScans() {
	s.mu.Lock()
	s.stopped = true
	s.pending = ""
	if s.scanCancel != nil {
		s.scanCancel()
	}
	if s.autoCancel != nil {
		s.autoCancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *scanService) requestScan(trigger string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanner == nil {
		return errors.New("scanner not configured")
	}
	if s.stopped || s.appCtx.Err() != nil {
		return context.Canceled
	}
	if s.active {
		// Manual requests retain cancel/restart semantics. Other triggers share
		// one pending slot and cannot starve active work.
		if s.pending != "manual" {
			s.pending = trigger
		}
		if trigger == "manual" {
			s.state = "cancelling"
			s.scanCancel()
		}
		return nil
	}
	s.active = true
	ctx, p := s.beginLocked(trigger)
	s.wg.Add(1)
	go s.runScans(ctx, p)
	return nil
}

func (s *scanService) beginLocked(trigger string) (context.Context, *model.ScanProgress) {
	ctx, cancel := context.WithTimeout(s.appCtx, 30*time.Minute)
	s.scanCancel = cancel
	p := &model.ScanProgress{}
	p.Start(0)
	s.progress, s.trigger, s.state = p, trigger, "running"
	return ctx, p
}

func (s *scanService) runScans(ctx context.Context, p *model.ScanProgress) {
	defer s.wg.Done()
	for {
		err := s.runScan(ctx, p)
		p.Done(err)
		s.mu.Lock()
		s.scanCancel()
		s.state = "complete"
		if errors.Is(err, context.Canceled) {
			s.state = "cancelled"
		} else if err != nil {
			s.state = "failed"
		}
		if s.pending == "" || s.stopped || s.appCtx.Err() != nil {
			s.active = false
			s.scanCancel = nil
			s.mu.Unlock()
			s.notifyDone()
			return
		}
		trigger := s.pending
		s.pending = ""
		ctx, p = s.beginLocked(trigger)
		s.mu.Unlock()
		s.notifyDone()
	}
}

func (s *scanService) runScan(ctx context.Context, p *model.ScanProgress) (err error) {
	defer func() {
		if panicErr := RecoverWorker(s.logger, "rescan", recover()); panicErr != nil {
			err = panicErr
		}
	}()
	if s.trigger != "manual" {
		// Automatic inventory must not run the manual orphan/trash policy.
		inventory, ok := s.scanner.(interface {
			ScanInventory(context.Context, string, *model.ScanProgress) error
		})
		if !ok {
			return errors.New("automatic inventory scan not supported")
		}
		err = inventory.ScanInventory(ctx, s.mediaRoot, p)
	} else {
		err = s.scanner.Scan(ctx, s.mediaRoot, p)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		s.logger.Error("rescan failed", "err", err)
	}
	return err
}

func (s *scanService) automate(ctx context.Context) {
	// One goroutine observes sequentially. Tickers drop excess ticks, so
	// slow network roots cannot accumulate polling jobs.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	now := s.clock.Now()
	state := scanAutomationState{nextWatch: now, nextScan: now.Add(time.Duration(s.intervalMinutes) * time.Minute)}
	for {
		s.automationTick(ctx, s.clock.Now(), &state)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *scanService) automationTick(ctx context.Context, now time.Time, state *scanAutomationState) {
	if ctx.Err() != nil {
		return
	}
	if s.watchSeconds > 0 && s.signature != nil && !now.Before(state.nextWatch) {
		s.observe(ctx, &state.previous, &state.baseline)
		state.nextWatch = now.Add(time.Duration(s.watchSeconds) * time.Second)
	}
	if s.intervalMinutes > 0 && !now.Before(state.nextScan) {
		_ = s.requestScan("schedule")
		state.nextScan = now.Add(time.Duration(s.intervalMinutes) * time.Minute)
	}
}

func (s *scanService) observe(ctx context.Context, previous *string, baseline *bool) {
	value, err := s.inspectSources(ctx)
	s.mu.Lock()
	s.watchError = ""
	if err != nil && ctx.Err() == nil {
		s.watchError = "Could not inspect library files. Check root access, then refresh or rescan."
	}
	s.mu.Unlock()
	if err != nil {
		s.logger.Warn("scan watcher inspection failed", "err", err)
		return
	}
	// First observation establishes a baseline without changing startup behavior.
	changed := *baseline && value != *previous
	*previous, *baseline = value, true
	if changed {
		_ = s.requestScan("watch")
	}
}

func (s *scanService) inspectSources(ctx context.Context) (value string, err error) {
	defer func() {
		if panicErr := RecoverWorker(s.logger, "scan watcher", recover()); panicErr != nil {
			err = panicErr
		}
	}()
	return s.signature(ctx)
}

func (s *scanService) notifyDone() {
	if s.doneCh == nil {
		return
	}
	select {
	case s.doneCh <- struct{}{}:
	default:
	}
}
