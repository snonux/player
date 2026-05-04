// Package probe implements media metadata probing.
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/model"
	"github.com/rwcarlsen/goexif/exif"
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
	meta, err := parseFFprobeOutput(out)
	if err != nil {
		return nil, err
	}
	// For images, also extract EXIF data.
	if mediatype.IsImageExt(path) {
		extractEXIF(path, meta)
	}
	return meta, nil
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
				meta.Width = s.Width
				meta.Height = s.Height
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

func extractEXIF(path string, meta *model.Metadata) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		return
	}

	if v, err := x.Get(exif.Make); err == nil {
		if s, err := v.StringVal(); err == nil {
			meta.EXIFCamera = s
		}
	}
	if v, err := x.Get(exif.Model); err == nil {
		if s, err := v.StringVal(); err == nil {
			if meta.EXIFCamera != "" {
				meta.EXIFCamera = meta.EXIFCamera + " " + s
			} else {
				meta.EXIFCamera = s
			}
		}
	}
	if v, err := x.Get(exif.LensModel); err == nil {
		if s, err := v.StringVal(); err == nil {
			meta.EXIFLens = s
		}
	}
	if v, err := x.Get(exif.DateTimeOriginal); err == nil {
		if s, err := v.StringVal(); err == nil {
			meta.EXIFDate = s
		}
	}
	if v, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if i, err := v.Int(0); err == nil {
			meta.EXIFISO = strconv.Itoa(i)
		}
	}
	if v, err := x.Get(exif.FNumber); err == nil {
		if r, err := v.Rat(0); err == nil {
			num := float64(r.Num().Int64())
			denom := float64(r.Denom().Int64())
			meta.EXIFFNumber = fmt.Sprintf("f/%.1f", num/denom)
		}
	}
	if v, err := x.Get(exif.ExposureTime); err == nil {
		if r, err := v.Rat(0); err == nil {
			meta.EXIFExposure = fmt.Sprintf("%s/%s s", r.Num().String(), r.Denom().String())
		}
	}
	if v, err := x.Get(exif.FocalLength); err == nil {
		if r, err := v.Rat(0); err == nil {
			num := float64(r.Num().Int64())
			denom := float64(r.Denom().Int64())
			meta.EXIFFocalLength = fmt.Sprintf("%.1f mm", num/denom)
		}
	}
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
