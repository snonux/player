package probe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type cancelingWriter struct {
	cancel context.CancelFunc
	err    error
}

func (w cancelingWriter) Write(p []byte) (int, error) {
	w.cancel()
	if w.err != nil {
		return 0, w.err
	}
	return len(p), nil
}

func TestFFRemuxerReturnsCopyErrorWhenContextCanceled(t *testing.T) {
	installFakeFFmpeg(t, "printf remuxed-output\nsleep 10\n")
	errRemuxWrite := errors.New("remux write failed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := NewFFRemuxer().Remux(ctx, "input.ts", cancelingWriter{
		cancel: cancel,
		err:    errRemuxWrite,
	})
	if !errors.Is(err, errRemuxWrite) {
		t.Fatalf("expected copy error, got %v", err)
	}
}

func TestFFRemuxerReturnsWaitErrorWhenContextCanceled(t *testing.T) {
	installFakeFFmpeg(t, "printf remuxed-output\nexit 7\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := NewFFRemuxer().Remux(ctx, "input.ts", cancelingWriter{cancel: cancel})
	if err == nil {
		t.Fatal("expected wait error")
	}
	if !strings.Contains(err.Error(), "remux wait") {
		t.Fatalf("expected remux wait error, got %v", err)
	}
}

func TestLooksLikeMPEGTS(t *testing.T) {
	ts := make([]byte, 188*5)
	for i := 0; i < len(ts); i += 188 {
		ts[i] = 0x47
	}
	tsPath := filepath.Join(t.TempDir(), "mislabelled.mp4")
	if err := os.WriteFile(tsPath, ts, 0o644); err != nil {
		t.Fatal(err)
	}
	if !LooksLikeMPEGTS(tsPath) {
		t.Fatal("expected MPEG-TS sync bytes to be detected")
	}

	mp4Path := filepath.Join(t.TempDir(), "real.mp4")
	if err := os.WriteFile(mp4Path, []byte("\x00\x00\x00\x18ftypmp42"), 0o644); err != nil {
		t.Fatal(err)
	}
	if LooksLikeMPEGTS(mp4Path) {
		t.Fatal("did not expect MP4 header to be detected as MPEG-TS")
	}
}

func installFakeFFmpeg(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Remuxed MPEG-TS must carry AAC AudioSpecificConfig so Firefox can decode
// audio (docs/media-firefox-audio.md).
func TestFFRemuxerWritesAACExtradata(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}
	dir := t.TempDir()
	tsPath := filepath.Join(dir, "clip.ts")
	outPath := filepath.Join(dir, "clip.mp4")
	makeTS := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:sample_rate=48000:duration=2",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d=2",
		"-c:a", "aac", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-shortest",
		"-f", "mpegts", tsPath)
	if out, err := makeTS.CombinedOutput(); err != nil {
		t.Fatalf("generate MPEG-TS: %v\n%s", err, out)
	}
	outFile, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewFFRemuxer().Remux(context.Background(), tsPath, outFile); err != nil {
		outFile.Close()
		t.Fatalf("remux: %v", err)
	}
	if err := outFile.Close(); err != nil {
		t.Fatal(err)
	}
	probe := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name,extradata_size", "-of", "csv=p=0", outPath)
	out, err := probe.CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %v\n%s", err, out)
	}
	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, ",")
	if len(parts) != 2 || parts[0] != "aac" {
		t.Fatalf("expected aac,extradata_size got %q", line)
	}
	size, err := strconv.Atoi(parts[1])
	if err != nil || size < 2 {
		t.Fatalf("AAC extradata_size want >= 2, got %q", line)
	}
}
