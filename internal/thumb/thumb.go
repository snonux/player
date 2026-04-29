// Package thumb generates thumbnail images.
package thumb

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"time"
)

// Generator creates a thumbnail for a given media file.
type Generator interface {
	Generate(ctx context.Context, inputPath, outputPath string, duration float64) error
}

// FFmpegGenerator uses ffmpeg to extract a random frame.
type FFmpegGenerator struct {
	execer func(ctx context.Context, name string, arg ...string) *exec.Cmd
	rnd    *rand.Rand
}

// NewFFmpegGenerator creates a new FFmpegGenerator with a seeded random source.
func NewFFmpegGenerator() *FFmpegGenerator {
	return &FFmpegGenerator{
		execer: exec.CommandContext,
		rnd:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Generate picks a random offset (at least 1 second if duration > 0) and
// runs ffmpeg to produce a JPEG thumbnail.
func (g *FFmpegGenerator) Generate(ctx context.Context, inputPath, outputPath string, duration float64) error {
	offset := 0.0
	if duration > 0 {
		offset = g.rnd.Float64() * duration
		if offset < 1.0 {
			offset = 1.0
		}
	}

	cmd := g.execer(ctx, "ffmpeg",
		"-ss", fmt.Sprintf("%.3f", offset),
		"-i", inputPath,
		"-vf", "scale=320:-1",
		"-frames:v", "1",
		"-q:v", "2",
		"-y",
		outputPath,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg generate thumbnail for %s: %w", inputPath, err)
	}
	return nil
}

// MockGenerator is a test fake for Generator.
type MockGenerator struct {
	GenerateFunc func(ctx context.Context, inputPath, outputPath string, duration float64) error
}

// Generate delegates to GenerateFunc or succeeds silently.
func (m *MockGenerator) Generate(ctx context.Context, inputPath, outputPath string, duration float64) error {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, inputPath, outputPath, duration)
	}
	return nil
}
