package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/ffsafe"
)

// TestFFmpegArgs pins the complete argument lists. It needs no ffmpeg, so the
// security-relevant input options, the thread caps and the encoder settings
// are verified even where the real-ffmpeg tests are skipped.
func TestFFmpegArgs(t *testing.T) {
	input, err := ffsafe.InputArgs("/media/a.avi")
	if err != nil {
		t.Fatal(err)
	}
	head := slices.Concat(
		[]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-threads", "2"},
		[]string{"-protocol_whitelist", "file", "-format_whitelist", ffsafe.FormatWhitelist, "-i", "file:/media/a.avi"},
		[]string{"-sn", "-dn"},
	)
	encodeVideo := []string{
		"-filter_threads", "2",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-profile:v", "high", "-level:v", "4.1", "-pix_fmt", "yuv420p",
		"-vf", videoFilter, "-threads", "2",
	}
	encodeAudio := []string{"-c:a", "aac", "-b:a", "128k", "-ac", "2"}
	tail := []string{"-max_muxing_queue_size", "1024", "-fs", "5000", "-movflags", "+faststart", "-f", "mp4", "file:/cache/out.tmp"}
	videoMaps := []string{"-map", "0:v:0?", "-map", "0:a:0?"}
	audioMaps := []string{"-map", "0:a:0", "-vn"}

	tests := []struct {
		name string
		plan plan
		want [][]string
	}{
		{"video encode", plan{kind: KindVideo}, [][]string{head, videoMaps, encodeVideo, encodeAudio, tail}},
		{"video copy", plan{kind: KindVideo, copyVideo: true}, [][]string{head, videoMaps, {"-c:v", "copy"}, encodeAudio, tail}},
		{"video and audio copy", plan{kind: KindVideo, copyVideo: true, copyAudio: true}, [][]string{head, videoMaps, {"-c:v", "copy"}, {"-c:a", "copy"}, tail}},
		{"audio encode", plan{kind: KindAudio}, [][]string{head, audioMaps, encodeAudio, tail}},
		{"audio copy", plan{kind: KindAudio, copyAudio: true}, [][]string{head, audioMaps, {"-c:a", "copy"}, tail}},
	}
	f := &FFmpegRunner{threads: 2}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := f.args(tt.plan, input, "/cache/out.tmp", 5000)
			if want := slices.Concat(tt.want...); !slices.Equal(got, want) {
				t.Errorf("args =\n %q\nwant\n %q", got, want)
			}
		})
	}
	// Without a cap no "-fs" is passed.
	if got := f.args(plan{kind: KindAudio}, input, "/o", 0); slices.Contains(got, "-fs") {
		t.Errorf("unexpected -fs without a cap: %q", got)
	}
}

