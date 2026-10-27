package probe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/snonux/player/internal/ffsafe"
)

// maxFormatOutputBytes bounds ffprobe's answer: stream entries only, no tags.
const maxFormatOutputBytes = 64 << 10

// AudioFormat describes how the first audio stream of a file is encoded.
// Zero values mean ffprobe did not report the property.
type AudioFormat struct {
	Codec         string `json:"codec,omitempty"`
	CodecLabel    string `json:"codec_label,omitempty"`
	Profile       string `json:"profile,omitempty"`
	Bitrate       int    `json:"bitrate,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	BitDepth      int    `json:"bit_depth,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	Lossless      bool   `json:"lossless"`
}

func formatArgs(path string) ([]string, error) {
	input, err := ffsafe.InputArgs(path)
	if err != nil {
		return nil, err
	}
	return append([]string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries",
		"format=bit_rate:stream=codec_name,profile,sample_rate,channels,channel_layout,bits_per_sample,bits_per_raw_sample,bit_rate",
		"-of", "json",
	}, input...), nil
}

// ReadAudioFormat reports the codec and quality of the first audio stream,
// with bounded output and runtime. A file without an audio stream gives a
// zero AudioFormat. The caller must validate that path is a regular file
// inside its media root.
func ReadAudioFormat(ctx context.Context, path string) (AudioFormat, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	args, err := formatArgs(path)
	if err != nil {
		return AudioFormat{}, err
	}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return AudioFormat{}, err
	}
	// Diagnostics are intentionally discarded: they may contain local paths.
	if err := cmd.Start(); err != nil {
		return AudioFormat{}, err
	}
	out, readErr := io.ReadAll(io.LimitReader(stdout, maxFormatOutputBytes+1))
	if len(out) > maxFormatOutputBytes {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	switch {
	case len(out) > maxFormatOutputBytes:
		return AudioFormat{}, errors.New("format metadata too large")
	case readErr != nil:
		return AudioFormat{}, readErr
	case waitErr != nil:
		return AudioFormat{}, waitErr
	}
	return parseAudioFormat(out)
}

func parseAudioFormat(data []byte) (AudioFormat, error) {
	var result struct {
		Format struct {
			BitRate string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			CodecName        string `json:"codec_name"`
			Profile          string `json:"profile"`
			SampleRate       string `json:"sample_rate"`
			Channels         int    `json:"channels"`
			ChannelLayout    string `json:"channel_layout"`
			BitsPerSample    int    `json:"bits_per_sample"`
			BitsPerRawSample string `json:"bits_per_raw_sample"`
			BitRate          string `json:"bit_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return AudioFormat{}, err
	}
	if len(result.Streams) == 0 {
		return AudioFormat{}, nil
	}
	s := result.Streams[0]
	format := AudioFormat{
		Codec:         s.CodecName,
		CodecLabel:    codecLabel(s.CodecName),
		SampleRate:    positiveInt(s.SampleRate),
		Channels:      max(s.Channels, 0),
		ChannelLayout: s.ChannelLayout,
		Lossless:      losslessCodec(s.CodecName),
	}
	if s.Profile != "" && !strings.EqualFold(s.Profile, "unknown") {
		format.Profile = s.Profile
		// HE-AAC sounds different enough from AAC-LC to name it.
		if s.CodecName == "aac" && strings.HasPrefix(s.Profile, "HE-AAC") {
			format.CodecLabel = s.Profile
		}
	}
	// FLAC and ALAC report their depth as bits_per_raw_sample, PCM as
	// bits_per_sample. Lossy codecs decode to float and have no depth.
	if format.Lossless {
		format.BitDepth = positiveInt(s.BitsPerRawSample)
		if format.BitDepth == 0 {
			format.BitDepth = max(s.BitsPerSample, 0)
		}
	}
	// Containers such as FLAC and Ogg often omit the stream rate; the
	// container's average is the next best answer.
	format.Bitrate = positiveInt(s.BitRate)
	if format.Bitrate == 0 {
		format.Bitrate = positiveInt(result.Format.BitRate)
	}
	return format, nil
}

func positiveInt(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

var codecLabels = map[string]string{
	"aac":         "AAC",
	"ac3":         "AC-3",
	"alac":        "ALAC",
	"amr_nb":      "AMR-NB",
	"amr_wb":      "AMR-WB",
	"ape":         "APE",
	"dts":         "DTS",
	"eac3":        "E-AC-3",
	"flac":        "FLAC",
	"mp2":         "MP2",
	"mp3":         "MP3",
	"mp3float":    "MP3",
	"opus":        "Opus",
	"speex":       "Speex",
	"truehd":      "TrueHD",
	"tta":         "TTA",
	"vorbis":      "Vorbis",
	"wavpack":     "WavPack",
	"wmalossless": "WMA Lossless",
	"wmapro":      "WMA Pro",
	"wmav1":       "WMA",
	"wmav2":       "WMA",
}

func codecLabel(codec string) string {
	if label, ok := codecLabels[codec]; ok {
		return label
	}
	switch {
	case strings.HasPrefix(codec, "pcm_"):
		return "PCM"
	case strings.HasPrefix(codec, "dsd_"):
		return "DSD"
	}
	return strings.ToUpper(codec)
}

func losslessCodec(codec string) bool {
	switch codec {
	case "flac", "alac", "ape", "wavpack", "tta", "truehd", "mlp", "wmalossless":
		return true
	}
	return strings.HasPrefix(codec, "pcm_") || strings.HasPrefix(codec, "dsd_")
}
