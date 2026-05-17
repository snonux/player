package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
)

func TestScanService_ScanLibrary(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		scanErr error
		wantErr bool
	}{
		{
			name: "ok",
		},
		{
			name:    "scan error",
			scanErr: errors.New("boom"),
			wantErr: false, // TriggerRescan returns nil immediately; background goroutine logs error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan struct{})
			sc := &fakeScanner{
				scanFunc: func(_ context.Context, root string, progress *model.ScanProgress) error {
					defer close(done)
					return tt.scanErr
				},
			}
			svc := NewScanService(ctx, sc, "/media", clock.RealClock{}, nil)
			err := svc.TriggerRescan(ctx)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Wait for background goroutine to finish.
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for scan goroutine")
			}
		})
	}
}

func TestScanService_TriggerRescanMarksProgressRunningImmediately(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{}, 1)
	sc := &fakeScanner{
		scanFunc: func(_ context.Context, _ string, _ *model.ScanProgress) error {
			close(started)
			<-release
			return nil
		},
	}
	svc := NewScanService(ctx, sc, "/media", clock.RealClock{}, nil)

	if err := svc.TriggerRescan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := svc.ScanProgress(ctx)
	if !p.Running {
		t.Fatal("expected progress to be running immediately after trigger")
	}
	if p.SetsTotal != 0 || p.FilesTotal != 0 {
		t.Fatalf("expected unknown initial totals, got sets=%d files=%d", p.SetsTotal, p.FilesTotal)
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for scan goroutine to start")
	}

	svc.doneCh = done
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for scan goroutine to finish")
	}
}

func TestScanService_CancelledByAppContext(t *testing.T) {
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	done := make(chan struct{}, 1)
	started := make(chan struct{})
	sc := &fakeScanner{
		scanFunc: func(scanCtx context.Context, _ string, progress *model.ScanProgress) error {
			close(started)
			<-scanCtx.Done()
			return scanCtx.Err()
		},
	}

	svc := NewScanService(appCtx, sc, "/media", clock.RealClock{}, nil)
	svc.doneCh = done
	if err := svc.TriggerRescan(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-started
	appCancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for scan goroutine to exit after app context cancellation")
	}

	p := svc.ScanProgress(context.Background())
	if p.Running {
		t.Fatal("expected scan to be stopped after app context cancellation")
	}
	if p.LastError == "" {
		t.Fatal("expected a last error after cancellation")
	}
}

func TestScanService_CancelledContextRecordedWhenScannerReturnsNil(t *testing.T) {
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	done := make(chan struct{}, 1)
	started := make(chan struct{})
	sc := &fakeScanner{
		scanFunc: func(scanCtx context.Context, _ string, progress *model.ScanProgress) error {
			progress.Start(1)
			close(started)
			<-scanCtx.Done()
			return nil
		},
	}

	svc := NewScanService(appCtx, sc, "/media", clock.RealClock{}, nil)
	svc.doneCh = done
	if err := svc.TriggerRescan(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-started
	appCancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for scan goroutine to exit after app context cancellation")
	}

	p := svc.ScanProgress(context.Background())
	if p.Running {
		t.Fatal("expected scan to be stopped after app context cancellation")
	}
	if p.LastError != context.Canceled.Error() {
		t.Fatalf("expected last error %q, got %q", context.Canceled.Error(), p.LastError)
	}
}