func TestPlanFor(t *testing.T) {
	good := mediaInfo{
		format: "flv", hasVideo: true, videoCodec: "h264", pixFmt: "yuv420p", width: 1920, height: 1080, level: 41,
		hasAudio: true, audioCodec: "aac", audioProfile: "LC", channels: 2,
	}
	with := func(change func(*mediaInfo)) mediaInfo {
		m := good
		change(&m)
		return m
	}
	tests := []struct {
		name      string
		kind      Kind
		src       mediaInfo
		copyVideo bool
		copyAudio bool
	}{
		{"h264 + aac-lc stereo", KindVideo, good, true, true},
		{"full range pixels", KindVideo, with(func(m *mediaInfo) { m.pixFmt = "yuvj420p" }), true, true},
		{"matroska", KindVideo, with(func(m *mediaInfo) { m.format = "matroska,webm" }), true, true},
		// AVI has no presentation timestamps: never copy video out of it.
		{"avi", KindVideo, with(func(m *mediaInfo) { m.format = "avi" }), false, true},
		// Everything the encoder guarantees must already hold for a copy.
		{"10-bit", KindVideo, with(func(m *mediaInfo) { m.pixFmt = "yuv420p10le" }), false, true},
		{"wider than 1920", KindVideo, with(func(m *mediaInfo) { m.width = 2560 }), false, true},
		{"taller than 1080", KindVideo, with(func(m *mediaInfo) { m.height = 1440 }), false, true},
		{"level above 4.1", KindVideo, with(func(m *mediaInfo) { m.level = 51 }), false, true},
		{"unknown level", KindVideo, with(func(m *mediaInfo) { m.level = 0 }), false, true},
		{"other video codec", KindVideo, with(func(m *mediaInfo) { m.videoCodec = "hevc" }), false, true},
		{"he-aac", KindVideo, with(func(m *mediaInfo) { m.audioProfile = "HE-AAC" }), true, false},
		{"5.1 aac", KindVideo, with(func(m *mediaInfo) { m.channels = 6 }), true, false},
		{"other audio codec", KindVideo, with(func(m *mediaInfo) { m.audioCodec = "ac3" }), true, false},
		{"probe failed", KindVideo, mediaInfo{}, false, false},
		// An audio rendition never carries video (cover art is dropped).
		{"audio kind", KindAudio, good, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := planFor(tt.kind, tt.src)
			if p.copyVideo != tt.copyVideo || p.copyAudio != tt.copyAudio || p.kind != tt.kind {
				t.Errorf("plan = %+v, want copyVideo=%v copyAudio=%v", p, tt.copyVideo, tt.copyAudio)
			}
		})
	}
}

