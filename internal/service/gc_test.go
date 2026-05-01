package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestGCWorker_RunOnce(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tmpDir := t.TempDir()

	// Create two files.
	oldFile := filepath.Join(tmpDir, "old.mp4")
	err := os.WriteFile(oldFile, []byte("old"), 0o644)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}
	newFile := filepath.Join(tmpDir, "new.mp4")
	err = os.WriteFile(newFile, []byte("new"), 0o644)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	deletedAtOld := now.Add(-8 * 24 * time.Hour)
	deletedAtNew := now.Add(-1 * 24 * time.Hour)

	var hardDeleted int64
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
				return []model.Media{
					{ID: 1, AbsPath: oldFile, DeletedAt: &deletedAtOld},
					{ID: 2, AbsPath: newFile, DeletedAt: &deletedAtNew},
				}, nil
			},
			HardDeleteMediaFunc: func(ctx context.Context, id int64) error {
				hardDeleted = id
				return nil
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(store, &clock.MockClock{T: now}, tmpDir, time.Minute, logger).WithAge(7 * 24 * time.Hour)
	w.RunOnce()

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("expected old file to be deleted")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatal("expected new file to still exist")
	}
	if hardDeleted != 1 {
		t.Fatalf("expected hard delete id=1, got %d", hardDeleted)
	}
}

func TestGCWorker_StartStop(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tmpDir := t.TempDir()

	deletedAt := now.Add(-8 * 24 * time.Hour)
	file := filepath.Join(tmpDir, "gone.mp4")
	os.WriteFile(file, []byte("data"), 0o644)

	var called bool
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
				called = true
				return []model.Media{
					{ID: 1, AbsPath: file, DeletedAt: &deletedAt},
				}, nil
			},
			HardDeleteMediaFunc: func(ctx context.Context, id int64) error {
				return nil
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(store, &clock.MockClock{T: now}, tmpDir, 10*time.Millisecond, logger).WithAge(7 * 24 * time.Hour)
	w.Start()
	time.Sleep(50 * time.Millisecond)
	w.Stop()

	if !called {
		t.Fatal("expected gc run to be called")
	}
}

func TestGCWorker_ListDeletedError(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
				return nil, errors.New("boom")
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(store, &clock.MockClock{T: now}, "/tmp", time.Minute, logger)
	w.RunOnce()
}

func TestGCWorker_StopBeforeStart(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(&repository.MockStore{}, &clock.MockClock{T: now}, "/tmp", time.Minute, logger)
	w.Stop() // must not panic
}

func TestGCWorker_DoubleStop(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(&repository.MockStore{}, &clock.MockClock{T: now}, "/tmp", time.Minute, logger)
	w.Start()
	w.Stop()
	w.Stop() // must not panic
}

func TestGCWorker_RelPathFallback(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tmpDir := t.TempDir()

	rel := "music/song.mp3"
	dir := filepath.Join(tmpDir, filepath.Dir(rel))
	os.MkdirAll(dir, 0o755)
	fpath := filepath.Join(tmpDir, rel)
	os.WriteFile(fpath, []byte("data"), 0o644)

	deletedAt := now.Add(-8 * 24 * time.Hour)
	var hardDeleted int64
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
				return []model.Media{
					{ID: 5, RelPath: rel, AbsPath: "", DeletedAt: &deletedAt},
				}, nil
			},
			HardDeleteMediaFunc: func(ctx context.Context, id int64) error {
				hardDeleted = id
				return nil
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(store, &clock.MockClock{T: now}, tmpDir, time.Minute, logger).WithAge(7 * 24 * time.Hour)
	w.RunOnce()

	if hardDeleted != 5 {
		t.Fatalf("expected hard delete id=5, got %d", hardDeleted)
	}
	if _, err := os.Stat(fpath); !os.IsNotExist(err) {
		t.Fatal("expected file to be deleted")
	}
}

func TestGCWorker_WithInterval(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(&repository.MockStore{}, &clock.MockClock{T: now}, "/tmp", time.Minute, logger).WithInterval(2 * time.Minute)
	if w.interval != 2*time.Minute {
		t.Fatalf("expected interval 2m, got %v", w.interval)
	}
}

func TestGCWorker_RunOnce_NotStarted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := NewGCWorker(&repository.MockStore{}, &clock.MockClock{}, "/tmp", 0, logger)
	err := w.RunOnce()
	if err == nil {
		t.Fatal("expected error when interval is 0")
	}
}
