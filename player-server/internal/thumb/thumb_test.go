package thumb

import (
	"context"
	"errors"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFFmpegGenerator_Generate(t *testing.T) {
	ctx := context.Background()

	// Video with duration > 0 should include -ss.
	videoCalled := false
	fakeExecerVideo := func(_ context.Context, name string, arg ...string) *exec.Cmd {
		videoCalled = true
		if name != "ffmpeg" {
			t.Errorf("expected ffmpeg, got %s", name)
		}
		args := strings.Join(arg, " ")
		if !strings.Contains(args, "-ss") {
			t.Error("missing -ss flag")
		}
		if !strings.Contains(args, "-i") {
			t.Error("missing -i flag")
		}
		if !strings.Contains(args, "-frames:v 1") {
			t.Error("missing -frames:v 1 flag")
		}
		if !strings.Contains(args, "-y") {
			t.Error("missing -y flag")
		}
		return exec.Command("true")
	}
	g := &FFmpegGenerator{execer: fakeExecerVideo, rnd: rand.New(rand.NewSource(1))}
	if err := g.Generate(ctx, "input.mp4", "out.jpg", 120.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !videoCalled {
		t.Fatal("expected fake execer to be called")
	}

	// Image with duration == 0 should NOT include -ss.
	imgCalled := false
	fakeExecerImage := func(_ context.Context, name string, arg ...string) *exec.Cmd {
		imgCalled = true
		args := strings.Join(arg, " ")
		if strings.Contains(args, "-ss") {
			t.Error("unexpected -ss flag for image")
		}
		if !strings.Contains(args, "-i") {
			t.Error("missing -i flag")
		}
		return exec.Command("true")
	}
	gi := &FFmpegGenerator{execer: fakeExecerImage, rnd: rand.New(rand.NewSource(1))}
	if err := gi.Generate(ctx, "photo.jpg", "thumb.jpg", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !imgCalled {
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
	g1 := &FFmpegGenerator{execer: fakeExecer, rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
	g2 := &FFmpegGenerator{execer: fakeExecer, rnd: rand.New(rand.NewSource(time.Now().UnixNano() + 12345))}

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
