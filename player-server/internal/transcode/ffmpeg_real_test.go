package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
)

// requireFFmpegEnv makes the real-ffmpeg tests mandatory. They skip when the
// tools are missing so a developer machine without ffmpeg still passes, but a
// skip is easy to overlook: CI sets this variable (see the server-transcode
// step in .woodpecker/server.yml) and then a missing tool fails the run.
const requireFFmpegEnv = "PLAYER_REQUIRE_FFMPEG"

// requireFFmpeg skips (or, with PLAYER_REQUIRE_FFMPEG=1, fails) the test
// unless ffmpeg, ffprobe and the libx264 encoder are available.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	missing := ""
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			missing = bin + " not installed"
		}
	}
	if missing == "" {
		out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
		if err != nil || !strings.Contains(string(out), "libx264") {
			missing = "ffmpeg lacks the libx264 encoder"
		}
	}
	if missing == "" {
		return
	}
	if os.Getenv(requireFFmpegEnv) == "1" {
		t.Fatalf("%s=1 but %s", requireFFmpegEnv, missing)
	}
	t.Skip(missing)
}

// makeSample generates a short media file with ffmpeg's synthetic sources.
// Sample generation depends on optional encoders of the local ffmpeg build,
// so a build that cannot create a sample skips that case.
func makeSample(t *testing.T, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	if out, err := exec.Command("ffmpeg", append(full, "file:"+path)...).CombinedOutput(); err != nil {
		t.Skipf("cannot generate %s sample: %v: %s", name, err, out)
	}
	return path
}

// probed is what ffprobe reports about a rendition.
type probed struct {
	codecs map[string]string // codec_type -> codec_name
	width  int
	pixFmt string
}

func probeFile(t *testing.T, path string) probed {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_streams", "-of", "json", "file:"+path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			PixFmt    string `json:"pix_fmt"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse ffprobe output: %v", err)
	}
	p := probed{codecs: map[string]string{}}
	for _, s := range parsed.Streams {
		p.codecs[s.CodecType] = s.CodecName
		if s.CodecType == "video" {
			p.width, p.pixFmt = s.Width, s.PixFmt
		}
	}
	return p
}

// realCache is a cache driven by the production ffmpeg runner.
func realCache(t *testing.T) *Cache {
	t.Helper()
	c := NewCache(context.Background(), NewFFmpegRunner(), clock.RealClock{}, quietLogger(), Options{
		Dir: filepath.Join(t.TempDir(), "cache"), MaxBytes: 1 << 30, FreeSpace: plentyOfSpace,
	})
	t.Cleanup(c.Wait)
	return c
}

// Synthetic inputs. The video source is 161 pixels wide on purpose: cases
// that keep that odd width exercise the even-dimension scaling.
var (
	lavfiVideo = []string{"-f", "lavfi", "-i", "testsrc=duration=1:size=161x120:rate=10"}
	lavfiAudio = []string{"-f", "lavfi", "-i", "sine=duration=1"}
)

func sampleArgs(parts ...[]string) []string {
	var args []string
	for _, p := range parts {
		args = append(args, p...)
	}
	return args
}

// TestCache_RealFFmpeg runs the production runner end to end: legacy formats
// in, client-compatible H.264/AAC MP4 or AAC M4A out.
func TestCache_RealFFmpeg(t *testing.T) {
	requireFFmpeg(t)
	both := sampleArgs(lavfiVideo, lavfiAudio)
	av := map[string]string{"video": "h264", "audio": "aac"}
	tests := []struct {
		name string
		file string
		args []string
		kind Kind
		want map[string]string
	}{
		{"wmv", "clip.wmv", append(both, "-c:v", "wmv2", "-s", "160x120", "-c:a", "wmav2"), KindVideo, av},
		{"avi odd width", "clip.avi", append(both, "-c:v", "mpeg4", "-c:a", "mp2"), KindVideo, av},
		{"flv", "clip.flv", append(both, "-c:v", "flv", "-s", "160x120", "-ar", "44100", "-c:a", "mp3"), KindVideo, av},
		{"silent avi", "silent.avi", append(lavfiVideo, "-c:v", "mpeg4"), KindVideo, map[string]string{"video": "h264"}},
		{"wma", "song.wma", append(lavfiAudio, "-c:a", "wmav2"), KindAudio, map[string]string{"audio": "aac"}},
		// Audio-only Windows Media file with a video extension: the row is
		// typed "video", there is no video stream, and it must still play.
		{"audio-only wmv", "music.wmv", append(lavfiAudio, "-c:a", "wmav2", "-f", "asf"), KindVideo, map[string]string{"audio": "aac"}},
		// File names that look like options, protocols or image patterns.
		{"hostile file name", "-weird:name %03d.avi", append(both, "-c:v", "mpeg4", "-c:a", "mp2", "-f", "avi"), KindVideo, av},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := makeSample(t, tt.file, tt.args...)
			r, err := realCache(t).Ensure(context.Background(), Source{MediaID: int64(i + 1), Path: src, Kind: tt.kind})
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			got := probeFile(t, r.Path)
			if len(got.codecs) != len(tt.want) {
				t.Fatalf("streams = %v, want %v", got.codecs, tt.want)
			}
			for typ, codec := range tt.want {
				if got.codecs[typ] != codec {
					t.Errorf("%s codec = %q, want %q", typ, got.codecs[typ], codec)
				}
			}
			if _, hasVideo := tt.want["video"]; hasVideo && (got.width%2 != 0 || got.pixFmt != "yuv420p") {
				t.Errorf("video is %d wide, %s; want even width and yuv420p", got.width, got.pixFmt)
			}
		})
	}
}

