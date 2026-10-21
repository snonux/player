package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

func TestScan_MusicRefreshPreservesIdentityAndState(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "Music", "unrelated-name.flac")
	write := func(title string) {
		t.Helper()
		args := []string{"-f", "lavfi", "-i", "sine=duration=1", "-c:a", "flac"}
		if title != "" {
			args = append(args, "-metadata", "title="+title, "-metadata", "artist=Ünïcødé", "-metadata", "disc=2/3")
		}
		ffsafetest.Generate(t, path, args...)
	}
	write("First Title")
	initialFile, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := repository.Open(filepath.Join(t.TempDir(), "music.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now()
	sid, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	// Upgrade: an old row already has progress/counts but no music object.
	id, err := store.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: filepath.Base(path), FileName: filepath.Base(path), AbsPath: path, Type: model.MediaTypeAudio, FileSizeBytes: initialFile.Size(), PlayCount: 7, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	uid, err := store.CreateUser(ctx, &model.User{Username: "listener", PasswordHash: "hash", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: uid, MediaID: id, PositionSeconds: 0.3, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	scanner := NewFSScanner(store, probe.NewFFProber(), &thumb.MockGenerator{}, clock.RealClock{}, root)
	for _, title := range []string{"First Title", "Updated 東京", strings.Repeat("Long metadata 東京 ", 1000) + "End", ""} {
		write(title)
		if err := scanner.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		if err := scanner.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		rows, err := store.ListMedia(ctx, repository.MediaFilter{})
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows=%v %v", rows, err)
		}
		row := rows[0]
		sourceFile, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if row.FileSizeBytes != sourceFile.Size() {
			t.Fatalf("indexed source size=%d, actual=%d after retagging title length=%d", row.FileSizeBytes, sourceFile.Size(), len(title))
		}
		if row.ID != id || row.FileName != filepath.Base(path) || row.PlayCount != 7 || row.Music == nil || row.Music.Title != title {
			t.Fatalf("row=%+v music=%+v", row, row.Music)
		}
		progress, err := store.GetProgress(ctx, uid, id)
		if err != nil || progress == nil || progress.PositionSeconds != 0.3 {
			t.Fatalf("progress=%+v %v", progress, err)
		}
		if title != "" {
			matches, err := store.ListMedia(ctx, repository.MediaFilter{Search: title})
			if err != nil || len(matches) != 1 {
				t.Fatalf("title search rows=%v %v", matches, err)
			}
		}
	}
}
