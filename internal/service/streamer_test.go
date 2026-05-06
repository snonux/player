package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type mockRemuxer struct {
	path string
	data string
	err  error
}

func (m *mockRemuxer) Remux(ctx context.Context, inputPath string, w io.Writer) error {
	m.path = inputPath
	if m.data != "" {
		_, _ = io.WriteString(w, m.data)
	}
	return m.err
}

func TestMediaStreamerOpenDirect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}

	stream, err := NewMediaStreamer(nil).Open(context.Background(), &FileResult{
		Path:     path,
		FileName: "clip.mp4",
	}, false)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.File.Close()

	if stream.Remuxed {
		t.Fatal("expected direct stream")
	}
	if stream.ContentType != "video/mp4" {
		t.Fatalf("expected video/mp4, got %q", stream.ContentType)
	}
	if stream.Size != 3 {
		t.Fatalf("expected size 3, got %d", stream.Size)
	}
}

func TestMediaStreamerOpenAttachmentSkipsRemux(t *testing.T) {
	path := writeMPEGTSFile(t)
	stream, err := NewMediaStreamer(&mockRemuxer{}).Open(context.Background(), &FileResult{
		Path:     path,
		FileName: "clip.ts",
	}, true)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.File.Close()

	if stream.Remuxed {
		t.Fatal("expected attachment to skip remux")
	}
	if !stream.Attachment {
		t.Fatal("expected attachment flag")
	}
}

func TestMediaStreamerOpenRemuxedMPEGTS(t *testing.T) {
	path := writeMPEGTSFile(t)
	stream, err := NewMediaStreamer(&mockRemuxer{}).Open(context.Background(), &FileResult{
		Path:     path,
		FileName: "mislabelled.mp4",
		Duration: 42,
	}, false)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.File.Close()

	if !stream.Remuxed {
		t.Fatal("expected remuxed stream")
	}
	if stream.ContentType != "video/mp4" {
		t.Fatalf("expected video/mp4, got %q", stream.ContentType)
	}
	if stream.Duration != 42 {
		t.Fatalf("expected duration 42, got %f", stream.Duration)
	}
}

func TestMediaStreamerRemux(t *testing.T) {
	remuxer := &mockRemuxer{data: "remuxed"}
	streamer := NewMediaStreamer(remuxer)
	var out bytes.Buffer

	err := streamer.Remux(context.Background(), &StreamResult{Path: "/media/input.ts"}, &out)
	if err != nil {
		t.Fatalf("remux: %v", err)
	}
	if remuxer.path != "/media/input.ts" {
		t.Fatalf("expected remux path, got %q", remuxer.path)
	}
	if out.String() != "remuxed" {
		t.Fatalf("expected remuxed output, got %q", out.String())
	}
}

func writeMPEGTSFile(t *testing.T) string {
	t.Helper()
	ts := make([]byte, 188*5)
	for i := 0; i < len(ts); i += 188 {
		ts[i] = 0x47
	}
	path := filepath.Join(t.TempDir(), "clip.ts")
	if err := os.WriteFile(path, ts, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
