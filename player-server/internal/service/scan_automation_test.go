package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
)

type inventoryScanner struct {
	started         chan string
	release         chan struct{}
	active, maximum atomic.Int32
	failNext        atomic.Bool
}

func (f *inventoryScanner) Scan(ctx context.Context, _ string, _ *model.ScanProgress) error {
	return f.run(ctx, "manual")
}

func (f *inventoryScanner) ScanInventory(ctx context.Context, _ string, _ *model.ScanProgress) error {
	return f.run(ctx, "inventory")
}

func (f *inventoryScanner) run(ctx context.Context, mode string) error {
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for old := f.maximum.Load(); n > old; old = f.maximum.Load() {
		if f.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	f.started <- mode
	if f.failNext.Swap(false) {
		return errors.New("library root unavailable")
	}
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func awaitScan(t *testing.T, started <-chan string, want string) {
	t.Helper()
	select {
	case mode := <-started:
		if mode != want {
			t.Fatalf("mode=%s want %s", mode, want)
		}
	case <-time.After(time.Second):
		t.Fatal("scan did not start")
	}
}

func TestScanAutomationCoalescesAndCancels(t *testing.T) {
	ctx := context.Background()
	f := &inventoryScanner{started: make(chan string, 4), release: make(chan struct{})}
	s := NewScanService(ctx, f, "/media", clock.RealClock{}, nil)
	defer s.StopScans()
	if err := s.TriggerRescan(ctx); err != nil {
		t.Fatal(err)
	}
	awaitScan(t, f.started, "manual")
	for i := 0; i < 100; i++ {
		if err := s.requestScan("watch"); err != nil {
			t.Fatal(err)
		}
	}
	if !s.ScanProgress(ctx).Pending {
		t.Fatal("no pending scan")
	}
	// Explicit manual restart wins over automatic work and waits for exit.
	if err := s.TriggerRescan(ctx); err != nil {
		t.Fatal(err)
	}
	awaitScan(t, f.started, "manual")
	if f.maximum.Load() != 1 {
		t.Fatal("overlapping scans")
	}
	if err := s.requestScan("schedule"); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelScan(ctx); err != nil {
		t.Fatal(err)
	}
	s.StopScans()
	p := s.ScanProgress(ctx)
	if p.Running || p.Pending || p.State != "cancelled" {
		t.Fatalf("bad cancelled status: running=%t pending=%t state=%s", p.Running, p.Pending, p.State)
	}
	if !errors.Is(s.TriggerRescan(ctx), context.Canceled) {
		t.Fatal("scan accepted after stop")
	}
}

func TestScanAutomationClockAndObservation(t *testing.T) {
	ctx := context.Background()
	f := &inventoryScanner{started: make(chan string, 4), release: make(chan struct{})}
	s := NewScanService(ctx, f, "/media", clock.RealClock{}, nil)
	defer s.StopScans()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	state := scanAutomationState{nextWatch: now, nextScan: now.Add(time.Minute)}
	sig := "a"
	var observeErr error
	s.WithScanAutomation(10, 1, func(context.Context) (string, error) { return sig, observeErr })
	s.automationTick(ctx, now, &state)
	if s.ScanProgress(ctx).Running {
		t.Fatal("baseline triggered a scan")
	}
	sig = "b"
	s.automationTick(ctx, now.Add(9*time.Second), &state)
	if s.ScanProgress(ctx).Running {
		t.Fatal("poll interval ignored")
	}
	s.automationTick(ctx, now.Add(10*time.Second), &state)
	awaitScan(t, f.started, "inventory")
	s.automationTick(ctx, now.Add(59*time.Second), &state)
	if s.ScanProgress(ctx).Pending {
		t.Fatal("unchanged signature retriggered scan")
	}
	s.automationTick(ctx, now.Add(time.Minute), &state)
	if !s.ScanProgress(ctx).Pending {
		t.Fatal("schedule did not queue inventory")
	}
	observeErr = errors.New("private path is unavailable")
	s.automationTick(ctx, now.Add(70*time.Second), &state)
	if s.ScanProgress(ctx).WatchError == "" {
		t.Fatal("inspection failure invisible")
	}
	observeErr = nil
	s.automationTick(ctx, now.Add(80*time.Second), &state)
	if s.ScanProgress(ctx).WatchError != "" {
		t.Fatal("inspection failure did not recover")
	}
	f.release <- struct{}{}
	awaitScan(t, f.started, "inventory")
	if f.maximum.Load() != 1 {
		t.Fatal("overlapping inventory scans")
	}
}

func TestStartupInventoryLifecycle(t *testing.T) {
	ctx := context.Background()
	f := &inventoryScanner{started: make(chan string, 4), release: make(chan struct{})}
	s := NewScanService(ctx, f, "/media", clock.RealClock{}, nil)
	defer s.StopScans()
	// Startup remains enabled when polling and scheduled scans are disabled.
	s.StartScanAutomation()
	awaitScan(t, f.started, "inventory")
	if p := s.ScanProgress(ctx); p.Trigger != "startup" || !p.Running {
		t.Fatalf("startup status: %+v", p.Copy())
	}
	s.StartScanAutomation()
	if s.ScanProgress(ctx).Pending {
		t.Fatal("second start queued duplicate startup")
	}
	if err := s.TriggerRescan(ctx); err != nil {
		t.Fatal(err)
	}
	awaitScan(t, f.started, "manual")
	if f.maximum.Load() != 1 {
		t.Fatal("startup overlapped manual scan")
	}
	s.StopScans()
	s.StartScanAutomation()
	if len(f.started) != 0 {
		t.Fatal("startup accepted after stop")
	}
}

func TestStartupObservationBoundaryAndCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "followup", true: "cancel"}[cancel], func(t *testing.T) {
			ctx := context.Background()
			f := &inventoryScanner{started: make(chan string, 4), release: make(chan struct{})}
			s := NewScanService(ctx, f, "/media", clock.RealClock{}, nil)
			defer s.StopScans()
			observing, observed := make(chan struct{}), make(chan struct{})
			s.WithScanAutomation(30, 0, func(ctx context.Context) (string, error) {
				close(observing)
				select {
				case <-observed:
					return "new source may have arrived after startup discovery", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			})
			s.StartScanAutomation()
			awaitScan(t, f.started, "inventory")
			<-observing
			if cancel {
				if err := s.CancelScan(ctx); err != nil {
					t.Fatal(err)
				}
			}
			close(observed)
			if !cancel {
				// Releasing startup lets either a queued or immediately started
				// follow-up complete the boundary inventory without overlap.
				f.release <- struct{}{}
				awaitScan(t, f.started, "inventory")
				if f.maximum.Load() != 1 {
					t.Fatal("overlapping startup follow-up")
				}
			} else {
				// Stop joins observation before checking that it did not undo cancel.
				s.StopScans()
				if p := s.ScanProgress(ctx); p.Pending || p.State != "cancelled" || len(f.started) != 0 {
					t.Fatalf("observation undid explicit cancel: %+v", p.Copy())
				}
			}
		})
	}
}

