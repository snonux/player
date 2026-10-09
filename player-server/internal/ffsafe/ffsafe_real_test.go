package ffsafe

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
)

// These tests run the real ffprobe with the arguments built here. They skip
// where ffmpeg is missing, or fail with PLAYER_REQUIRE_FFMPEG=1.

// probeWidth runs ffprobe with the given input arguments and returns the
// width of the first video stream it reports, 0 when it reports none.
func probeWidth(t *testing.T, input ...string) (int, error) {
	t.Helper()
	args := append([]string{"-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width", "-of", "csv=p=0"}, input...)
	out, err := exec.Command("ffprobe", args...).Output()
	// The stream may be listed more than once, behind other lines (MPEG-TS
	// lists it per program as well): take the first number.
	for _, field := range strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' || r == ',' }) {
		if width, convErr := strconv.Atoi(strings.TrimSpace(field)); convErr == nil {
			return width, err
		}
	}
	return 0, err
}

// sourceWidth probes path the hardened way.
func sourceWidth(t *testing.T, path string) (int, error) {
	t.Helper()
	input, err := SourceArgs(path)
	if err != nil {
		return 0, err
	}
	return probeWidth(t, input...)
}

// The control: opened by its bare name, a disguised playlist really does
// make ffprobe report the secret file. Without this the refusal tests below
// could pass on fixtures that attack nothing.
//
// Only ffconcat is checked. ffmpeg 6.1 and 8.1 themselves decline an HLS
// playlist whose name does not end in .m3u8 ("Not detecting m3u8/hls with
// non standard extension"), so the HLS fixtures are refused with or without
// the hardening; they are kept so that a build without that check is
// covered too.
func TestReal_BareNameFollowsPlaylists(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	ffsafetest.Secret(t, dir)
	for _, path := range ffsafetest.WriteDisguised(t, dir) {
		if !strings.HasPrefix(filepath.Base(path), "ffconcat") {
			continue
		}
		width, err := probeWidth(t, path)
		if err != nil || width != ffsafetest.SecretWidth {
			t.Errorf("%s: bare ffprobe = width %d, %v; the fixture no longer reaches the secret file",
				filepath.Base(path), width, err)
		}
	}
}

func TestReal_DisguisedPlaylistsAreRefused(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	ffsafetest.Secret(t, dir)
	for _, path := range ffsafetest.WriteDisguised(t, dir) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			width, err := sourceWidth(t, path)
			if err == nil || width != 0 {
				t.Errorf("hardened ffprobe = width %d, %v; want a failure reporting nothing", width, err)
			}
		})
	}
}

// Names that look like options, protocols or sequence patterns are read
// literally. The sibling a000.png is what image2 would read for the pattern
// "a%03d.png"; it has another size, so reading it would show.
func TestReal_HostileNames(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	ffsafetest.Generate(t, filepath.Join(dir, "a000.png"),
		"-f", "lavfi", "-i", "testsrc=s=32x24", "-frames:v", "1", "-update", "1")
	for _, name := range []string{"-dash.mp4", "co:lon.mp4", "a%03d.mp4", "-dash.png", "co:lon.png", "a%03d.png", "a%03d.jpg"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if filepath.Ext(name) == ".mp4" {
				ffsafetest.Video(t, path, "64x48")
			} else {
				ffsafetest.Still(t, path)
			}
			if width, err := sourceWidth(t, path); err != nil || width != 64 {
				t.Errorf("width = %d, %v; want 64", width, err)
			}
		})
	}
}

// Every raster format ffmpeg can write here is read back through its forced
// demuxer, also under a wrong extension.
func TestReal_ImageFormats(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	for _, ext := range []string{".jpg", ".png", ".gif", ".bmp", ".webp"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "still"+ext)
			ffsafetest.Still(t, path)
			if width, err := sourceWidth(t, path); err != nil || width != 64 {
				t.Errorf("width = %d, %v; want 64", width, err)
			}
			mislabeled := filepath.Join(t.TempDir(), "mislabeled.jpeg")
			copyFile(t, path, mislabeled)
			if width, err := sourceWidth(t, mislabeled); err != nil || width != 64 {
				t.Errorf("as .jpeg: width = %d, %v; want 64", width, err)
			}
		})
	}
}

// A real container under the wrong media extension still works; an image
// under a video extension does not (no image demuxer is on that list).
func TestReal_MislabeledMedia(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "v.mp4")
	ffsafetest.Video(t, video, "64x48")
	asFLV := filepath.Join(dir, "v.flv")
	copyFile(t, video, asFLV)
	if width, err := sourceWidth(t, asFLV); err != nil || width != 64 {
		t.Errorf("MP4 named .flv: width = %d, %v; want 64", width, err)
	}
	// An MPEG-TS recording named .mp4 is what the remuxer exists for.
	tsAsMP4 := filepath.Join(dir, "recording.mp4")
	ffsafetest.Generate(t, tsAsMP4, "-f", "lavfi", "-i", "testsrc=d=3:s=64x48:r=5", "-pix_fmt", "yuv420p", "-f", "mpegts")
	if width, err := sourceWidth(t, tsAsMP4); err != nil || width != 64 {
		t.Errorf("MPEG-TS named .mp4: width = %d, %v; want 64", width, err)
	}
	still := filepath.Join(dir, "s.png")
	ffsafetest.Still(t, still)
	asAVI := filepath.Join(dir, "s.avi")
	copyFile(t, still, asAVI)
	if width, err := sourceWidth(t, asAVI); err == nil || width != 0 {
		t.Errorf("PNG named .avi: width = %d, %v; want a refusal", width, err)
	}
	asJPG := filepath.Join(dir, "garbage.jpg")
	writeFile(t, asJPG, []byte("neither image nor media"))
	if _, err := sourceWidth(t, asJPG); !errors.Is(err, ErrNotAnImage) {
		t.Errorf("garbage named .jpg: %v, want ErrNotAnImage", err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dst, content)
}
