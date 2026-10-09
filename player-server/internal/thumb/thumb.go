// Package thumb generates thumbnail images.
package thumb

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"time"

	"codeberg.org/snonux/player/internal/ffsafe"
)

// Generator creates a thumbnail for a given media file.
type Generator interface {
	Generate(ctx context.Context, inputPath, outputPath string, duration float64) error
}

// FFmpegGenerator uses ffmpeg to extract a random frame.
type FFmpegGenerator struct {
	execer func(ctx context.Context, name string, arg ...string) *exec.Cmd
	rnd    *rand.Rand
	// stat inspects the generated file; nil means os.Stat. Tests whose
	// fake execer writes no file replace it.
	stat func(name string) (os.FileInfo, error)
}

var _ Generator = (*FFmpegGenerator)(nil)

// NewFFmpegGenerator creates a new FFmpegGenerator with a seeded random source.
func NewFFmpegGenerator() *FFmpegGenerator {
	return &FFmpegGenerator{
		execer: exec.CommandContext,
		rnd:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

const thumbWaitDelay = 15 * time.Second

// seekMargin is how far before the reported end of a video the random frame
// offset stops, in seconds, and the duration up to which no seek is done at
// all. Seeking to or past the last video frame yields no frame, and the
// reported duration is that of the container: it is rounded, and an audio
// track may run longer than the video.
const seekMargin = 1.0

// Generate runs ffmpeg to produce a JPEG thumbnail of inputPath at
// outputPath. duration is the media duration in seconds, 0 for an image.
//
// A video longer than seekMargin gets a frame from a random offset in
// [0, duration-seekMargin). The margin cannot rule out an offset behind the
// last video frame (an audio track can outlast the video by any amount),
// and ffmpeg then writes no frame: depending on its version it fails, or
// exits successfully leaving an empty or no file. In either case the first
// frame is taken instead, which every decodable video has. Images, and
// videos too short to seek in, get the first frame straight away.
func (g *FFmpegGenerator) Generate(ctx context.Context, inputPath, outputPath string, duration float64) error {
	if duration > seekMargin {
		offset := g.rnd.Float64() * (duration - seekMargin)
		if err := g.run(ctx, inputPath, outputPath, &offset); err == nil && g.wroteFrame(outputPath) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("ffmpeg generate thumbnail for %s: %w", inputPath, err)
		}
	}
	if err := g.run(ctx, inputPath, outputPath, nil); err != nil {
		return fmt.Errorf("ffmpeg generate thumbnail for %s: %w", inputPath, err)
	}
	return nil
}

// run executes ffmpeg once, seeking to *seek first when it is given.
func (g *FFmpegGenerator) run(ctx context.Context, inputPath, outputPath string, seek *float64) error {
	args, err := thumbArgs(inputPath, outputPath, seek)
	if err != nil {
		return err
	}
	cmd := g.execer(ctx, "ffmpeg", args...)
	cmd.WaitDelay = thumbWaitDelay
	return cmd.Run()
}

// thumbArgs builds the ffmpeg argument list for one thumbnail.
//
// Input: opened through ffsafe.SourceArgs, never by its bare name. ffmpeg
// would otherwise follow a playlist disguised as media and put a frame of
// another file into the thumbnail, and read an image named "a%03d.png" as a
// sequence of its sibling files. With the hardening such a name is read
// literally and a disguised playlist fails.
//
// Output: ffsafe.OutputArg keeps a name starting with "-" or containing ":"
// from being read as an option or protocol, and -update 1 makes the image
// muxer take the name literally. Without it a "%03d" anywhere in the output
// path (file or directory name, both come from user-chosen media names) is
// expanded as an image sequence pattern: ffmpeg then writes a differently
// named file, or none, and may still exit successfully.
func thumbArgs(inputPath, outputPath string, seek *float64) ([]string, error) {
	input, err := ffsafe.SourceArgs(inputPath)
	if err != nil {
		return nil, err
	}
	output, err := ffsafe.OutputArg(outputPath)
	if err != nil {
		return nil, err
	}
	args := []string{"-nostdin"}
	if seek != nil {
		// -ss before -i is a fast seek. It is only used for videos: for a
		// static image it produces no output frame on some ffmpeg versions
		// (it skips past the single frame).
		args = append(args, "-ss", fmt.Sprintf("%.3f", *seek))
	}
	args = append(args, input...)
	return append(args,
		"-vf", "scale=320:-1",
		"-frames:v", "1",
		"-q:v", "2",
		"-update", "1",
		"-y",
		output,
	), nil
}

// wroteFrame reports whether a non-empty file is at outputPath.
func (g *FFmpegGenerator) wroteFrame(outputPath string) bool {
	stat := g.stat
	if stat == nil {
		stat = os.Stat
	}
	info, err := stat(outputPath)
	return err == nil && info.Size() > 0
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
