package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
)

func TestFFmpegArgs(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		want    []string
		notWant []string
	}{
		{"video", KindVideo, []string{"libx264", "yuv420p", "0:v:0", "0:a:0?", "aac", "+faststart"}, []string{"-vn"}},
		{"audio", KindAudio, []string{"-vn", "0:a:0", "aac", "+faststart"}, []string{"libx264"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := ffmpegArgs(tt.kind, "in.avi", "out.tmp")
			for _, w := range tt.want {
				if !slices.Contains(args, w) {
					t.Errorf("args %v missing %q", args, w)
				}
			}
			for _, w := range tt.notWant {
				if slices.Contains(args, w) {
					t.Errorf("args %v must not contain %q", args, w)
				}
			}
			// The output has a .tmp name, so the muxer must be explicit and
			// the output path must come last.
			if args[len(args)-1] != "out.tmp" || args[len(args)-3] != "-f" || args[len(args)-2] != "mp4" {
				t.Errorf("args must end with -f mp4 out.tmp, got %v", args[len(args)-3:])
			}
		})
	}
}

func TestTailBuffer(t *testing.T) {
	b := &tailBuffer{limit: 5}
	for _, s := range []string{"abc", "defg", "hi"} {
		if n, err := b.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", s, n, err)
		}
	}
	if got := b.String(); got != "efghi" {
		t.Errorf("tail = %q, want %q", got, "efghi")
	}
}

func TestFFmpegRunner_MissingBinary(t *testing.T) {
	r := &FFmpegRunner{binary: filepath.Join(t.TempDir(), "no-such-ffmpeg")}
	if err := r.Transcode(context.Background(), KindVideo, "in", "out"); err == nil {
		t.Fatal("expected error when ffmpeg is not installed")
	}
}

func TestFFmpegRunner_ReportsStderrOnFailure(t *testing.T) {
	// A stand-in "ffmpeg" that fails the way the real one does: diagnostics
	// on stderr and a non-zero exit code.
	script := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'Invalid data found' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &FFmpegRunner{binary: script}
	err := r.Transcode(context.Background(), KindVideo, "in", "out")
	if err == nil || !strings.Contains(err.Error(), "Invalid data found") {
		t.Fatalf("error = %v, want ffmpeg stderr in message", err)
	}
}

func TestFFmpegRunner_CancelledContext(t *testing.T) {
	script := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &FFmpegRunner{binary: script}
	if err := r.Transcode(ctx, KindVideo, "in", "out"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// requireFFmpeg skips the test unless ffmpeg, ffprobe and the libx264 encoder
// (present in the Alpine runtime image, missing in some distro builds) are
// available.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil || !strings.Contains(string(out), "libx264") {
		t.Skip("ffmpeg lacks the libx264 encoder")
	}
}

// makeLegacySample generates a one second legacy-format file with ffmpeg's
// synthetic sources; it skips when this ffmpeg build cannot encode it.
func makeLegacySample(t *testing.T, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	if out, err := exec.Command("ffmpeg", append(full, path)...).CombinedOutput(); err != nil {
		t.Skipf("cannot generate %s sample: %v: %s", name, err, out)
	}
	return path
}

// probeCodecs returns codec_type -> codec_name of a media file.
func probeCodecs(t *testing.T, path string) map[string]string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_streams", "-of", "json", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse ffprobe output: %v", err)
	}
	codecs := map[string]string{}
	for _, s := range parsed.Streams {
		codecs[s.CodecType] = s.CodecName
	}
	return codecs
}

// TestCache_RealFFmpeg runs the production runner end to end: legacy WMV/AVI/
// FLV/WMA in, client-compatible H.264/AAC MP4 or AAC M4A out.
func TestCache_RealFFmpeg(t *testing.T) {
	requireFFmpeg(t)

	// The synthetic source is 161 pixels wide on purpose: the AVI case keeps
	// that odd width and so exercises the even-dimension scale filter.
	video := []string{"-f", "lavfi", "-i", "testsrc=duration=1:size=161x120:rate=10", "-f", "lavfi", "-i", "sine=duration=1"}
	audio := []string{"-f", "lavfi", "-i", "sine=duration=1"}
	tests := []struct {
		name string
		file string
		args []string
		kind Kind
		want map[string]string
	}{
		{"wmv", "clip.wmv", append(video, "-c:v", "wmv2", "-s", "160x120", "-c:a", "wmav2"), KindVideo, map[string]string{"video": "h264", "audio": "aac"}},
		{"avi", "clip.avi", append(video, "-c:v", "mpeg4", "-c:a", "mp2"), KindVideo, map[string]string{"video": "h264", "audio": "aac"}},
		{"flv", "clip.flv", append(video, "-c:v", "flv", "-s", "160x120", "-ar", "44100", "-c:a", "mp3"), KindVideo, map[string]string{"video": "h264", "audio": "aac"}},
		{"silent avi", "silent.avi", []string{"-f", "lavfi", "-i", "testsrc=duration=1:size=160x120:rate=10", "-c:v", "mpeg4"}, KindVideo, map[string]string{"video": "h264"}},
		{"wma", "song.wma", append(audio, "-c:a", "wmav2"), KindAudio, map[string]string{"audio": "aac"}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := makeLegacySample(t, tt.file, tt.args...)
			c := NewCache(context.Background(), NewFFmpegRunner(), clock.RealClock{}, quietLogger(), Options{
				Dir: filepath.Join(t.TempDir(), "cache"), MaxBytes: 1 << 30,
			})
			r, err := c.Ensure(context.Background(), Source{MediaID: int64(i + 1), Path: src, Kind: tt.kind})
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			got := probeCodecs(t, r.Path)
			if len(got) != len(tt.want) {
				t.Fatalf("streams = %v, want %v", got, tt.want)
			}
			for typ, codec := range tt.want {
				if got[typ] != codec {
					t.Errorf("%s codec = %q, want %q", typ, got[typ], codec)
				}
			}
		})
	}
}

// TestCache_RealFFmpegRejectsGarbage checks the failure path with the real
// tool: a file that is not media yields an error and leaves no cache files.
func TestCache_RealFFmpegRejectsGarbage(t *testing.T) {
	requireFFmpeg(t)
	src := filepath.Join(t.TempDir(), "broken.avi")
	if err := os.WriteFile(src, []byte("this is not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "cache")
	c := NewCache(context.Background(), NewFFmpegRunner(), clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: 1 << 30})
	if _, err := c.Ensure(context.Background(), Source{MediaID: 1, Path: src, Kind: KindVideo}); err == nil {
		t.Fatal("expected ffmpeg to fail on garbage input")
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("failed transcode left files behind: %v", names)
	}
}
