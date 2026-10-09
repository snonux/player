package thumb

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestFFmpegGenerator_Generate_Video: a video (duration well above the seek
// margin) is seeked into with -ss.
func TestFFmpegGenerator_Generate_Video(t *testing.T) {
	called := false
	fakeExecer := func(_ context.Context, name string, arg ...string) *exec.Cmd {
		called = true
		if name != "ffmpeg" {
			t.Errorf("expected ffmpeg, got %s", name)
		}
		args := strings.Join(arg, " ")
		for _, want := range []string{"-ss", "-i", "-frames:v 1", "-update 1", "-y"} {
			if !strings.Contains(args, want) {
				t.Errorf("missing %s flag in %q", want, args)
			}
		}
		return exec.Command("true")
	}
	g := &FFmpegGenerator{execer: fakeExecer, rnd: rand.New(rand.NewSource(1)), stat: wroteSomething}
	if err := g.Generate(context.Background(), "input.mp4", "out.jpg", 120.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected fake execer to be called")
	}
}

// TestFFmpegGenerator_Generate_Image: an image (duration 0) must not get
// -ss, which makes some ffmpeg versions skip its only frame.
func TestFFmpegGenerator_Generate_Image(t *testing.T) {
	called := false
	fakeExecer := func(_ context.Context, _ string, arg ...string) *exec.Cmd {
		called = true
		args := strings.Join(arg, " ")
		if strings.Contains(args, "-ss") {
			t.Error("unexpected -ss flag for image")
		}
		if !strings.Contains(args, "-i") {
			t.Error("missing -i flag")
		}
		return exec.Command("true")
	}
	g := &FFmpegGenerator{execer: fakeExecer, rnd: rand.New(rand.NewSource(1))}
	if err := g.Generate(context.Background(), "photo.jpg", "thumb.jpg", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected fake execer to be called for image")
	}
}

func TestFFmpegGenerator_Generate_Error(t *testing.T) {
	ctx := context.Background()
	fakeExecer := func(_ context.Context, name string, arg ...string) *exec.Cmd {
		return exec.Command("false")
	}
	g := &FFmpegGenerator{execer: fakeExecer, rnd: rand.New(rand.NewSource(1))}
	if err := g.Generate(ctx, "input.mp4", "output.jpg", 10.0); err == nil {
		t.Fatal("expected error from failing ffmpeg command")
	}
}

func TestFFmpegGenerator_Generate_RandomOffset(t *testing.T) {
	ctx := context.Background()
	var offsets []float64
	fakeExecer := func(_ context.Context, name string, arg ...string) *exec.Cmd {
		for i := 0; i < len(arg); i++ {
			if arg[i] == "-ss" && i+1 < len(arg) {
				off, err := strconv.ParseFloat(arg[i+1], 64)
				if err != nil {
					t.Fatalf("failed to parse offset: %v", err)
				}
				offsets = append(offsets, off)
			}
		}
		return exec.Command("true")
	}

	// Use two generators with different seeds.
	g1 := &FFmpegGenerator{execer: fakeExecer, rnd: rand.New(rand.NewSource(time.Now().UnixNano())), stat: wroteSomething}
	g2 := &FFmpegGenerator{execer: fakeExecer, rnd: rand.New(rand.NewSource(time.Now().UnixNano() + 12345)), stat: wroteSomething}

	for i := 0; i < 5; i++ {
		if err := g1.Generate(ctx, "input.mp4", "out.jpg", 100.0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := g2.Generate(ctx, "input.mp4", "out.jpg", 100.0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(offsets) != 10 {
		t.Fatalf("expected 10 offsets, got %d", len(offsets))
	}

	// Check that not all offsets are identical (should be extremely unlikely with different seeds).
	allSame := true
	for i := 1; i < len(offsets); i++ {
		if offsets[i] != offsets[0] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Fatal("expected different random offsets, but all were identical")
	}
}

// wroteSomething and wroteNothing stand in for os.Stat on the output file
// of a fake ffmpeg run, which writes no file.
func wroteSomething(string) (os.FileInfo, error) { return fakeInfo{size: 1}, nil }
func wroteNothing(string) (os.FileInfo, error)   { return fakeInfo{size: 0}, nil }

// recordingExecer returns a fake execer that runs command in place of ffmpeg
// and appends each invocation's -ss offset to runs, or -1 when it had none.
func recordingExecer(runs *[]float64, command string) func(context.Context, string, ...string) *exec.Cmd {
	return func(_ context.Context, _ string, arg ...string) *exec.Cmd {
		seek := -1.0
		for i := 0; i+1 < len(arg); i++ {
			if arg[i] == "-ss" {
				seek, _ = strconv.ParseFloat(arg[i+1], 64)
			}
		}
		*runs = append(*runs, seek)
		return exec.Command(command)
	}
}

// TestFFmpegGenerator_SeekOffsetStaysInsideTheVideo: seeking to or past the
// last frame yields no frame, so the offset stays a margin away from the
// reported end, and a video too short for that is not seeked in at all.
func TestFFmpegGenerator_SeekOffsetStaysInsideTheVideo(t *testing.T) {
	ctx := context.Background()
	for _, duration := range []float64{0, 0.6, 1.0} {
		var runs []float64
		g := &FFmpegGenerator{execer: recordingExecer(&runs, "true"), rnd: rand.New(rand.NewSource(1)), stat: wroteSomething}
		if err := g.Generate(ctx, "in.mp4", "out.jpg", duration); err != nil {
			t.Fatalf("duration %v: %v", duration, err)
		}
		if len(runs) != 1 || runs[0] != -1 {
			t.Errorf("duration %v: runs = %v, want a single run without -ss", duration, runs)
		}
	}
	for _, duration := range []float64{1.2, 2, 3, 8, 3600} {
		var runs []float64
		g := &FFmpegGenerator{execer: recordingExecer(&runs, "true"), rnd: rand.New(rand.NewSource(7)), stat: wroteSomething}
		for i := 0; i < 200; i++ {
			if err := g.Generate(ctx, "in.mp4", "out.jpg", duration); err != nil {
				t.Fatalf("duration %v: %v", duration, err)
			}
		}
		for _, off := range runs {
			if off < 0 || off > duration-seekMargin {
				t.Fatalf("duration %v: offset %v outside [0, %v]", duration, off, duration-seekMargin)
			}
		}
		if len(runs) != 200 {
			t.Errorf("duration %v: %d runs for 200 thumbnails, want no fallback runs", duration, len(runs))
		}
	}
}

// TestFFmpegGenerator_FallsBackToFirstFrame: when the seeked run fails, or
// succeeds without writing a frame (the offset was behind the last video
// frame, e.g. under a longer audio track), the first frame is taken.
func TestFFmpegGenerator_FallsBackToFirstFrame(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		command string
		stat    func(string) (os.FileInfo, error)
	}{
		{"seeked run exits successfully with an empty file", "true", wroteNothing},
		{"seeked run exits successfully without a file", "true", func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var runs []float64
			g := &FFmpegGenerator{execer: recordingExecer(&runs, tt.command), rnd: rand.New(rand.NewSource(1)), stat: tt.stat}
			if err := g.Generate(ctx, "in.mp4", "out.jpg", 30); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(runs) != 2 || runs[0] < 0 || runs[1] != -1 {
				t.Errorf("runs = %v, want a seeked run followed by one without -ss", runs)
			}
		})
	}
	t.Run("seeked run fails, so does the fallback", func(t *testing.T) {
		var runs []float64
		g := &FFmpegGenerator{execer: recordingExecer(&runs, "false"), rnd: rand.New(rand.NewSource(1))}
		if err := g.Generate(ctx, "in.mp4", "out.jpg", 30); err == nil {
			t.Fatal("expected the error of the fallback run")
		}
		if len(runs) != 2 || runs[1] != -1 {
			t.Errorf("runs = %v, want the fallback to have been tried", runs)
		}
	})
	t.Run("no fallback once the context is cancelled", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		var runs []float64
		g := &FFmpegGenerator{execer: recordingExecer(&runs, "false"), rnd: rand.New(rand.NewSource(1))}
		if err := g.Generate(cancelled, "in.mp4", "out.jpg", 30); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if len(runs) != 1 {
			t.Errorf("runs = %v, want no second run after cancellation", runs)
		}
	})
}

func TestMockGenerator(t *testing.T) {
	ctx := context.Background()
	m := &MockGenerator{}
	if err := m.Generate(ctx, "in", "out", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m.GenerateFunc = func(context.Context, string, string, float64) error {
		return errors.New("fail")
	}
	if err := m.Generate(ctx, "in", "out", 0); err == nil {
		t.Fatal("expected error from mock generator")
	}
}

func TestNewFFmpegGenerator_Seeded(t *testing.T) {
	g := NewFFmpegGenerator()
	if g.rnd == nil {
		t.Fatal("expected rnd to be initialized")
	}
	// Generate a value to ensure the source is functional.
	v := g.rnd.Float64()
	if v < 0 || v >= 1 {
		t.Fatalf("expected float in [0,1), got %v", v)
	}
}
