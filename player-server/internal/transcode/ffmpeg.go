package transcode

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// ffmpegWaitDelay bounds how long we wait for ffmpeg to exit after its
	// context is cancelled before the process is killed.
	ffmpegWaitDelay = 10 * time.Second
	// stderrTailBytes caps how much ffmpeg diagnostics end up in an error.
	stderrTailBytes = 2048
)

// FFmpegRunner implements Runner with the ffmpeg command-line tool.
type FFmpegRunner struct {
	binary string
}

var _ Runner = (*FFmpegRunner)(nil)

// NewFFmpegRunner creates a Runner that invokes "ffmpeg" from PATH.
func NewFFmpegRunner() *FFmpegRunner {
	return &FFmpegRunner{binary: "ffmpeg"}
}

// Transcode runs ffmpeg and writes the rendition to outputPath. The muxer is
// given explicitly because the cache writes to a temporary file name whose
// extension ffmpeg cannot map to a format.
func (f *FFmpegRunner) Transcode(ctx context.Context, kind Kind, inputPath, outputPath string) error {
	cmd := exec.CommandContext(ctx, f.binary, ffmpegArgs(kind, inputPath, outputPath)...)
	stderr := &tailBuffer{limit: stderrTailBytes}
	cmd.Stderr = stderr
	cmd.WaitDelay = ffmpegWaitDelay
	if err := cmd.Run(); err != nil {
		// Prefer the context error so callers can tell a cancelled job from
		// a file ffmpeg could not convert.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("ffmpeg transcode %s: %w", inputPath, ctxErr)
		}
		return fmt.Errorf("ffmpeg transcode %s: %w: %s", inputPath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ffmpegArgs builds the argument list for a rendition profile.
//
// Common choices: only the first video/audio stream is kept (subtitle and data
// streams of legacy containers frequently break the MP4 muxer), audio is
// downmixed to stereo AAC (WMA Pro can be multichannel, which not every
// client decodes), and "+faststart" moves the moov atom to the front so
// playback and seeking work over HTTP Range without fetching the file tail.
func ffmpegArgs(kind Kind, inputPath, outputPath string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", inputPath, "-sn", "-dn"}
	if kind == KindAudio {
		args = append(args, "-map", "0:a:0", "-vn")
	} else {
		// yuv420p plus even dimensions is what H.264 decoders in browsers
		// and on Android universally accept; "veryfast" keeps the first
		// playback wait short at a modest size cost.
		args = append(args,
			"-map", "0:v:0", "-map", "0:a:0?",
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
			"-pix_fmt", "yuv420p", "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		)
	}
	return append(args,
		"-c:a", "aac", "-b:a", "160k", "-ac", "2",
		"-movflags", "+faststart", "-f", "mp4", outputPath,
	)
}

// tailBuffer is an io.Writer that keeps only the last limit bytes written, so
// a badly damaged input cannot grow the captured ffmpeg diagnostics unbounded.
type tailBuffer struct {
	limit int
	data  []byte
}

// Write appends p and drops the oldest bytes beyond the limit.
func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if over := len(b.data) - b.limit; over > 0 {
		b.data = b.data[over:]
	}
	return len(p), nil
}

// String returns the retained tail.
func (b *tailBuffer) String() string { return string(b.data) }
