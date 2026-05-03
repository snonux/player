package probe

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Remuxer remuxes media on-the-fly to a browser-friendly container.
type Remuxer interface {
	Remux(ctx context.Context, inputPath string, w io.Writer) error
}

// FFRemuxer implements Remuxer using ffmpeg.
type FFRemuxer struct{}

// NewFFRemuxer creates a new FFRemuxer.
func NewFFRemuxer() *FFRemuxer {
	return &FFRemuxer{}
}

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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("remux stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("remux start: %w", err)
	}
	if _, err := io.Copy(w, stdout); err != nil && ctx.Err() == nil {
		slog.Error("copy remuxed media", "file", inputPath, "err", err)
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("remux wait: %w", err)
	}
	return nil
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

// MimeTypeForFilename returns an HTTP Content-Type based on the file extension.
func MimeTypeForFilename(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	t := mime.TypeByExtension(ext)
	if t != "" {
		return t
	}
	switch ext {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".wav":
		return "audio/wav"
	case ".aac", ".m4a":
		return "audio/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".m4b":
		return "audio/x-m4b"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".avif":
		return "image/avif"
	case ".svg":
		return "image/svg+xml"
	}
	return "application/octet-stream"
}
