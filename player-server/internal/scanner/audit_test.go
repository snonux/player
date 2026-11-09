package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
)

func auditScanner(t *testing.T, root string, probeFn func(context.Context, string) (*model.Metadata, error)) (*FSScanner, *repository.SQLite) {
	t.Helper()
	store, err := repository.Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := NewFSScanner(store, &probe.MockProber{ProbeFunc: probeFn}, &thumb.MockGenerator{}, clock.RealClock{}, root)
	s.workers = 1
	return s, store
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// B10: a file that vanishes while it is probed (beets moving it during an
// import) is skipped; the scan of everything else completes.
func TestScan_FileVanishingDuringProbeIsSkipped(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	gone := filepath.Join(root, "set", "gone.mp3")
	kept := filepath.Join(root, "set", "kept.mp3")
	other := filepath.Join(root, "other", "song.mp3")
	for _, p := range []string{gone, kept, other} {
		touch(t, p)
	}
	s, store := auditScanner(t, root, func(_ context.Context, path string) (*model.Metadata, error) {
		if path == gone {
			_ = os.Remove(path)
		}
		return &model.Metadata{}, nil
	})
	if err := s.Scan(ctx, root, nil); err != nil {
		t.Fatalf("scan aborted: %v", err)
	}
	rows, err := store.ListMedia(ctx, repository.MediaFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.AbsPath] = true
	}
	if got[gone] || !got[kept] || !got[other] {
		t.Fatalf("indexed %v", got)
	}
}

func activePaths(t *testing.T, store *repository.SQLite) map[string]bool {
	t.Helper()
	rows, err := store.ListMedia(context.Background(), repository.MediaFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		if r.DeletedAt == nil {
			got[r.RelPath] = true
		}
	}
	return got
}

// B11: rows of a set directory that was deleted or renamed are retired by
// a full scan, but an empty or unreadable root (a disconnected mount)
// retires nothing.
func TestScan_VanishedSetDirectory(t *testing.T) {
	ctx := context.Background()
	meta := func(context.Context, string) (*model.Metadata, error) { return &model.Metadata{}, nil }
	t.Run("deleted set", func(t *testing.T) {
		root := t.TempDir()
		touch(t, filepath.Join(root, "set", "a.mp4"))
		touch(t, filepath.Join(root, "keep", "b.mp4"))
		s, store := auditScanner(t, root, meta)
		if err := s.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(root, "set")); err != nil {
			t.Fatal(err)
		}
		touch(t, filepath.Join(root, "other", "c.mp4"))
		if err := s.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if got := activePaths(t, store); got["a.mp4"] || !got["b.mp4"] || !got["c.mp4"] {
			t.Fatalf("active rows %v", got)
		}
	})
	t.Run("inventory scan keeps rows", func(t *testing.T) {
		root := t.TempDir()
		touch(t, filepath.Join(root, "set", "a.mp4"))
		touch(t, filepath.Join(root, "keep", "b.mp4"))
		s, store := auditScanner(t, root, meta)
		if err := s.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(root, "set")); err != nil {
			t.Fatal(err)
		}
		if err := s.ScanInventory(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if got := activePaths(t, store); !got["a.mp4"] {
			t.Fatalf("inventory scan retired rows: %v", got)
		}
	})
	t.Run("empty root keeps rows", func(t *testing.T) {
		root := t.TempDir()
		touch(t, filepath.Join(root, "set", "a.mp4"))
		s, store := auditScanner(t, root, meta)
		if err := s.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(root, "set")); err != nil {
			t.Fatal(err)
		}
		if err := s.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if got := activePaths(t, store); !got["a.mp4"] {
			t.Fatalf("empty root retired rows: %v", got)
		}
	})
	t.Run("unreadable root keeps rows", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "mnt")
		touch(t, filepath.Join(root, "set", "a.mp4"))
		s, store := auditScanner(t, root, meta)
		if err := s.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		if err := s.Scan(ctx, root, nil); err == nil {
			t.Fatal("scan of a missing root succeeded")
		}
		if got := activePaths(t, store); !got["a.mp4"] {
			t.Fatalf("unreadable root retired rows: %v", got)
		}
	})
}
