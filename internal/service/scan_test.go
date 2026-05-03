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
		name     string
		scanErr  error
		wantErr  bool
	}{
		{
			name: "ok",
		},
		{
			name:     "scan error",
			scanErr:  errors.New("boom"),
			wantErr:  false, // TriggerRescan returns nil immediately; background goroutine logs error
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
			svc := NewScanService(sc, "/media", clock.RealClock{}, nil)
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
