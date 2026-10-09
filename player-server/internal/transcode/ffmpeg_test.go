package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
)

// TestFFmpegArgs pins the complete argument lists. It needs no ffmpeg, so the
// security-relevant input options and the encoder settings are verified even
// where the real-ffmpeg tests are skipped.
func TestFFmpegArgs(t *testing.T) {
	input := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file", "-f", "avi", "-i", "file:/media/a.avi", "-sn", "-dn",
	}
	encodeVideo := []string{
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-profile:v", "high", "-level:v", "4.1", "-pix_fmt", "yuv420p",
		"-vf", videoFilter, "-threads", "2",
	}
	encodeAudio := []string{"-c:a", "aac", "-b:a", "160k", "-ac", "2"}
	output := []string{"-max_muxing_queue_size", "1024", "-movflags", "+faststart", "-f", "mp4", "file:/cache/out.tmp"}
	videoMaps := []string{"-map", "0:v:0?", "-map", "0:a:0?"}

	tests := []struct {
		name string
		plan plan
		want [][]string
	}{
		{"video encode", plan{kind: KindVideo}, [][]string{input, videoMaps, encodeVideo, encodeAudio, output}},
		{"video copy", plan{kind: KindVideo, copyVideo: true}, [][]string{input, videoMaps, {"-c:v", "copy"}, encodeAudio, output}},
		{"video and audio copy", plan{kind: KindVideo, copyVideo: true, copyAudio: true}, [][]string{input, videoMaps, {"-c:v", "copy"}, {"-c:a", "copy"}, output}},
		{"audio encode", plan{kind: KindAudio}, [][]string{input, {"-map", "0:a:0", "-vn"}, encodeAudio, output}},
		{"audio copy", plan{kind: KindAudio, copyAudio: true}, [][]string{input, {"-map", "0:a:0", "-vn"}, {"-c:a", "copy"}, output}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ffmpegArgs(tt.plan, "avi", "/media/a.avi", "/cache/out.tmp", 2)
			if want := slices.Concat(tt.want...); !slices.Equal(got, want) {
				t.Errorf("args =\n %q\nwant\n %q", got, want)
			}
		})
	}
}

