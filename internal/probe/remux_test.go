package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
