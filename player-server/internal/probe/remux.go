package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"
)

// Remuxer remuxes media on-the-fly to a browser-friendly container.
type Remuxer interface {
	Remux(ctx context.Context, inputPath string, w io.Writer) error
}

// FFRemuxer implements Remuxer using ffmpeg.
type FFRemuxer struct{}

var _ Remuxer = (*FFRemuxer)(nil)

// NewFFRemuxer creates a new FFRemuxer.
func NewFFRemuxer() *FFRemuxer {
	return &FFRemuxer{}
}

const remuxWaitDelay = 10 * time.Second

// Remux runs ffmpeg to copy video/audio streams into a fragmented MP4
// suitable for streaming to a browser.
func (f *FFRemuxer) Remux(ctx context.Context, inputPath string, w io.Writer) error {
	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", inputPath,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-dn",
		"-sn",
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	)
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = remuxWaitDelay
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("remux stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if isContextError(err) {
			return nil
		}
		return fmt.Errorf("remux start: %w", err)
	}
	var firstErr error
	if _, err := io.Copy(w, stdout); err != nil {
		if !isContextError(err) {
			slog.Error("copy remuxed media", "file", inputPath, "err", err)
			firstErr = fmt.Errorf("copy remuxed media: %w", err)
		}
	}
	if err := cmd.Wait(); err != nil {
		if !isContextError(err) && firstErr == nil {
			firstErr = fmt.Errorf("remux wait: %w", err)
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// LooksLikeMPEGTS inspects the first bytes of a file for MPEG-TS sync
// markers (0x47 at 188- or 192-byte intervals).
func LooksLikeMPEGTS(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 188*5)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	buf = buf[:n]
	return hasMPEGTSsync(buf, 188) || hasMPEGTSsync(buf, 192)
}

func hasMPEGTSsync(buf []byte, packetSize int) bool {
	if len(buf) < packetSize*3+1 {
		return false
	}
	for offset := 0; offset < packetSize; offset++ {
		matches := 0
		for pos := offset; pos < len(buf); pos += packetSize {
			if buf[pos] != 0x47 {
				break
			}
			matches++
			if matches >= 3 {
				return true
			}
		}
	}
	return false
}
