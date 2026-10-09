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
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

// requireFFmpegEnv makes the real-ffmpeg tests mandatory. They skip when the
// tools are missing so a developer machine without ffmpeg still passes, but a
// skip is easy to overlook: CI sets this variable (see the server-transcode
// step in .woodpecker/server.yml) and then nothing in this file may skip.
const requireFFmpegEnv = "PLAYER_REQUIRE_FFMPEG"

// skipOrFail skips the test, or fails it when real-ffmpeg tests are
// mandatory.
func skipOrFail(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv(requireFFmpegEnv) == "1" {
		t.Fatalf("%s=1 but %s", requireFFmpegEnv, reason)
	}
	t.Skip(reason)
}

// requireFFmpeg skips (or, with PLAYER_REQUIRE_FFMPEG=1, fails) the test
// unless ffmpeg, ffprobe and the libx264 encoder are available.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			skipOrFail(t, bin+" not installed")
		}
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil || !strings.Contains(string(out), "libx264") {
		skipOrFail(t, "ffmpeg lacks the libx264 encoder")
	}
}

// makeSample generates a short media file with ffmpeg's synthetic sources.
// Sample generation depends on encoders of the local ffmpeg build; a build
// that cannot create a sample skips that case (or fails it in CI).
func makeSample(t *testing.T, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	if out, err := exec.Command("ffmpeg", append(full, "file:"+path)...).CombinedOutput(); err != nil {
		skipOrFail(t, "cannot generate "+name+" sample: "+err.Error()+": "+string(out))
	}
	age(t, path, time.Hour) // not "still being written"
	return path
}

// probed is what ffprobe reports about a rendition.
type probed struct {
	codecs   map[string]string // codec_type -> codec_name
	width    int
	pixFmt   string
	channels int
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
			Channels  int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse ffprobe output: %v", err)
	}
	p := probed{codecs: map[string]string{}}
	for _, s := range parsed.Streams {
		p.codecs[s.CodecType] = s.CodecName
		switch s.CodecType {
		case "video":
			p.width, p.pixFmt = s.Width, s.PixFmt
		case "audio":
			p.channels = s.Channels
		}
	}
	return p
}

// realCache is a cache driven by the production ffmpeg runner.
func realCache(t *testing.T, maxBytes int64) *Cache {
	t.Helper()
	c := NewCache(context.Background(), NewFFmpegRunner(1), clock.RealClock{}, quietLogger(), Options{
		Dir: filepath.Join(t.TempDir(), "cache"), MaxBytes: maxBytes, FreeSpace: plentyOfSpace,
	})
	t.Cleanup(func() { stop(c) })
	return c
}

// Synthetic inputs. The video source is 161 pixels wide on purpose: cases
// that keep that odd width exercise the even-dimension scaling.
var (
	lavfiVideo = []string{"-f", "lavfi", "-i", "testsrc=duration=1:size=161x120:rate=10"}
	lavfiAudio = []string{"-f", "lavfi", "-i", "sine=duration=1"}
	// x264 turns the synthetic source into ordinary 8-bit 4:2:0 H.264.
	h264Small = []string{"-c:v", "libx264", "-pix_fmt", "yuv420p", "-s", "160x120"}
)

func sampleArgs(parts ...[]string) []string {
	var args []string
	for _, p := range parts {
		args = append(args, p...)
	}
	return args
}

// realCase is one source file and the streams its rendition must have.
type realCase struct {
	name string
	file string
	args []string
	kind Kind
	want map[string]string
}

func realCases() []realCase {
	both := sampleArgs(lavfiVideo, lavfiAudio)
	av := map[string]string{"video": "h264", "audio": "aac"}
	return []realCase{
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
		// Mislabeled but genuine media: the container is detected among
		// the whitelisted formats, whatever the extension says.
		{"mp4 named .flv", "mislabeled.flv", sampleArgs(both, h264Small, []string{"-c:a", "aac", "-f", "mp4"}), KindVideo, av},
		{"avi named .mkv", "mislabeled.mkv", append(both, "-c:v", "mpeg4", "-c:a", "mp2", "-f", "avi"), KindVideo, av},
	}
}

