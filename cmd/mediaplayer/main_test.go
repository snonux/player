package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/play/internal"
	"codeberg.org/snonux/play/internal/clock"
	"codeberg.org/snonux/play/internal/repository"
	"codeberg.org/snonux/play/internal/service"
)

// TestGCWorkerWiring verifies that the GC worker can be constructed with the
// same dependencies used in main, started, and stopped cleanly against a real
// SQLite store. This is an integration-friendly smoke test for the wiring.
func TestGCWorkerWiring(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	mediaRoot := filepath.Join(tmpDir, "media")
	if err := os.MkdirAll(mediaRoot, 0o755); err != nil {
		t.Fatalf("mkdir media root: %v", err)
	}

	store, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Logf("close db: %v", err)
		}
	}()

	cfg := &internal.Config{
		GCIntervalMinutes: 1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	clk := clock.RealClock{}

	w := service.NewGCWorker(store, clk, mediaRoot, time.Duration(cfg.GCIntervalMinutes)*time.Minute, logger)
	w.Start()
	w.Stop() // must not panic even after interacting with real store
}

func TestRun_VersionFlag(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := run([]string{"-version"})
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())
	if out != internal.Version {
		t.Fatalf("expected %q, got %q", internal.Version, out)
	}
}

func TestRun_InvalidFlag(t *testing.T) {
	err := run([]string{"-invalidflag"})
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
}

func TestRun_InvalidConfig(t *testing.T) {
	t.Setenv("PORT", "invalid")
	err := run([]string{})
	if err == nil {
		t.Fatal("expected error for invalid PORT")
	}
}