func TestStartupDoesNotQueueBehindManual(t *testing.T) {
	f := &inventoryScanner{started: make(chan string, 4), release: make(chan struct{})}
	s := NewScanService(context.Background(), f, "/media", clock.RealClock{}, nil)
	defer s.StopScans()
	if err := s.TriggerRescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitScan(t, f.started, "manual")
	s.StartScanAutomation()
	if s.ScanProgress(context.Background()).Pending {
		t.Fatal("startup queued behind manual")
	}
}

func TestStartupObservationRetriesAfterRootRecovery(t *testing.T) {
	ctx := context.Background()
	f := &inventoryScanner{started: make(chan string, 4), release: make(chan struct{})}
	f.failNext.Store(true)
	s := NewScanService(ctx, f, "/media", clock.RealClock{}, nil)
	defer s.StopScans()
	done := make(chan struct{}, 1)
	s.doneCh = done
	if err := s.requestScan("startup"); err != nil {
		t.Fatal(err)
	}
	awaitScan(t, f.started, "inventory")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("startup did not fail")
	}
	if s.ScanProgress(ctx).State != "failed" {
		t.Fatal("startup unexpectedly succeeded")
	}
	var previous string
	var baseline bool
	unavailable := true
	s.WithScanAutomation(30, 0, func(context.Context) (string, error) {
		if unavailable {
			return "", errors.New("library root unavailable")
		}
		return "restored library sources", nil
	})
	s.observe(ctx, &previous, &baseline)
	if baseline || s.ScanProgress(ctx).WatchError == "" {
		t.Fatal("unavailable root became watcher baseline")
	}
	unavailable = false
	s.observe(ctx, &previous, &baseline)
	awaitScan(t, f.started, "inventory")
	if p := s.ScanProgress(ctx); p.Trigger != "watch" || p.WatchError != "" {
		t.Fatalf("root recovery status: %+v", p.Copy())
	}
}

func TestCancelAfterStartupCompletesSuppressesInitialObservation(t *testing.T) {
	ctx := context.Background()
	f := &inventoryScanner{started: make(chan string, 4), release: make(chan struct{})}
	close(f.release)
	s := NewScanService(ctx, f, "/media", clock.RealClock{}, nil)
	defer s.StopScans()
	done := make(chan struct{}, 1)
	s.doneCh = done
	if err := s.requestScan("startup"); err != nil {
		t.Fatal(err)
	}
	awaitScan(t, f.started, "inventory")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("startup did not finish")
	}
	if err := s.CancelScan(ctx); err != nil {
		t.Fatal(err)
	}
	var previous string
	var baseline bool
	s.WithScanAutomation(30, 0, func(context.Context) (string, error) { return "initial sources", nil })
	s.observe(ctx, &previous, &baseline)
	s.StopScans()
	if len(f.started) != 0 {
		t.Fatal("initial watcher observation undid explicit cancel")
	}
}