// TestCache_RealFFmpeg runs the production runner end to end: legacy formats
// in, client-compatible H.264/AAC MP4 or AAC M4A out.
func TestCache_RealFFmpeg(t *testing.T) {
	requireFFmpeg(t)
	for i, tt := range realCases() {
		t.Run(tt.name, func(t *testing.T) {
			src := makeSample(t, tt.file, tt.args...)
			r, err := realCache(t, 1<<30).Ensure(context.Background(), Source{MediaID: int64(i + 1), Path: src, Kind: tt.kind})
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

// TestCache_RealFFmpegStreamCopy checks the copy path: streams that already
// meet the rendition's guarantees are kept bit-for-bit, everything else is
// re-encoded.
func TestCache_RealFFmpegStreamCopy(t *testing.T) {
	requireFFmpeg(t)
	both := sampleArgs(lavfiVideo, lavfiAudio)
	surround := []string{"-f", "lavfi", "-i", "sine=duration=1", "-ac", "6", "-c:a", "aac"}
	tests := []struct {
		name      string
		file      string
		args      []string
		wantCopy  bool // video stream copied
		wantWidth int
	}{
		{"h264+aac in flv", "clip.flv", sampleArgs(both, h264Small, []string{"-c:a", "aac", "-ar", "44100"}), true, 160},
		{"h264+ac3 in mkv", "clip.mkv", sampleArgs(both, h264Small, []string{"-c:a", "ac3"}), true, 160},
		// Multichannel AAC is re-encoded to stereo; the video is still copied.
		{"h264+5.1 aac in mkv", "surround.mkv", sampleArgs(lavfiVideo, surround, h264Small), true, 160},
		// Same streams in AVI: no timestamps, so the video is re-encoded.
		{"h264 in avi", "clip.avi", sampleArgs(both, h264Small, []string{"-c:a", "mp2"}), false, 160},
		// H.264 above the 1080p cap is scaled down, not copied.
		{"oversized h264", "big.mkv", sampleArgs(both, []string{"-c:v", "libx264", "-pix_fmt", "yuv420p", "-s", "2560x1440", "-c:a", "aac"}), false, 1920},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := makeSample(t, tt.file, tt.args...)
			r, err := realCache(t, 1<<30).Ensure(context.Background(), Source{MediaID: int64(i + 1), Path: src, Kind: KindVideo})
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			got := probeFile(t, r.Path)
			if got.codecs["video"] != "h264" || got.codecs["audio"] != "aac" || got.channels > 2 || got.width != tt.wantWidth {
				t.Fatalf("rendition = %+v, want h264 %d wide + aac with at most 2 channels", got, tt.wantWidth)
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

// privateTree creates dir/private/s.mp4 — the file another user is not
// allowed to see — and returns dir.
func privateTree(t *testing.T) string {
	t.Helper()
	sample := makeSample(t, "s.mp4", sampleArgs(lavfiVideo, h264Small)...)
	dir := filepath.Dir(sample)
	if err := os.Mkdir(filepath.Join(dir, "private"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(sample, filepath.Join(dir, "private", "s.mp4")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestCache_RealFFmpegRejectsPlaylists is the regression test for the
// content-probing attack: a file with a media extension that really is an
// ffconcat playlist must not make ffmpeg open the files it references.
//
// The playlist uses a relative reference, which ffmpeg's concat demuxer
// follows by default. Without "-format_whitelist" every case here produces a
// rendition of the private file, so the test fails if the protection is
// removed; with it ffmpeg reports that the format is not on the whitelist,
// which the test checks to make sure it fails for the right reason.
// (HLS playlists are not covered: current ffmpeg already refuses to detect
// them under a non-HLS file name, so such cases would guard nothing.)
func TestCache_RealFFmpegRejectsPlaylists(t *testing.T) {
	requireFFmpeg(t)
	dir := privateTree(t)
	const concat = "ffconcat version 1.0\nfile 'private/s.mp4'\n"
	for i, name := range []string{"evil.avi", "evil.wmv", "evil.mkv", "evil.mp4", "evil.wma"} {
		t.Run(name, func(t *testing.T) {
			src := filepath.Join(dir, name)
			if err := os.WriteFile(src, []byte(concat), 0o644); err != nil {
				t.Fatal(err)
			}
			age(t, src, time.Hour)
			c := realCache(t, 1<<30)
			_, err := c.Ensure(context.Background(), Source{MediaID: int64(i + 1), Path: src, Kind: KindVideo})
			if err == nil || !strings.Contains(err.Error(), "not on whitelist") {
				t.Fatalf("Ensure = %v; want ffmpeg to refuse the concat format", err)
			}
			stop(c)
			wantNames(t, c.opts.Dir)
			// The runner's own ffprobe must not follow the playlist either.
			if info := NewFFmpegRunner(1).probe(context.Background(), src); info != (mediaInfo{}) {
				t.Errorf("probe followed the playlist: %+v", info)
			}
		})
	}
}

// A file that is no media at all fails and leaves nothing behind. (This is
// plain error handling, not a guard against the playlist attack.)
func TestCache_RealFFmpegRejectsGarbage(t *testing.T) {
	requireFFmpeg(t)
	src := filepath.Join(t.TempDir(), "broken.avi")
	if err := os.WriteFile(src, []byte("this is not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	age(t, src, time.Hour)
	c := realCache(t, 1<<30)
	if _, err := c.Ensure(context.Background(), Source{MediaID: 1, Path: src, Kind: KindVideo}); err == nil || errors.Is(err, ErrPending) {
		t.Fatalf("Ensure = %v, want a transcode failure", err)
	}
	stop(c)
	wantNames(t, c.opts.Dir)
}

// With real ffmpeg, "-fs" stops a rendition that would outgrow the space the
// cache may use; the truncated file is reported as "no space", not published.
func TestCache_RealFFmpegOutputCap(t *testing.T) {
	requireFFmpeg(t)
	noise := []string{"-f", "lavfi", "-i", "testsrc2=duration=4:size=640x480:rate=25", "-c:v", "mpeg4", "-q:v", "2"}
	src := makeSample(t, "big.avi", noise...)
	c := realCache(t, 20_000)
	if _, err := c.Ensure(context.Background(), Source{MediaID: 1, Path: src, Kind: KindVideo}); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("Ensure = %v, want ErrNoSpace", err)
	}
	stop(c)
	wantNames(t, c.opts.Dir)

	// The same file fits a normal budget.
	if _, err := realCache(t, 1<<30).Ensure(context.Background(), Source{MediaID: 1, Path: src, Kind: KindVideo}); err != nil {
		t.Fatalf("Ensure with room: %v", err)
	}
}
