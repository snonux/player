package scanner

import (
	"context"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

func TestScan_RealEmbeddedAudioArtwork(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	ctx := context.Background()
	root, cache := t.TempDir(), t.TempDir()
	album := filepath.Join(root, "Music", "album")
	track := filepath.Join(album, "-song %03d.flac")
	plain := filepath.Join(root, "Music", "plain", "plain.flac")
	ffsafetest.Audio(t, track, true)
	newTrack := filepath.Join(root, "Music", "new", "new.flac")
	if err := os.MkdirAll(filepath.Dir(newTrack), 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(track)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newTrack, content, 0o644); err != nil {
		t.Fatal(err)
	}
	ffsafetest.Audio(t, plain, false)
	store, err := repository.Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	setID, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// Existing rows model an upgrade: there was no thumbnail for embedded art.
	trackID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "album/-song %03d.flac", FileName: filepath.Base(track), AbsPath: track, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := map[string]int{}
	gen := thumb.NewFFmpegGenerator()
	counted := &thumb.MockGenerator{GenerateFunc: func(ctx context.Context, input, output string, duration float64) error {
		mu.Lock()
		calls[input]++
		mu.Unlock()
		if input == plain {
			t.Error("audio without art must not invoke ffmpeg")
		}
		if input == track && duration != 0 {
			t.Errorf("embedded still was given duration %v", duration)
		}
		return gen.Generate(ctx, input, output, duration)
	}}
	layout := thumb.NewLayout(root, cache)
	scanner := NewFSScannerWithMaker(store, probe.NewFFProber(), thumb.NewFSMaker(counted, nil, nil).WithLayout(layout), clock.RealClock{}, root, nil).WithThumbnailLayout(layout)
	check := func(want string) {
		t.Helper()
		if err := scanner.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		row, err := store.GetMediaByID(ctx, trackID)
		if err != nil || row == nil || row.ThumbnailPath != want {
			t.Fatalf("track = %+v, %v; want %q", row, err, want)
		}
		media, err := store.ListMedia(ctx, repository.MediaFilter{})
		if err != nil {
			t.Fatal(err)
		}
		seenNew, seenPlain := false, false
		for _, m := range media {
			if m.AbsPath == newTrack {
				seenNew = true
				expected, _ := layout.PathFor(newTrack)
				if m.ThumbnailPath != expected {
					t.Fatalf("new embedded track has thumbnail %q, want %q", m.ThumbnailPath, expected)
				}
			}
			if m.AbsPath == plain {
				seenPlain = true
			}
			if m.AbsPath == plain && m.ThumbnailPath != "" {
				t.Fatalf("no-art track has thumbnail %q", m.ThumbnailPath)
			}
		}
		if !seenNew || !seenPlain {
			t.Fatal("new audio rows were not indexed")
		}
	}
	want, _ := layout.PathFor(track)
	check(want)
	check(want)
	if calls[track] != 1 {
		t.Fatalf("generated %d times after two scans", calls[track])
	}
	f, err := os.Open(want)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := img.At(10, 10).RGBA()
	if r < 50000 || b > 5000 {
		t.Fatalf("thumbnail is not the red front picture: red=%d blue=%d", r, b)
	}
	if img.Bounds().Dx() != 320 {
		t.Fatalf("thumbnail width = %d", img.Bounds().Dx())
	}
	if err := os.Remove(want); err != nil {
		t.Fatal(err)
	}
	check(want)
	if calls[track] != 2 {
		t.Fatal("missing generated artwork was not repaired")
	}
	cover := filepath.Join(album, "cover.jpg")
	ffsafetest.Still(t, cover)
	check(cover)
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("sidecar adoption left unused generated artwork: %v", err)
	}
	if _, err := os.Stat(cover); err != nil {
		t.Fatalf("sidecar was deleted: %v", err)
	}
	if calls[track] != 2 {
		t.Fatal("sidecar cover did not suppress extraction")
	}
	if err := os.Remove(cover); err != nil {
		t.Fatal(err)
	}
	check(want)
	if calls[track] != 3 {
		t.Fatal("removed sidecar did not restore embedded artwork")
	}
	if _, err := os.Stat(thumb.ThumbnailPathFor(track)); !os.IsNotExist(err) {
		t.Fatalf("wrote beside source despite cache: %v", err)
	}
	// A transient Stat failure is not evidence that custom art disappeared.
	// Exercise the whole scan: the earlier refresh phase must preserve it too.
	custom := filepath.Join(t.TempDir(), "custom.jpg")
	artwork, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(custom, artwork, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateMediaThumbnail(ctx, trackID, custom); err != nil {
		t.Fatal(err)
	}
	scanner.fs = inaccessibleArtworkFS{FS: osFS{}, artwork: custom}
	check(custom)
	if calls[track] != 3 {
		t.Fatal("inaccessible custom artwork was replaced with embedded art")
	}
}

type inaccessibleArtworkFS struct {
	FS
	artwork string
}

func (f inaccessibleArtworkFS) Stat(path string) (os.FileInfo, error) {
	if path == f.artwork {
		return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
	}
	return f.FS.Stat(path)
}
