package api

import (
	"log/slog"
	"testing"
)

func TestNewServerWithLogger_PanicsOnNilConfig(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil Config, got none")
		}
	}()

	NewServerWithLogger(ServerDeps{
		Config:   nil,
		StaticFS: nil,
	}, slog.Default())
}
