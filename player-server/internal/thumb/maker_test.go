package thumb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// recordingFS is a tiny MakerFS used to confirm FSMaker invokes MkdirAll
// with the canonical .thumbnails directory and surfaces filesystem errors.
type recordingFS struct {
	calls    []string
	mkdirErr error
}

func (r *recordingFS) MkdirAll(path string, _ os.FileMode) error {
	r.calls = append(r.calls, path)
	return r.mkdirErr
}

func TestFSMaker_MakeVideo_DelegatesToGenerator(t *testing.T) {
	var gotInput, gotOutput string
	var gotDuration float64
	gen := &MockGenerator{
		GenerateFunc: func(_ context.Context, in, out string, dur float64) error {
			gotInput = in
			gotOutput = out
			gotDuration = dur
			return nil
		},
	}
	fs := &recordingFS{}
	m := NewFSMaker(gen, fs, nil)

	got, err := m.MakeVideo(context.Background(), "/set/movie.mp4", 12.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantPath := filepath.Join("/set", DirName, "movie.mp4.jpg")
	if got != wantPath {
		t.Fatalf("path = %q, want %q", got, wantPath)
	}
	if gotInput != "/set/movie.mp4" {
		t.Fatalf("input = %q", gotInput)
	}
	if gotOutput != wantPath {
		t.Fatalf("output = %q, want %q", gotOutput, wantPath)
	}
	if gotDuration != 12.5 {
		t.Fatalf("duration = %v", gotDuration)
	}
	if len(fs.calls) != 1 || fs.calls[0] != filepath.Join("/set", DirName) {
		t.Fatalf("mkdir calls = %v", fs.calls)
	}
}

func TestFSMaker_MakeImage_PassesZeroDuration(t *testing.T) {
	var gotDuration float64
	gen := &MockGenerator{
		GenerateFunc: func(_ context.Context, _, _ string, dur float64) error {
			gotDuration = dur
			return nil
		},
	}
	m := NewFSMaker(gen, &recordingFS{}, nil)

	if _, err := m.MakeImage(context.Background(), "/p/img.png"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotDuration != 0 {
		t.Fatalf("duration = %v, want 0", gotDuration)
	}
}

func TestFSMaker_GeneratorError_SwallowedAsEmptyPath(t *testing.T) {
	gen := &MockGenerator{
		GenerateFunc: func(_ context.Context, _, _ string, _ float64) error {
			return errors.New("ffmpeg boom")
		},
	}
	m := NewFSMaker(gen, &recordingFS{}, nil)

	got, err := m.MakeVideo(context.Background(), "/s/v.mp4", 1)
	if err != nil {
		t.Fatalf("expected generator error to be swallowed, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty path on generator failure, got %q", got)
	}
}

func TestFSMaker_MkdirErrorPropagates(t *testing.T) {
	fs := &recordingFS{mkdirErr: errors.New("readonly fs")}
	m := NewFSMaker(&MockGenerator{}, fs, nil)

	_, err := m.MakeImage(context.Background(), "/s/x.jpg")
	if err == nil {
		t.Fatal("expected mkdir error to propagate")
	}
}

// TestFSMaker_NestedSourceGetsOwnThumbnail: same-named files in two folders
// of a set must not overwrite each other; each thumbnail goes into the
// .thumbnails directory of its own folder, which the maker creates.
func TestFSMaker_NestedSourceGetsOwnThumbnail(t *testing.T) {
	fs := &recordingFS{}
	m := NewFSMaker(&MockGenerator{}, fs, nil)

	a, errA := m.MakeVideo(context.Background(), "/set/a/clip.mp4", 1)
	b, errB := m.MakeVideo(context.Background(), "/set/b/clip.mp4", 1)
	if errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v, %v", errA, errB)
	}
	if want := filepath.Join("/set", "a", DirName, "clip.mp4.jpg"); a != want {
		t.Fatalf("path = %q, want %q", a, want)
	}
	if a == b {
		t.Fatalf("both sources map to %q", a)
	}
	if len(fs.calls) != 2 || fs.calls[0] != filepath.Dir(a) || fs.calls[1] != filepath.Dir(b) {
		t.Fatalf("mkdir calls = %v", fs.calls)
	}
}
