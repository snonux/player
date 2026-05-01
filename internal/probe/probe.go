// Package probe implements media metadata probing.
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"codeberg.org/snonux/player/internal/model"
)

// Prober extracts metadata from a media file.
type Prober interface {
	Probe(ctx context.Context, path string) (*model.Metadata, error)
}

// FFProber wraps the ffprobe command-line tool.
type FFProber struct{}

// NewFFProber creates a new FFProber.
func NewFFProber() *FFProber {
	return &FFProber{}
}

// Probe runs ffprobe against the given path and parses the resulting JSON.
func (f *FFProber) Probe(ctx context.Context, path string) (*model.Metadata, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("ffprobe %s: %w: %s", path, err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	return parseFFprobeOutput(out)
}

type ffprobeOutput struct {
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		CodecType string `json:"codec_type"`
	} `json:"streams"`
}

func parseFFprobeOutput(data []byte) (*model.Metadata, error) {
	var out ffprobeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal ffprobe output: %w", err)
	}

	meta := &model.Metadata{}
	if out.Format.Duration != "" {
		if d, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil {
			meta.Duration = d
		}
	}
	if out.Format.BitRate != "" {
		if b, err := strconv.Atoi(out.Format.BitRate); err == nil {
			meta.Bitrate = b
		}
	}

	for _, s := range out.Streams {
		if s.CodecType == "video" {
			if meta.Codec == "" {
				meta.Codec = s.CodecName
			}
			if s.Width > 0 && s.Height > 0 {
				meta.Resolution = fmt.Sprintf("%dx%d", s.Width, s.Height)
			}
			break
		}
	}

	// Fallback to first stream codec if no video stream found.
	if meta.Codec == "" && len(out.Streams) > 0 {
		meta.Codec = out.Streams[0].CodecName
	}

	return meta, nil
}

// MockProber is a test fake for Prober.
type MockProber struct {
	ProbeFunc func(ctx context.Context, path string) (*model.Metadata, error)
}

// Probe delegates to ProbeFunc or returns zero-value metadata.
func (m *MockProber) Probe(ctx context.Context, path string) (*model.Metadata, error) {
	if m.ProbeFunc != nil {
		return m.ProbeFunc(ctx, path)
	}
	return &model.Metadata{}, nil
}
