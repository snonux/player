package service

import (
	"bytes"
	"context"
	"errors"
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
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}

	stream, err := NewMediaStreamer(nil, dir).Open(context.Background(), &FileResult{
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
	dir := t.TempDir()
	path := writeMPEGTSFileInDir(t, dir)
	stream, err := NewMediaStreamer(&mockRemuxer{}, dir).Open(context.Background(), &FileResult{
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
	dir := t.TempDir()
	path := writeMPEGTSFileInDir(t, dir)
	stream, err := NewMediaStreamer(&mockRemuxer{}, dir).Open(context.Background(), &FileResult{
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
	streamer := NewMediaStreamer(remuxer, "")
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

// writeMPEGTSFileInDir writes a minimal MPEG-TS file into dir and returns its
// path. dir is supplied by the caller so that the same temp directory can be
// used as both the file location and the mediaRoot passed to NewMediaStreamer,
// satisfying the path-traversal check in Open.
func writeMPEGTSFileInDir(t *testing.T, dir string) string {
	t.Helper()
	ts := make([]byte, 188*5)
	for i := 0; i < len(ts); i += 188 {
		ts[i] = 0x47
	}
	path := filepath.Join(dir, "clip.ts")
	if err := os.WriteFile(path, ts, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMediaStreamerOpenRejectsPathOutsideRoot(t *testing.T) {
	// Write a file outside the designated media root to verify that Open
	// returns ErrForbidden rather than serving the file.
	outsideDir := t.TempDir()
	path := filepath.Join(outsideDir, "secret.mp4")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use a separate directory as the media root so path is definitely outside.
	mediaRoot := t.TempDir()
	_, err := NewMediaStreamer(nil, mediaRoot).Open(context.Background(), &FileResult{
		Path:     path,
		FileName: "secret.mp4",
	}, false)
	if err == nil {
		t.Fatal("expected error for path outside media root, got nil")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}
