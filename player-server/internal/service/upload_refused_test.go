package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
)

// TestUploadMedia_RefusedFileLeavesNothing uploads, through the real prober
// and a real SQLite store, a playlist disguised as an image. Its content is
// refused before ffprobe is started, so the test needs no ffmpeg.
//
// The upload must fail with the bare ErrUnreadableMedia — the error text is
// what the client sees, and it must not carry the server's path — and leave
// neither the file nor a media row behind.
func TestUploadMedia_RefusedFileLeavesNothing(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clk := newMockClock()
	adminID, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "hash", IsAdmin: true, CreatedAt: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	setID, err := store.CreateSet(ctx, &model.Set{Name: "uploads", RootPath: "uploads", CreatedAt: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	mediaRoot := t.TempDir()
	svc := NewMediaService(store, clk, mediaRoot, nil, probe.NewFFProber())

	const playlist = "ffconcat version 1.0\nfile 'private/s.mp4'\n"
	media, err := svc.UploadMedia(ctx, setID, adminID, "evil.jpg", strings.NewReader(playlist), int64(len(playlist)))
	if err != ErrUnreadableMedia || media != nil {
		t.Fatalf("UploadMedia = %+v, %v; want the bare ErrUnreadableMedia", media, err)
	}
	if strings.Contains(err.Error(), mediaRoot) {
		t.Errorf("error text %q names the server path", err)
	}

	entries, err := os.ReadDir(filepath.Join(mediaRoot, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("%s was left in the set directory", e.Name())
	}
	rows, err := store.ListMedia(ctx, repository.MediaFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("%d media rows left behind: %+v", len(rows), rows)
	}
}

// probeError keeps the detail of a refusal out of the returned error and
// leaves other failures recognisable.
func TestProbeError(t *testing.T) {
	refused := errors.Join(probe.ErrUnreadable, errors.New("ffprobe /media/set/evil.avi: Format not on whitelist"))
	if got := probeError("/media/set/evil.avi", refused); got != ErrUnreadableMedia {
		t.Errorf("refusal = %v, want the bare ErrUnreadableMedia", got)
	}
	broken := errors.New("exec: ffprobe not found")
	if got := probeError("/media/set/a.avi", broken); !errors.Is(got, broken) || errors.Is(got, ErrUnreadableMedia) {
		t.Errorf("other failure = %v, want it wrapped as it is", got)
	}
}