func TestParseMediaInfo(t *testing.T) {
	got := parseMediaInfo([]byte(`{"format":{"format_name":"matroska,webm","duration":"12.5"},"streams":[
		{"codec_type":"data","codec_name":"bin_data"},
		{"codec_type":"video","codec_name":"h264","profile":"High","pix_fmt":"yuv420p","width":1280,"height":720,"level":31},
		{"codec_type":"audio","codec_name":"aac","profile":"LC","channels":2},
		{"codec_type":"video","codec_name":"mjpeg","pix_fmt":"yuvj444p","width":600,"height":600},
		{"codec_type":"audio","codec_name":"ac3","channels":6}]}`))
	want := mediaInfo{
		format: "matroska,webm", duration: 12.5,
		hasVideo: true, videoCodec: "h264", pixFmt: "yuv420p", width: 1280, height: 720, level: 31,
		hasAudio: true, audioCodec: "aac", audioProfile: "LC", channels: 2,
	}
	if got != want {
		t.Errorf("parseMediaInfo = %+v\nwant %+v (first stream of each type)", got, want)
	}
	if got := parseMediaInfo([]byte("not json")); got != (mediaInfo{}) {
		t.Errorf("parseMediaInfo(garbage) = %+v, want zero", got)
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

// script writes an executable shell script standing in for ffmpeg, ffprobe
// or nice.
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

// transcodeTo runs the runner on a dummy source and returns the output path.
func transcodeTo(t *testing.T, r *FFmpegRunner, ctx context.Context) (string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.tmp")
	return out, r.Transcode(ctx, Job{Source: Source{Path: "/media/in.flv", Kind: KindVideo}, Output: out, MaxBytes: 1 << 20})
}

func TestFFmpegRunner_MissingBinary(t *testing.T) {
	r := &FFmpegRunner{binary: filepath.Join(t.TempDir(), "no-such-ffmpeg")}
	if _, err := transcodeTo(t, r, context.Background()); err == nil {
		t.Fatal("expected error when ffmpeg is not installed")
	}
	if err := r.Available(); err == nil {
		t.Error("Available must report the missing binary")
	}
	if err := (&FFmpegRunner{binary: "sh"}).Available(); err != nil {
		t.Errorf("Available for an installed binary: %v", err)
	}
}

func TestFFmpegRunner_ReportsStderrOnFailure(t *testing.T) {
	// A stand-in "ffmpeg" that fails the way the real one does: diagnostics
	// on stderr and a non-zero exit code.
	r := &FFmpegRunner{binary: script(t, "ffmpeg", "echo 'Invalid data found' >&2\nexit 1")}
	_, err := transcodeTo(t, r, context.Background())
	if err == nil || !strings.Contains(err.Error(), "Invalid data found") || errors.Is(err, ErrNoSpace) {
		t.Fatalf("error = %v, want ffmpeg stderr in message", err)
	}
}

func TestFFmpegRunner_DiskFullIsNoSpace(t *testing.T) {
	r := &FFmpegRunner{binary: script(t, "ffmpeg", "echo 'av_interleaved_write_frame(): No space left on device' >&2\nexit 1")}
	if _, err := transcodeTo(t, r, context.Background()); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("error = %v, want ErrNoSpace", err)
	}
}

func TestFFmpegRunner_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &FFmpegRunner{binary: script(t, "ffmpeg", "exec sleep 30")}
	if _, err := transcodeTo(t, r, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// copyableProbe is a fake ffprobe: the source is copyable H.264; the output
// is reported with the given JSON.
func copyableProbe(t *testing.T, outputJSON string) string {
	t.Helper()
	const source = `{"format":{"format_name":"flv","duration":"3"},"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","width":640,"height":480,"level":30}]}`
	return script(t, "ffprobe", `case "$*" in *out.tmp*) echo '`+outputJSON+`';; *) echo '`+source+`';; esac`)
}

func TestFFmpegRunner_StreamCopyFallback(t *testing.T) {
	const goodOutput = `{"format":{"duration":"3"},"streams":[{"codec_type":"video","codec_name":"h264"}]}`
	tests := []struct {
		name       string
		ffmpegBody string // runs before the output is written
		outputJSON string
		wantCalls  []string
		wantErr    error
	}{
		{"copy accepted", "", goodOutput, []string{"-c:v copy"}, nil},
		// ffmpeg refuses the copy: encode instead.
		{"copy rejected by ffmpeg", `case "$*" in *"-c:v copy"*) exit 1;; esac`, goodOutput, []string{"-c:v copy", "-c:v libx264"}, nil},
		// ffmpeg "succeeds" but the result has no video or no duration.
		{"copy lost the video stream", "", `{"format":{"duration":"3"},"streams":[]}`, []string{"-c:v copy", "-c:v libx264"}, nil},
		{"copy without duration", "", `{"format":{"duration":"0"},"streams":[{"codec_type":"video"}]}`, []string{"-c:v copy", "-c:v libx264"}, nil},
		// A full disk fails the encode just the same: do not try.
		{"copy hits a full disk", `echo 'No space left on device' >&2; exit 1`, goodOutput, []string{"-c:v copy"}, ErrNoSpace},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "calls")
			ffmpeg := script(t, "ffmpeg", `echo "$*" >> `+log+"\n"+tt.ffmpegBody+"\n"+writeLastArg)
			r := &FFmpegRunner{binary: ffmpeg, probeBinary: copyableProbe(t, tt.outputJSON), threads: 1}

			_, err := transcodeTo(t, r, context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Transcode error = %v, want %v", err, tt.wantErr)
			}
			calls, _ := os.ReadFile(log)
			lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
			if len(lines) != len(tt.wantCalls) {
				t.Fatalf("ffmpeg ran %d times, want %d: %q", len(lines), len(tt.wantCalls), lines)
			}
			for i, want := range tt.wantCalls {
				if !strings.Contains(lines[i], want) {
					t.Errorf("call %d = %q, want it to contain %q", i, lines[i], want)
				}
			}
		})
	}
}

