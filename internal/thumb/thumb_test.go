package thumb

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestFFmpegGenerator_Generate(t *testing.T) {
	ctx := context.Background()
	called := false

	fakeExecer := func(_ context.Context, name string, arg ...string) *exec.Cmd {
		called = true
		if name != "ffmpeg" {
			t.Errorf("expected ffmpeg, got %s", name)
		}
		// Verify some expected flags exist.
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
		// Return a command that does nothing successfully.
		return exec.Command("true")
	}

	g := &FFmpegGenerator{execer: fakeExecer}
	if err := g.Generate(ctx, "input.mp4", "output.jpg", 120.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected fake execer to be called")
	}

	// Duration zero or negative should still call execer with valid offset.
	called = false
	if err := g.Generate(ctx, "input.mp4", "output.jpg", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected fake execer to be called for zero duration")
	}
}

func TestFFmpegGenerator_Generate_Error(t *testing.T) {
	ctx := context.Background()
	fakeExecer := func(_ context.Context, name string, arg ...string) *exec.Cmd {
		return exec.Command("false")
	}
	g := &FFmpegGenerator{execer: fakeExecer}
	if err := g.Generate(ctx, "input.mp4", "output.jpg", 10.0); err == nil {
		t.Fatal("expected error from failing ffmpeg command")
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
