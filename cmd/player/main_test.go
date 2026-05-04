package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return strings.TrimSpace(buf.String())
}

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
	out := captureStdout(func() {
		err := run([]string{"-version"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
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

func TestRunWithSignal_NormalShutdown(t *testing.T) {
	if os.Getenv("GO_TEST_IN_CONTAINER") == "no_ffprobe" {
		t.Skip("ffprobe not available in this environment")
	}

	tmpDir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(tmpDir, "test.db"))
	t.Setenv("MEDIA_ROOT", filepath.Join(tmpDir, "media"))
	t.Setenv("PORT", "0")

	// Build a channel we can use instead of real OS signals.
	sigCh := make(chan os.Signal, 1)

	// Run the server in a goroutine; it will block on <-sigCh.
	errCh := make(chan error, 1)
	go func() {
		errCh <- runWithSignal([]string{}, sigCh)
	}()

	// Give the server a moment to start listening.
	time.Sleep(500 * time.Millisecond)

	// Send a synthetic signal to trigger shutdown.
	sigCh <- syscall.SIGINT

	// Wait for graceful shutdown.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for server shutdown")
	}
}

func TestRunWithSignal_LogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "invalid"} {
		t.Run(level, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("DB_PATH", filepath.Join(tmpDir, "test.db"))
			t.Setenv("MEDIA_ROOT", filepath.Join(tmpDir, "media"))
			t.Setenv("PORT", "0")
			t.Setenv("LOG_LEVEL", level)

			sigCh := make(chan os.Signal, 1)
			errCh := make(chan error, 1)
			go func() {
				errCh <- runWithSignal([]string{}, sigCh)
			}()
			time.Sleep(200 * time.Millisecond)
			sigCh <- syscall.SIGTERM

			select {
			case err := <-errCh:
				if level == "invalid" {
					if err == nil {
						t.Fatal("expected error for invalid LOG_LEVEL")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for server shutdown")
			}
		})
	}
}

func TestRunWithSignal_InvalidDB(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(tmpDir, "readonly"))
	if err := os.MkdirAll(filepath.Join(tmpDir, "readonly"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIA_ROOT", filepath.Join(tmpDir, "media"))
	t.Setenv("PORT", "0")

	err := runWithSignal([]string{}, nil)
	if err == nil {
		t.Fatal("expected error for invalid DB_PATH")
	}
}

func TestRunWithSignal_ServerErrorPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(tmpDir, "test.db"))
	t.Setenv("MEDIA_ROOT", filepath.Join(tmpDir, "media"))
	// Port 1 is privileged and should fail on non-root Linux.
	t.Setenv("PORT", "1")

	// No signal channel; we expect the server start to fail quickly.
	err := runWithSignal([]string{}, nil)
	if err == nil {
		t.Fatal("expected error when server cannot bind privileged port")
	}
}
