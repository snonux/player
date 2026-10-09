package probe

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/ffsafe"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
)

// pngHead is enough of a PNG for ffsafe to recognise the format.
const pngHead = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"

// TestProbeArgs pins the ffprobe argument list. It needs no ffprobe, so it
// fails wherever the tests run if the input hardening is dropped.
func TestProbeArgs(t *testing.T) {
	image := filepath.Join(t.TempDir(), "a%03d.png")
	if err := os.WriteFile(image, []byte(pngHead), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/media/set/-a:b.avi", "/media/set/song.mp3", image} {
		input, err := ffsafe.SourceArgs(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := probeArgs(path)
		if err != nil {
			t.Fatalf("probeArgs(%q): %v", path, err)
		}
		want := append([]string{"-v", "error", "-show_format", "-show_streams", "-of", "json"}, input...)
		if !slices.Equal(got, want) {
			t.Errorf("probeArgs(%q) = %q, want %q", path, got, want)
		}
		ffsafetest.AssertHardened(t, got, input, path)
	}
}

// A file refused before ffprobe runs is not retried: the prober returns at
// once instead of sleeping through its retry delays.
func TestFFProber_RefusedImageIsNotRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evil.jpg")
	if err := os.WriteFile(path, []byte(ffsafetest.Playlists()["ffconcat"]), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &FFProber{maxRetries: 5, retryDelay: time.Hour, waitDelay: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Probe(ctx, path); !errors.Is(err, ffsafe.ErrNotAnImage) {
		t.Fatalf("Probe = %v, want ErrNotAnImage without waiting for a retry", err)
	}
}

// TestRemuxArgs pins the ffmpeg argument list of the remuxer; see
// TestProbeArgs.
func TestRemuxArgs(t *testing.T) {
	const path = "/media/set/-rec:1.mp4"
	input, err := ffsafe.InputArgs(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := remuxArgs(path)
	if err != nil {
		t.Fatalf("remuxArgs: %v", err)
	}
	want := slices.Concat(
		[]string{"-hide_banner", "-loglevel", "error", "-nostdin"},
		input,
		[]string{
			"-map", "0:v:0?", "-map", "0:a:0?", "-dn", "-sn", "-c", "copy", "-bsf:a", "aac_adtstoasc",
			"-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1",
		},
	)
	if !slices.Equal(got, want) {
		t.Errorf("remuxArgs = %q, want %q", got, want)
	}
	ffsafetest.AssertHardened(t, got, input, path)
}

// realProber is the production prober without retry delays.
func realProber() *FFProber {
	return &FFProber{maxRetries: 0, waitDelay: defaultProbeWaitDelay}
}

// A playlist disguised as media must not yield the metadata of the file it
// points at: the probe fails.
func TestFFProber_RealRefusesDisguisedPlaylists(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	secret := ffsafetest.Secret(t, dir)
	if meta, err := realProber().Probe(context.Background(), secret); err != nil || meta.Width != ffsafetest.SecretWidth {
		t.Fatalf("probing the secret file itself: %+v, %v", meta, err)
	}
	for _, path := range ffsafetest.WriteDisguised(t, dir) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			meta, err := realProber().Probe(context.Background(), path)
			if err == nil {
				t.Errorf("Probe succeeded with %+v, want a refusal", meta)
			}
		})
	}
}

// Names starting with "-" or containing ":" or "%" are probed as the files
// they are. The sibling a000.png (another size) is what an image sequence
// pattern would read instead of "a%03d.png".
func TestFFProber_RealHostileNames(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	ffsafetest.Generate(t, filepath.Join(dir, "a000.png"),
		"-f", "lavfi", "-i", "testsrc=s=32x24", "-frames:v", "1", "-update", "1")
	for _, name := range []string{"-dash.mp4", "co:lon.mp4", "a%03d.mp4", "-dash.png", "co:lon.jpg", "a%03d.png"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if filepath.Ext(name) == ".mp4" {
				ffsafetest.Video(t, path, "64x48")
			} else {
				ffsafetest.Still(t, path)
			}
			meta, err := realProber().Probe(context.Background(), path)
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if meta.Width != 64 || meta.Height != 48 {
				t.Errorf("size = %dx%d, want 64x48", meta.Width, meta.Height)
			}
		})
	}
}

// The remuxer does not follow a disguised playlist either: nothing of the
// secret file reaches the client.
func TestFFRemuxer_RealRefusesDisguisedPlaylists(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	secret := ffsafetest.Secret(t, dir)
	var good bytes.Buffer
	if err := NewFFRemuxer().Remux(context.Background(), secret, &good); err != nil || good.Len() == 0 {
		t.Fatalf("remuxing the secret file itself: %d bytes, %v", good.Len(), err)
	}
	for _, path := range ffsafetest.WriteDisguised(t, dir) {
		if filepath.Ext(path) == ".jpg" || filepath.Ext(path) == ".png" {
			continue // images are never remuxed
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			var out bytes.Buffer
			err := NewFFRemuxer().Remux(context.Background(), path, &out)
			if err == nil || out.Len() != 0 {
				t.Errorf("Remux wrote %d bytes, err %v; want a failure and no output", out.Len(), err)
			}
		})
	}
}