// TestCache_RealFFmpegStreamCopy checks the copy path: a source that already
// holds H.264/AAC keeps its video bit-for-bit instead of being re-encoded.
func TestCache_RealFFmpegStreamCopy(t *testing.T) {
	requireFFmpeg(t)
	h264 := []string{"-c:v", "libx264", "-pix_fmt", "yuv420p", "-s", "160x120"}
	tests := []struct {
		name     string
		file     string
		args     []string
		wantCopy bool
	}{
		{"h264+aac in flv", "clip.flv", sampleArgs(lavfiVideo, lavfiAudio, h264, []string{"-c:a", "aac", "-ar", "44100"}), true},
		{"h264+ac3 in mkv", "clip.mkv", sampleArgs(lavfiVideo, lavfiAudio, h264, []string{"-c:a", "ac3"}), true},
		// Same streams in AVI: no timestamps, so the video is re-encoded.
		{"h264 in avi", "clip.avi", sampleArgs(lavfiVideo, lavfiAudio, h264, []string{"-c:a", "mp2"}), false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := makeSample(t, tt.file, tt.args...)
			r, err := realCache(t).Ensure(context.Background(), Source{MediaID: int64(i + 1), Path: src, Kind: KindVideo})
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			got := probeFile(t, r.Path)
			if got.codecs["video"] != "h264" || got.codecs["audio"] != "aac" {
				t.Fatalf("streams = %v, want h264 + aac", got.codecs)
			}
			// A copied video stream has the same MD5 as the source's.
			if same := videoMD5(t, src) == videoMD5(t, r.Path); same != tt.wantCopy {
				t.Errorf("video stream copied = %v, want %v", same, tt.wantCopy)
			}
		})
	}
}

// videoMD5 hashes the compressed video packets of a file.
func videoMD5(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", "file:"+path, "-map", "0:v:0", "-c", "copy", "-f", "md5", "-").Output()
	if err != nil {
		t.Fatalf("hash video stream of %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

// TestCache_RealFFmpegRejectsPlaylists is the regression test for the
// content-probing attack: a file with a media extension that is really a
// playlist must not make ffmpeg open the files it references. The playlists
// use a relative reference, the form ffmpeg's concat demuxer follows by
// default (it only refuses absolute paths), so without the pinned demuxer
// the first cases do produce a rendition of the private file.
func TestCache_RealFFmpegRejectsPlaylists(t *testing.T) {
	requireFFmpeg(t)
	// dir/private/s.mp4 is the file another user is not allowed to see.
	sample := makeSample(t, "s.mp4", sampleArgs(lavfiVideo, []string{"-c:v", "libx264", "-pix_fmt", "yuv420p", "-s", "160x120"})...)
	dir := filepath.Dir(sample)
	if err := os.Mkdir(filepath.Join(dir, "private"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(sample, filepath.Join(dir, "private", "s.mp4")); err != nil {
		t.Fatal(err)
	}
	const concat = "ffconcat version 1.0\nfile 'private/s.mp4'\n"
	const hls = "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1.0,\nprivate/s.mp4\n#EXT-X-ENDLIST\n"
	tests := []struct{ name, file, content string }{
		{"ffconcat as avi", "evil.avi", concat},
		{"ffconcat as wmv", "evil.wmv", concat},
		{"ffconcat as mkv", "evil.mkv", concat},
		{"hls as flv", "evil.flv", hls},
		{"hls as mp4", "evil.mp4", hls},
		{"plain garbage", "broken.avi", "this is not a video"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := filepath.Join(dir, tt.file)
			if err := os.WriteFile(src, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			c := realCache(t)
			_, err := c.Ensure(context.Background(), Source{MediaID: int64(i + 1), Path: src, Kind: KindVideo})
			if err == nil || errors.Is(err, ErrPending) {
				t.Fatalf("Ensure = %v; a playlist must fail to parse, not be followed", err)
			}
			c.Wait()
			wantNames(t, c.opts.Dir)
		})
	}
}
