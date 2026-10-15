package app

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/scanner"
	"codeberg.org/snonux/player/internal/thumb"
)

// Shared Beets artwork goes through the ordinary authenticated thumbnail route,
// with the track's set permissions. Only media probing/generation is faked.
func TestBeetsArtThumbnailAPI(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	root := e.deps.Cfg.MediaRoot
	album := filepath.Join(root, "set1", "album")
	track := filepath.Join(album, "track.mp3")
	cover := filepath.Join(album, "cover.jpg")
	e.must(os.MkdirAll(album, 0o755))
	e.must(os.WriteFile(track, []byte("audio"), 0o644))
	// Use a real image for ServeContent's MIME detection; its file name is the
	// Beets contract, and no re-encoding is needed to serve these shared bytes.
	writePNG(t, cover)
	original, err := os.ReadFile(cover)
	e.must(err)
	set, err := e.store.GetMediaByID(ctx, e.media["clip.avi"])
	e.must(err)
	id, err := e.store.CreateMedia(ctx, &model.Media{SetID: set.SetID, RelPath: "album/track.mp3", AbsPath: track, FileName: "track.mp3", Type: model.MediaTypeAudio, ThumbnailPath: filepath.Join(album, "missing.jpg"), CreatedAt: time.Now()})
	e.must(err)
	for _, dir := range []string{album, filepath.Dir(album)} {
		e.must(os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}
	s := scanner.NewFSScanner(e.store, &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) { return &model.Metadata{}, nil }}, &thumb.MockGenerator{}, clock.RealClock{}, root)
	e.must(s.Scan(ctx, root, nil))
	if got := e.mediaByPath(track).ThumbnailPath; got != cover {
		t.Fatalf("thumbnail=%q, want %q", got, cover)
	}
	for _, prefix := range []string{"/api/", "/api/v1/"} {
		path := prefix + "media/" + strconv.FormatInt(id, 10) + "/thumbnail"
		for _, tc := range []struct {
			user   string
			status int
		}{{"alice", http.StatusOK}, {"admin", http.StatusOK}, {"bob", http.StatusForbidden}, {"", http.StatusUnauthorized}} {
			rr := e.get(path, tc.user)
			if rr.Code != tc.status {
				t.Fatalf("GET %s as %q=%d, want %d: %s", path, tc.user, rr.Code, tc.status, rr.Body)
			}
			if tc.status == http.StatusOK && !bytes.Equal(rr.Body.Bytes(), original) {
				t.Fatal("thumbnail differs from shared artwork")
			}
		}
	}
	current, err := os.ReadFile(cover)
	e.must(err)
	if !bytes.Equal(original, current) {
		t.Fatal("shared artwork was changed")
	}
}
