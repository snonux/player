package transcode

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"

	"codeberg.org/snonux/player/internal/ffsafe"
)

// mediaInfo describes the container and the first video and first audio
// stream of a file — the streams the rendition maps. Zero values mean
// "unknown" and never qualify a stream for copying.
type mediaInfo struct {
	format   string // ffprobe format_name, e.g. "avi" or "matroska,webm"
	duration float64

	hasVideo   bool
	videoCodec string
	pixFmt     string
	width      int
	height     int
	level      int // H.264 level times ten, e.g. 41

	hasAudio     bool
	audioCodec   string
	audioProfile string // e.g. "LC", "HE-AAC"
	channels     int
}

// probe asks ffprobe what a file really contains. It reads the file instead
// of trusting the codec stored in the database, which may be stale. The file
// is opened with the same hardening as for ffmpeg. Any failure yields the
// zero mediaInfo, which simply means "re-encode everything".
func (f *FFmpegRunner) probe(ctx context.Context, path string) mediaInfo {
	if f.probeBinary == "" {
		return mediaInfo{}
	}
	input, err := ffsafe.InputArgs(path)
	if err != nil {
		return mediaInfo{}
	}
	args := append([]string{
		"-v", "error", "-of", "json", "-show_entries",
		"stream=codec_type,codec_name,profile,pix_fmt,width,height,level,channels:format=format_name,duration",
	}, input...)
	cmd := exec.CommandContext(ctx, f.probeBinary, args...)
	cmd.WaitDelay = ffmpegWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return mediaInfo{}
	}
	return parseMediaInfo(out)
}

// parseMediaInfo extracts the container and the first video and audio stream
// from ffprobe's JSON.
func parseMediaInfo(data []byte) mediaInfo {
	var parsed struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Profile   string `json:"profile"`
			PixFmt    string `json:"pix_fmt"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Level     int    `json:"level"`
			Channels  int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return mediaInfo{}
	}
	info := mediaInfo{format: parsed.Format.FormatName}
	info.duration, _ = strconv.ParseFloat(parsed.Format.Duration, 64)
	for _, s := range parsed.Streams {
		switch {
		case s.CodecType == "video" && !info.hasVideo:
			info.hasVideo = true
			info.videoCodec, info.pixFmt = s.CodecName, s.PixFmt
			info.width, info.height, info.level = s.Width, s.Height, s.Level
		case s.CodecType == "audio" && !info.hasAudio:
			info.hasAudio = true
			info.audioCodec, info.audioProfile, info.channels = s.CodecName, s.Profile, s.Channels
		}
	}
	return info
}

// copyLooksRight verifies a rendition produced with stream copy: ffmpeg can
// exit successfully and still leave an MP4 without a stream it could not
// carry over, or one without usable timing. The output must contain every
// stream the source offers for this kind and have a duration.
func (f *FFmpegRunner) copyLooksRight(ctx context.Context, kind Kind, src mediaInfo, out string) bool {
	got := f.probe(ctx, out)
	if got.duration <= 0 {
		return false
	}
	if kind == KindVideo && src.hasVideo && !got.hasVideo {
		return false
	}
	return !src.hasAudio || got.hasAudio
}
