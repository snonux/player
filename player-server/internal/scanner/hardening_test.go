package scanner

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// TestScan_RealDisguisedPlaylists scans, with the real ffprobe and ffmpeg, a
// set into which playlists disguised as media were dropped next to genuine
// files. The playlists point at private/s.mp4, standing for media the
// uploader may not see.
//
// The disguised files must be skipped — no row, so nothing carries the
// other file's codec, size or duration, and no thumbnail showing one of its
// frames — while the scan itself succeeds and indexes everything else,
// including files with hostile names. A second scan (which probes the
// skipped files again, as it does for any unreadable file) ends the same.
func TestScan_RealDisguisedPlaylists(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	ctx := context.Background()
	root := t.TempDir()
	set := filepath.Join(root, "uploads")
	ffsafetest.Secret(t, set)
	ffsafetest.Video(t, filepath.Join(set, "good.mp4"), "64x48")
	ffsafetest.Video(t, filepath.Join(set, "-we:ird %03d.mp4"), "64x48")
	ffsafetest.Still(t, filepath.Join(set, "good.png"))
	ffsafetest.Still(t, filepath.Join(set, "a%03d.jpg"))
	disguised := ffsafetest.WriteDisguised(t, set)

	store, err := repository.Open(filepath.Join(t.TempDir(), "player.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := NewFSScanner(store, probe.NewFFProber(), thumb.NewFFmpegGenerator(), clock.RealClock{}, root)
	scanner.workers = 4

	want := []string{"-we:ird %03d.mp4", "a%03d.jpg", "good.mp4", "good.png", "private/s.mp4"}
	for scan := 1; scan <= 2; scan++ {
		if err := scanner.Scan(ctx, root, nil); err != nil {
			t.Fatalf("scan %d: %v", scan, err)
		}
		media, err := store.ListMedia(ctx, repository.MediaFilter{IncludeDeleted: true})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, m := range media {
			got = append(got, m.RelPath)
			if m.RelPath != "private/s.mp4" && m.Width != 64 {
				t.Errorf("scan %d: %s indexed with width %d, want its own 64", scan, m.RelPath, m.Width)
			}
			if m.ThumbnailPath == "" {
				t.Errorf("scan %d: genuine file %s got no thumbnail", scan, m.RelPath)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("scan %d: indexed %q, want %q", scan, got, want)
		}
		for _, path := range disguised {
			if _, err := os.Stat(thumb.ThumbnailPathFor(path)); !os.IsNotExist(err) {
				t.Errorf("scan %d: thumbnail exists for %s (stat: %v)", scan, filepath.Base(path), err)
			}
		}
		assertNoTemporaries(t, filepath.Join(set, thumb.DirName))
	}
}

// sampleLibraryEnv names a directory written by testdata/gen-all-formats.sh:
// one sample per supported extension.
const sampleLibraryEnv = "PLAYER_SAMPLE_LIBRARY"

// TestScan_RealSampleLibrary is the no-regression check for the input
// hardening: with the real ffprobe and ffmpeg, a file of every supported
// extension is still indexed with a codec, and every video and raster image
// still gets a generated thumbnail. It runs only when PLAYER_SAMPLE_LIBRARY
// points at a generated sample library (about 10 MB, not committed):
//
//	testdata/gen-all-formats.sh /tmp/lib
//	PLAYER_SAMPLE_LIBRARY=/tmp/lib go test ./internal/scanner -run SampleLibrary
func TestScan_RealSampleLibrary(t *testing.T) {
	library := os.Getenv(sampleLibraryEnv)
	if library == "" {
		t.Skip(sampleLibraryEnv + " not set")
	}
	ffsafetest.RequireFFmpeg(t)
	ctx := context.Background()
	// Scan a copy: the scan writes thumbnails next to the files.
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(library)); err != nil {
		t.Fatal(err)
	}
	// Listed before the scan adds its .thumbnails directories.
	samples, err := filepath.Glob(filepath.Join(root, "*", "*.*"))
	if err != nil || len(samples) == 0 {
		t.Fatalf("no samples in %s (%v)", library, err)
	}
	store, err := repository.Open(filepath.Join(t.TempDir(), "player.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := NewFSScanner(store, probe.NewFFProber(), thumb.NewFFmpegGenerator(), clock.RealClock{}, root)
	if err := scanner.Scan(ctx, root, nil); err != nil {
		t.Fatalf("scan: %v", err)
	}
	media, err := store.ListMedia(ctx, repository.MediaFilter{})
	if err != nil {
		t.Fatal(err)
	}
	indexed := map[string]bool{}
	for _, m := range media {
		indexed[m.AbsPath] = true
		if m.Codec == "" {
			t.Errorf("%s: indexed without a codec", m.RelPath)
		}
		wantsThumbnail := m.Type == model.MediaTypeVideo ||
			(m.Type == model.MediaTypeImage && !strings.EqualFold(filepath.Ext(m.RelPath), ".svg"))
		if wantsThumbnail && !thumb.IsGenerated(m.ThumbnailPath) {
			t.Errorf("%s: no generated thumbnail (thumbnail path %q)", m.RelPath, m.ThumbnailPath)
		}
	}
	for _, sample := range samples {
		if !indexed[sample] {
			t.Errorf("%s was not indexed", sample)
		}
	}
	t.Logf("%d samples indexed", len(samples))
}

// assertNoTemporaries fails when the thumbnail directory still holds a
// temporary file of a failed generation.
func assertNoTemporaries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temporary thumbnail %s left behind", e.Name())
		}
	}
}
