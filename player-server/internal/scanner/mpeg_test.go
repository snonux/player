package scanner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// Legacy program streams must be discovered, probed and thumbnailed without
// writing to the source library. Repeated scans must retain the media IDs.
func TestScan_RealMPEG(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	root, cache := t.TempDir(), t.TempDir()
	set := filepath.Join(root, "podcasts")
	originals := map[string][]byte{}
	for _, name := range []string{"clip.mpg", "clip.MPEG"} {
		path := filepath.Join(set, name)
		ffsafetest.Generate(t, path, "-f", "lavfi", "-i", "testsrc2=duration=2:size=64x48:rate=25",
			"-f", "lavfi", "-i", "sine=duration=2", "-c:v", "mpeg2video", "-c:a", "mp2", "-f", "mpeg")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		originals[path] = data
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	// Invalid files and playlist disguises must still be skipped.
	for name, data := range map[string]string{
		"broken.mpg":    "not media",
		"playlist.mpeg": "ffconcat version 1.0\nfile 'clip.mpg'\n",
		"._clip.mpg":    "AppleDouble sidecar",
	} {
		if err := os.WriteFile(filepath.Join(set, name), []byte(data), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(set, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(set, 0o755) })
	store, err := repository.Open(filepath.Join(t.TempDir(), "player.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	layout := thumb.NewLayout(root, cache)
	maker := thumb.NewFSMaker(thumb.NewFFmpegGenerator(), nil, nil).WithLayout(layout)
	scanner := NewFSScannerWithMaker(store, probe.NewFFProber(), maker, clock.RealClock{}, root, nil).WithThumbnailLayout(layout)
	ids := map[string]int64{}
	for scan := 1; scan <= 2; scan++ {
		if err := scanner.Scan(context.Background(), root, nil); err != nil {
			t.Fatal(err)
		}
		media, err := store.ListMedia(context.Background(), repository.MediaFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(media) != len(originals) {
			t.Fatalf("scan %d: indexed %d files, want %d", scan, len(media), len(originals))
		}
		for _, m := range media {
			if m.Type != model.MediaTypeVideo || m.Codec != "mpeg2video/mp2" || m.Duration <= 0 || !m.NeedsCompatStream() {
				t.Errorf("scan %d: incorrect metadata: %+v", scan, m)
			}
			if !layout.IsCachedFor(m.ThumbnailPath, m.AbsPath) {
				t.Errorf("thumbnail outside cache: %q", m.ThumbnailPath)
			}
			if info, err := os.Stat(m.ThumbnailPath); err != nil || info.Size() == 0 {
				t.Errorf("missing thumbnail: %v", err)
			}
			if scan == 2 && ids[m.AbsPath] != m.ID {
				t.Errorf("rescan changed media ID for %s", m.FileName)
			}
			ids[m.AbsPath] = m.ID
			data, err := os.ReadFile(m.AbsPath)
			if err != nil || !bytes.Equal(data, originals[m.AbsPath]) {
				t.Errorf("source changed: %s (%v)", m.FileName, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(set, ".thumbnails")); !os.IsNotExist(err) {
		t.Errorf("scanner wrote into source library: %v", err)
	}
}
