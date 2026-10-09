// Package probe implements media metadata probing.
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/ffsafe"
	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/model"
	"github.com/rwcarlsen/goexif/exif"
)

const (
	defaultProbeWaitDelay  = 10 * time.Second
	defaultProbeMaxRetries = 2
	defaultProbeRetryDelay = 500 * time.Millisecond
)

// ErrUnreadable reports a file that is not media ffprobe may read: its
// content was refused by the input hardening (see package ffsafe) or could
// not be parsed. It is a property of the file — probing it again gives the
// same answer — unlike a failure to run ffprobe or to reach the file.
var ErrUnreadable = errors.New("not readable as media")

// refusalMessages are the ffprobe diagnostics of a deterministic refusal:
// a demuxer that is not on the whitelist, and content no allowed demuxer
// can make sense of (which includes an empty file and an HLS playlist under
// a media name).
var refusalMessages = []string{
	"not on whitelist",
	"Invalid data found when processing input",
}

// Prober extracts metadata from a media file.
type Prober interface {
	Probe(ctx context.Context, path string) (*model.Metadata, error)
}

// FFProber wraps the ffprobe command-line tool.
type FFProber struct {
	maxRetries int
	retryDelay time.Duration
	waitDelay  time.Duration
}

var _ Prober = (*FFProber)(nil)

// NewFFProber creates a new FFProber with bounded retries and a process wait delay.
func NewFFProber() *FFProber {
	return &FFProber{
		maxRetries: defaultProbeMaxRetries,
		retryDelay: defaultProbeRetryDelay,
		waitDelay:  defaultProbeWaitDelay,
	}
}

// Probe runs ffprobe against the given path and parses the resulting JSON.
// A failure that may be transient (ffprobe could not run, the file could not
// be reached) is retried a bounded number of times; a file that is
// ErrUnreadable is not, and neither is a cancelled context.
func (f *FFProber) Probe(ctx context.Context, path string) (*model.Metadata, error) {
	var lastErr error
	attempts := f.maxRetries + 1
	if attempts <= 0 {
		attempts = 1
	}

	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		meta, err := f.probeOnce(ctx, path)
		if err == nil {
			return meta, nil
		}
		lastErr = err

		// Don't retry on context cancellation, nor a file whose content
		// was refused: waiting will not change it.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, ErrUnreadable) {
			break
		}

		if i < attempts-1 {
			delay := f.retryDelay * time.Duration(1<<i)
			const maxDelay = 30 * time.Second
			if delay > maxDelay {
				delay = maxDelay
			}
			// Use time.NewTimer instead of time.After so the underlying
			// timer is released promptly when ctx.Done fires first,
			// rather than leaking until the delay elapses.
			t := time.NewTimer(delay)
			select {
			case <-t.C:
				// Retry path: timer fired naturally, no Stop needed.
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}

// probeArgs builds the ffprobe argument list for path. The file is opened
// through ffsafe.SourceArgs, never by its bare name: ffprobe would otherwise
// follow a playlist disguised as media (or an image sequence pattern in the
// name) and report the metadata of other files.
func probeArgs(path string) ([]string, error) {
	input, err := ffsafe.SourceArgs(path)
	if err != nil {
		return nil, err
	}
	return append([]string{
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
	}, input...), nil
}

// probeOnce performs a single ffprobe invocation.
func (f *FFProber) probeOnce(ctx context.Context, path string) (*model.Metadata, error) {
	args, err := probeArgs(path)
	if errors.Is(err, ffsafe.ErrNotAnImage) {
		return nil, fmt.Errorf("ffprobe %s: %w: %w", path, ErrUnreadable, err)
	}
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.WaitDelay = f.waitDelay
	out, err := cmd.Output()
	if err != nil {
		return nil, probeFailure(path, err)
	}
	audioFile := mediatype.TypeForExt(path) == model.MediaTypeAudio
	meta, err := parseFFprobeOutput(out, audioFile)
	if err != nil {
		return nil, err
	}
	// For images, also extract EXIF data.
	if mediatype.IsImageExt(path) {
		extractEXIF(path, meta)
	}
	return meta, nil
}

// probeFailure turns a failed ffprobe run into an error carrying ffprobe's
// diagnostics, marked ErrUnreadable when they say the file itself was
// refused (see refusalMessages).
func probeFailure(path string, err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || len(exitErr.Stderr) == 0 {
		return fmt.Errorf("ffprobe %s: %w", path, err)
	}
	stderr := string(exitErr.Stderr)
	for _, message := range refusalMessages {
		if strings.Contains(stderr, message) {
			return fmt.Errorf("ffprobe %s: %w: %w: %s", path, ErrUnreadable, err, stderr)
		}
	}
	return fmt.Errorf("ffprobe %s: %w: %s", path, err, stderr)
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

// parseFFprobeOutput turns ffprobe's JSON into metadata. audioFile says the
// file is audio by its extension, which decides how the codec is recorded
// (see codecString).
func parseFFprobeOutput(data []byte, audioFile bool) (*model.Metadata, error) {
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

	var videoCodec, audioCodec string
	for _, s := range out.Streams {
		switch {
		case s.CodecType == "video" && videoCodec == "":
			videoCodec = s.CodecName
			if s.Width > 0 && s.Height > 0 {
				meta.Resolution = fmt.Sprintf("%dx%d", s.Width, s.Height)
				meta.Width = s.Width
				meta.Height = s.Height
			}
		case s.CodecType == "audio" && audioCodec == "":
			audioCodec = s.CodecName
		}
	}
	meta.Codec = codecString(videoCodec, audioCodec, audioFile)

	// Fallback to first stream codec if neither a video nor an audio stream
	// was identified.
	if meta.Codec == "" && len(out.Streams) > 0 {
		meta.Codec = out.Streams[0].CodecName
	}

	return meta, nil
}

// codecString builds the stored codec value from the first video and first
// audio stream.
//
// Video files get "video/audio" when both exist, otherwise the one present.
// The audio codec is recorded because playback decisions need it: H.264
// video with WMA or AC-3 audio plays silently (or not at all) in clients,
// which only the audio codec reveals. Rows probed before this was recorded
// hold the video codec alone; consumers treat a missing audio part as
// unknown.
//
// Audio files get the audio codec only. Their "video" stream is embedded
// cover art (mjpeg, png), which is not what the file is encoded in and must
// not show up as its codec in the UI or in playback decisions.
func codecString(video, audio string, audioFile bool) string {
	if audioFile && audio != "" {
		return audio
	}
	if video != "" && audio != "" {
		return video + "/" + audio
	}
	return video + audio
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