func TestDemuxerFor(t *testing.T) {
	tests := map[string]string{
		"a.avi": "avi", "A.AVI": "avi", "a.wmv": "asf", "a.wma": "asf", "a.asf": "asf", "a.flv": "flv",
		"a.mkv": "matroska", "a.webm": "matroska", "a.mp4": "mov", "a.mov": "mov", "a.m4a": "mov", "a.m4b": "mov",
		"a.mp3": "mp3", "a.ogg": "ogg", "a.opus": "ogg", "a.flac": "flac", "a.wav": "wav", "a.aac": "aac",
		// Never fall back to auto-detection.
		"a.m3u8": "", "a.txt": "", "noext": "", "a.avi.concat": "",
	}
	for name, want := range tests {
		got, ok := demuxerFor(name)
		if got != want || ok != (want != "") {
			t.Errorf("demuxerFor(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}

func TestPlanFor(t *testing.T) {
	h264 := streams{videoCodec: "h264", pixFmt: "yuv420p", audioCodec: "aac"}
	tests := []struct {
		name      string
		kind      Kind
		demuxer   string
		st        streams
		copyVideo bool
		copyAudio bool
	}{
		{"h264+aac in flv", KindVideo, "flv", h264, true, true},
		{"h264+ac3 in mkv", KindVideo, "matroska", streams{videoCodec: "h264", pixFmt: "yuv420p", audioCodec: "ac3"}, true, false},
		{"full range h264", KindVideo, "flv", streams{videoCodec: "h264", pixFmt: "yuvj420p"}, true, false},
		// AVI has no presentation timestamps: never copy video out of it.
		{"h264 in avi", KindVideo, "avi", h264, false, true},
		// 10-bit H.264 does not decode in browsers.
		{"10-bit h264", KindVideo, "matroska", streams{videoCodec: "h264", pixFmt: "yuv420p10le"}, false, false},
		{"legacy codecs", KindVideo, "asf", streams{videoCodec: "wmv3", pixFmt: "yuv420p", audioCodec: "wmav2"}, false, false},
		{"probe failed", KindVideo, "flv", streams{}, false, false},
		// An audio rendition never carries video (cover art is dropped).
		{"audio kind", KindAudio, "mov", h264, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := planFor(tt.kind, tt.demuxer, tt.st)
			if p.copyVideo != tt.copyVideo || p.copyAudio != tt.copyAudio || p.kind != tt.kind {
				t.Errorf("plan = %+v, want copyVideo=%v copyAudio=%v", p, tt.copyVideo, tt.copyAudio)
			}
		})
	}
}

func TestParseStreams(t *testing.T) {
	got := parseStreams([]byte(`{"streams":[
		{"codec_type":"data","codec_name":"bin_data"},
		{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p"},
		{"codec_type":"audio","codec_name":"aac"},
		{"codec_type":"video","codec_name":"mjpeg","pix_fmt":"yuvj444p"},
		{"codec_type":"audio","codec_name":"ac3"}]}`))
	if want := (streams{videoCodec: "h264", pixFmt: "yuv420p", audioCodec: "aac"}); got != want {
		t.Errorf("parseStreams = %+v, want %+v (first stream of each type)", got, want)
	}
	if got := parseStreams([]byte("not json")); got != (streams{}) {
		t.Errorf("parseStreams(garbage) = %+v, want empty", got)
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

// script writes an executable shell script standing in for ffmpeg or nice.
func script(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeLastArg is a script body that writes "ok" to the output file, i.e. the
// last argument without its "file:" prefix.
const writeLastArg = `for last; do :; done; printf ok > "${last#file:}"`

func TestFFmpegRunner_MissingBinary(t *testing.T) {
	r := &FFmpegRunner{binary: filepath.Join(t.TempDir(), "no-such-ffmpeg")}
	if err := r.Transcode(context.Background(), Source{Path: "in.avi"}, "out"); err == nil {
		t.Fatal("expected error when ffmpeg is not installed")
	}
	if err := r.Available(); err == nil {
		t.Error("Available must report the missing binary")
	}
	if err := (&FFmpegRunner{binary: "sh"}).Available(); err != nil {
		t.Errorf("Available for an installed binary: %v", err)
	}
}

func TestFFmpegRunner_UnsupportedExtension(t *testing.T) {
	// The fake ffmpeg would succeed; it must never be reached.
	r := &FFmpegRunner{binary: script(t, "ffmpeg", writeLastArg)}
	out := filepath.Join(t.TempDir(), "out")
	for _, in := range []string{"playlist.m3u8", "list.ffconcat", "noext"} {
		if err := r.Transcode(context.Background(), Source{Path: in}, out); !errors.Is(err, ErrUnsupportedInput) {
			t.Errorf("Transcode(%q) = %v, want ErrUnsupportedInput", in, err)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("ffmpeg ran for an unsupported input")
	}
}

func TestFFmpegRunner_ReportsStderrOnFailure(t *testing.T) {
	// A stand-in "ffmpeg" that fails the way the real one does: diagnostics
	// on stderr and a non-zero exit code.
	r := &FFmpegRunner{binary: script(t, "ffmpeg", "echo 'Invalid data found' >&2\nexit 1")}
	err := r.Transcode(context.Background(), Source{Path: "in.avi"}, "out")
	if err == nil || !strings.Contains(err.Error(), "Invalid data found") {
		t.Fatalf("error = %v, want ffmpeg stderr in message", err)
	}
}

func TestFFmpegRunner_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &FFmpegRunner{binary: script(t, "ffmpeg", "exec sleep 30")}
	if err := r.Transcode(ctx, Source{Path: "in.avi"}, "out"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// A stream copy that ffmpeg rejects is repeated with full encoding.
func TestFFmpegRunner_FallsBackFromCopyToEncode(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	ffmpeg := script(t, "ffmpeg", `echo "$*" >> `+log+`
case "$*" in *"-c:v copy"*) exit 1;; esac
`+writeLastArg)
	ffprobe := script(t, "ffprobe", `echo '{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p"}]}'`)
	r := &FFmpegRunner{binary: ffmpeg, probeBinary: ffprobe, threads: 1}

	out := filepath.Join(dir, "out.tmp")
	if err := r.Transcode(context.Background(), Source{Path: "in.flv", Kind: KindVideo}, out); err != nil {
		t.Fatalf("Transcode: %v", err)
	}
	calls, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "-c:v copy") || !strings.Contains(lines[1], "-c:v libx264") {
		t.Errorf("ffmpeg calls = %q, want a copy attempt then an encode", lines)
	}
	if got, _ := os.ReadFile(out); string(got) != "ok" {
		t.Errorf("output = %q", got)
	}
}

func TestFFmpegRunner_RunsUnderNice(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "nice-args")
	nice := script(t, "nice", `echo "$1 $2" > `+log+`; shift 2; exec "$@"`)
	r := &FFmpegRunner{binary: script(t, "ffmpeg", writeLastArg), nice: nice, threads: 1}

	out := filepath.Join(dir, "out.tmp")
	if err := r.Transcode(context.Background(), Source{Path: "in.avi"}, out); err != nil {
		t.Fatalf("Transcode: %v", err)
	}
	if got, _ := os.ReadFile(log); strings.TrimSpace(string(got)) != "-n "+niceness {
		t.Errorf("nice called with %q, want -n %s", got, niceness)
	}
	if got, _ := os.ReadFile(out); string(got) != "ok" {
		t.Errorf("output = %q", got)
	}
}

func TestNewFFmpegRunner(t *testing.T) {
	r := NewFFmpegRunner()
	if r.binary != "ffmpeg" || r.probeBinary != "ffprobe" || r.threads < 1 {
		t.Errorf("unexpected runner %+v", r)
	}
}

// Shutdown with a real child process: cancelling the base context kills it,
// Wait returns promptly and no temporary file stays behind.
func TestCache_ShutdownKillsProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	started := filepath.Join(t.TempDir(), "started")
	runner := &FFmpegRunner{binary: script(t, "ffmpeg", "touch "+started+"\nexec sleep 60")}
	base, shutdown := context.WithCancel(context.Background())
	c := NewCache(base, runner, clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: 1 << 20, WaitLimit: time.Millisecond, FreeSpace: plentyOfSpace})

	if _, err := c.Ensure(context.Background(), newSource(t, 1)); !errors.Is(err, ErrPending) {
		t.Fatalf("Ensure = %v, want ErrPending", err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake ffmpeg never started")
		}
	}

	begin := time.Now()
	shutdown()
	c.Wait()
	if took := time.Since(begin); took > 5*time.Second {
		t.Errorf("shutdown took %s; the process was not killed", took)
	}
	wantNames(t, dir)
}
