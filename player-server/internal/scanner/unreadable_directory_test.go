package scanner

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// walkErrorFS injects the callback errors documented by filepath.WalkDir,
// while retaining real traversal and SkipDir handling, even when run as root.
type walkErrorFS struct {
	osFS
	path     string
	err      error
	nilEntry bool
}

func (f walkErrorFS) WalkDir(root string, fn fs.WalkDirFunc) error {
	return f.osFS.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if path == f.path {
			if f.nilEntry {
				entry = nil
			}
			return fn(path, entry, f.err)
		}
		return fn(path, entry, err)
	})
}

func TestScan_UnreadableDirectory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dir      string
		nilEntry bool
	}{
		{name: "nested directory", dir: "private"},
		{name: "entire set"},
		{name: "set stat permission error", nilEntry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMigrationFixture(t, []seededMedia{
				{rel: "private/deep/kept.mp4"},
				{rel: "private/trashed.mp3", deleted: true},
				{rel: "private/missing.mp3", noSource: true},
				{rel: "private-old/missing.mp3", noSource: true},
				{rel: "missing.mp3", noSource: true},
			})
			f.write(f.abs("sibling/new.mp3"), "new media")
			f.write(filepath.Join(f.root, "zz-later", "new.mp3"), "later set media")
			blocked := filepath.Join(f.setPath, tc.dir)
			scanner := f.scanner()
			scanner.fs = walkErrorFS{path: blocked, err: &os.PathError{Op: "open", Path: blocked, Err: fs.ErrPermission}, nilEntry: tc.nilEntry}
			var logs bytes.Buffer
			scanner.logger = slog.New(slog.NewTextHandler(&logs, nil))
			var progress model.ScanProgress
			if err := scanner.Scan(context.Background(), f.root, &progress); err != nil {
				t.Fatal(err)
			}
			if f.row("private/deep/kept.mp4").DeletedAt != nil || f.row("private/missing.mp3").DeletedAt != nil {
				t.Fatal("scan soft-deleted descendants of an unreadable directory")
			}
			if f.row("private/trashed.mp3").DeletedAt == nil {
				t.Fatal("scan restored a deliberately trashed row")
			}
			for _, rel := range []string{"private-old/missing.mp3", "missing.mp3"} {
				deleted := f.row(rel).DeletedAt != nil
				if deleted != (tc.dir != "") {
					t.Errorf("%s deleted = %v, want %v", rel, deleted, tc.dir != "")
				}
			}
			// Preserved rows must not be treated as discovered files and sent
			// through thumbnail generation in the unreadable subtree.
			if len(f.generated) != 0 {
				t.Errorf("generated thumbnails for inaccessible media: %v", f.generated)
			}
			all, err := f.store.ListMedia(context.Background(), repository.MediaFilter{})
			if err != nil {
				t.Fatal(err)
			}
			laterIndexed, siblingIndexed := false, false
			for _, m := range all {
				laterIndexed = laterIndexed || m.AbsPath == filepath.Join(f.root, "zz-later", "new.mp3")
				siblingIndexed = siblingIndexed || m.AbsPath == f.abs("sibling/new.mp3")
			}
			if !laterIndexed || siblingIndexed != (tc.dir != "") {
				t.Errorf("later indexed = %v, sibling indexed = %v", laterIndexed, siblingIndexed)
			}
			p := progress.Copy()
			wantFiles := 1
			if tc.dir != "" {
				wantFiles++
			}
			if p.SetsDone != 2 || p.FilesTotal != wantFiles || p.FilesDone != wantFiles {
				t.Errorf("unexpected progress: sets done = %d, files total = %d, files done = %d", p.SetsDone, p.FilesTotal, p.FilesDone)
			}
			if !strings.Contains(logs.String(), "scanner skipped unreadable directory") || !strings.Contains(logs.String(), blocked) || !strings.Contains(logs.String(), "permission denied") {
				t.Errorf("skip warning lacks path or error: %s", logs.String())
			}
			// Once traversal works again, a truly missing descendant can
			// be reconciled and newly accessible files are indexed normally.
			scanner.fs = osFS{}
			if err := scanner.Scan(context.Background(), f.root, nil); err != nil {
				t.Fatal(err)
			}
			if f.row("private/missing.mp3").DeletedAt == nil {
				t.Fatal("missing descendant survived a later complete scan")
			}
		})
	}
}

func TestDiscover_WalkErrorsRemainFatal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		file     bool
		err      error
		nilEntry bool
	}{
		{name: "file permission error", file: true, err: fs.ErrPermission},
		{name: "cancelled directory walk", err: context.Canceled},
		{name: "directory walk deadline", err: context.DeadlineExceeded},
		{name: "missing set root", err: fs.ErrNotExist, nilEntry: true},
		{name: "unknown set stat error", err: errors.New("I/O error"), nilEntry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := root
			if tc.file {
				path = filepath.Join(root, "track.mp3")
				if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			files, skipped, err := newFileDiscoverer(walkErrorFS{path: path, err: tc.err, nilEntry: tc.nilEntry}).Discover(root)
			if !errors.Is(err, tc.err) || len(files) != 0 || len(skipped) != 0 {
				t.Fatalf("fatal traversal error = %v, files = %v, skipped = %v", err, files, skipped)
			}
		})
	}
}

func TestDiscover_RealUnreadableDirectory(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "private")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o700); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.ReadDir(blocked); !errors.Is(err, fs.ErrPermission) {
		t.Skip("filesystem permits reading mode-000 directory (for example, root)")
	}
	_, skipped, err := newFileDiscoverer(osFS{}).Discover(root)
	if err != nil || !errors.Is(skipped[blocked], fs.ErrPermission) {
		t.Fatalf("real unreadable directory: skipped = %v, err = %v", skipped, err)
	}
}
