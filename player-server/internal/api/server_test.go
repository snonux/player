package api

import (
	"log/slog"
	"testing"
)

// TestNewServerWithLogger_ErrorsOnNilConfig verifies that the constructor
// returns an error (rather than panicking) when deps.Config is nil. The
// caller in cmd/player/main.go relies on this to fail gracefully.
func TestNewServerWithLogger_ErrorsOnNilConfig(t *testing.T) {
	srv, err := NewServerWithLogger(ServerDeps{
		Config:   nil,
		StaticFS: nil,
	}, slog.Default())
	if err == nil {
		t.Fatal("expected error for nil Config, got nil")
	}
	if srv != nil {
		t.Fatalf("expected nil Server on error, got %v", srv)
	}
}