// A copied video stream is as big as in the source. When the source is larger
// than the cap the runner does not copy — and does not give up either: it
// re-encodes, which may fit. It never decides that the rendition is "too
// large"; only the cache knows whether the budget or a full disk set the cap.
func TestFFmpegRunner_StreamCopyOverCapFallsBackToEncode(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	src := filepath.Join(dir, "big.flv")
	if err := os.WriteFile(src, make([]byte, 2000), 0o644); err != nil {
		t.Fatal(err)
	}
	const good = `{"format":{"duration":"3"},"streams":[{"codec_type":"video","codec_name":"h264"}]}`
	r := &FFmpegRunner{binary: script(t, "ffmpeg", `echo "$*" >> `+log+"\n"+writeLastArg), probeBinary: copyableProbe(t, good), threads: 1}
	run := func(maxBytes int64) string {
		t.Helper()
		_ = os.Remove(log)
		job := Job{Source: Source{Path: src, Kind: KindVideo}, Output: filepath.Join(dir, "out.tmp"), MaxBytes: maxBytes}
		if err := r.Transcode(context.Background(), job); err != nil {
			t.Fatalf("Transcode with cap %d: %v", maxBytes, err)
		}
		calls, _ := os.ReadFile(log)
		return strings.TrimSpace(string(calls))
	}

	if calls := run(1000); strings.Contains(calls, "-c:v copy") || !strings.Contains(calls, "-c:v libx264") || strings.Count(calls, "\n") != 0 {
		t.Errorf("source over the cap: ffmpeg calls = %q, want exactly one encode", calls)
	}
	if calls := run(5000); !strings.Contains(calls, "-c:v copy") {
		t.Errorf("source under the cap: ffmpeg calls = %q, want a stream copy", calls)
	}
}

func TestFFmpegRunner_RunsUnderNice(t *testing.T) {
	log := filepath.Join(t.TempDir(), "nice-args")
	nice := script(t, "nice", `echo "$1 $2" > `+log+`; shift 2; exec "$@"`)
	r := &FFmpegRunner{binary: script(t, "ffmpeg", writeLastArg), nice: nice, threads: 1}

	out, err := transcodeTo(t, r, context.Background())
	if err != nil {
		t.Fatalf("Transcode: %v", err)
	}
	if got, _ := os.ReadFile(log); strings.TrimSpace(string(got)) != "-n "+niceness {
		t.Errorf("nice called with %q, want -n %s", got, niceness)
	}
	if got, _ := os.ReadFile(out); string(got) != "ok" {
		t.Errorf("output = %q", got)
	}
}

// The thread count follows the CPUs the process may use (GOMAXPROCS honours
// a container CPU limit), divided among the parallel jobs.
func TestNewFFmpegRunner_Threads(t *testing.T) {
	cpus := runtime.GOMAXPROCS(0)
	tests := []struct{ maxJobs, want int }{
		{1, cpus},
		{0, cpus}, // treated as one job
		{2, max(1, cpus/2)},
		{cpus * 4, 1}, // never zero
	}
	for _, tt := range tests {
		r := NewFFmpegRunner(tt.maxJobs)
		if r.threads != tt.want || r.binary != "ffmpeg" || r.probeBinary != "ffprobe" {
			t.Errorf("NewFFmpegRunner(%d) = %+v, want %d threads", tt.maxJobs, r, tt.want)
		}
	}
}

// Shutdown with a real child process: closing the cache kills it,
// WaitTimeout returns promptly and no temporary file stays behind.
func TestCache_ShutdownKillsProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	started := filepath.Join(t.TempDir(), "started")
	runner := &FFmpegRunner{binary: script(t, "ffmpeg", "touch "+started+"\nexec sleep 60")}
	c := NewCache(context.Background(), runner, clock.RealClock{}, quietLogger(), Options{Dir: dir, MaxBytes: 1 << 20, WaitLimit: time.Millisecond, FreeSpace: plentyOfSpace})

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

	c.Close()
	if !c.WaitTimeout(5 * time.Second) {
		t.Fatal("the process was not killed on shutdown")
	}
	wantNames(t, dir)
}
