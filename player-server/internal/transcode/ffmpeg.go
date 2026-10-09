package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// ffmpegWaitDelay bounds how long we wait for ffmpeg to exit after its
	// context is cancelled before the process is killed.
	ffmpegWaitDelay = 10 * time.Second
	// stderrTailBytes caps how much ffmpeg diagnostics end up in an error.
	stderrTailBytes = 2048
	// niceness lowers ffmpeg's CPU priority so transcodes never starve the
	// HTTP server that shares the container.
	niceness = "10"

	// videoFilter prepares frames for the H.264 encoder:
	//   - yadif deinterlaces only frames flagged as interlaced (old TV/DV
	//     captures in AVI), leaving progressive material untouched;
	//   - scale caps the picture at 1920x1080 (bounding CPU time, memory and
	//     rendition size for oversized sources, and keeping the stream
	//     inside H.264 level 4.1) without ever upscaling, and rounds to even
	//     dimensions as yuv420p requires.
	videoFilter = "yadif=deint=interlaced," +
		"scale=w='min(1920,iw)':h='min(1080,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2"
)

// FFmpegRunner implements Runner with the ffmpeg command-line tool.
type FFmpegRunner struct {
	binary string
	// probeBinary is ffprobe, used to see which streams can be copied.
	probeBinary string
	// nice is the path of the nice(1) wrapper, empty when unavailable.
	nice string
	// threads caps the encoder threads of one job.
	threads int
}

var _ Runner = (*FFmpegRunner)(nil)

// NewFFmpegRunner creates a Runner that invokes "ffmpeg" from PATH. Each job
// gets half of the CPUs (at least one) and runs under nice(1) when that is
// installed, so parallel jobs and the HTTP server share the machine.
func NewFFmpegRunner() *FFmpegRunner {
	nice, _ := exec.LookPath("nice")
	return &FFmpegRunner{binary: "ffmpeg", probeBinary: "ffprobe", nice: nice, threads: max(1, runtime.NumCPU()/2)}
}

// Available reports whether the ffmpeg binary can be found, for a startup
// check that turns a missing tool into a log line instead of a failed
// playback later.
func (f *FFmpegRunner) Available() error {
	if _, err := exec.LookPath(f.binary); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	return nil
}

// Transcode runs ffmpeg and writes the rendition to outputPath.
//
// When the source already carries H.264 video or AAC audio those streams are
// copied instead of re-encoded, which turns minutes into seconds. A copy can
// still fail on streams the MP4 muxer does not accept, so a failed copy
// attempt is repeated with full encoding.
func (f *FFmpegRunner) Transcode(ctx context.Context, src Source, outputPath string) error {
	demuxer, ok := demuxerFor(src.Path)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnsupportedInput, filepath.Ext(src.Path))
	}
	in, err := filepath.Abs(src.Path)
	if err != nil {
		return fmt.Errorf("resolve transcode input: %w", err)
	}
	out, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve transcode output: %w", err)
	}

	p := planFor(src.Kind, demuxer, f.probe(ctx, demuxer, in))
	err = f.run(ctx, ffmpegArgs(p, demuxer, in, out, f.threads))
	if err != nil && (p.copyVideo || p.copyAudio) && ctx.Err() == nil {
		err = f.run(ctx, ffmpegArgs(plan{kind: src.Kind}, demuxer, in, out, f.threads))
	}
	return err
}

// run executes one ffmpeg invocation.
func (f *FFmpegRunner) run(ctx context.Context, args []string) error {
	name := f.binary
	if f.nice != "" {
		name, args = f.nice, append([]string{"-n", niceness, f.binary}, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	stderr := &tailBuffer{limit: stderrTailBytes}
	cmd.Stderr = stderr
	cmd.WaitDelay = ffmpegWaitDelay
	if err := cmd.Run(); err != nil {
		// Prefer the context error so callers can tell a stopped job from
		// a file ffmpeg could not convert.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("ffmpeg transcode: %w", ctxErr)
		}
		return fmt.Errorf("ffmpeg transcode: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// streams describes the first video and first audio stream of a source.
type streams struct {
	videoCodec string
	pixFmt     string
	audioCodec string
}

// probe asks ffprobe what the source really contains. It reads the file
// instead of trusting the codec stored in the database, which may be stale
// or lack the audio codec. The input is opened with the same hardening as
// for ffmpeg (pinned demuxer, file protocol only). Any failure yields empty
// streams, which simply means "re-encode everything".
func (f *FFmpegRunner) probe(ctx context.Context, demuxer, in string) streams {
	if f.probeBinary == "" {
		return streams{}
	}
	cmd := exec.CommandContext(ctx, f.probeBinary,
		"-v", "error", "-protocol_whitelist", "file", "-f", demuxer,
		"-show_entries", "stream=codec_type,codec_name,pix_fmt", "-of", "json", "file:"+in,
	)
	cmd.WaitDelay = ffmpegWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return streams{}
	}
	return parseStreams(out)
}

// parseStreams extracts the first video and audio stream from ffprobe's JSON.
func parseStreams(data []byte) streams {
	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			PixFmt    string `json:"pix_fmt"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return streams{}
	}
	var st streams
	for _, s := range parsed.Streams {
		switch {
		case s.CodecType == "video" && st.videoCodec == "":
			st.videoCodec, st.pixFmt = s.CodecName, s.PixFmt
		case s.CodecType == "audio" && st.audioCodec == "":
			st.audioCodec = s.CodecName
		}
	}
	return st
}

// plan says which streams of a source can be copied instead of re-encoded.
type plan struct {
	kind      Kind
	copyVideo bool
	copyAudio bool
}

// planFor decides the stream-copy optimisation.
//
// Video is copied only when it already is exactly what the encoder would
// produce for clients: H.264 with 8-bit 4:2:0 pixels (10-bit or 4:2:2 H.264
// does not decode in browsers and is re-encoded). AVI is excluded because it
// stores no presentation timestamps; copying H.264 with B-frames out of AVI
// yields broken timing. Audio is copied when it already is AAC.
func planFor(kind Kind, demuxer string, st streams) plan {
	plainH264 := st.videoCodec == "h264" && (st.pixFmt == "yuv420p" || st.pixFmt == "yuvj420p")
	return plan{
		kind:      kind,
		copyVideo: kind == KindVideo && plainH264 && demuxer != "avi",
		copyAudio: st.audioCodec == "aac",
	}
}

// demuxerFor maps a file extension to the ffmpeg demuxer that must be used.
//
// The demuxer is never auto-detected: ffmpeg probes content, so a file named
// evil.avi that actually holds an ffconcat or HLS playlist would otherwise
// make ffmpeg open the files the playlist names — reading media the uploader
// has no permission for (or any file ffmpeg can parse) into the rendition.
// With a pinned demuxer such a file simply fails to parse.
func demuxerFor(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".avi":
		return "avi", true
	case ".wmv", ".wma", ".asf":
		return "asf", true
	case ".flv":
		return "flv", true
	case ".mkv", ".webm":
		return "matroska", true
	case ".mp4", ".mov", ".m4a", ".m4b":
		return "mov", true
	case ".mp3":
		return "mp3", true
	case ".ogg", ".opus":
		return "ogg", true
	case ".flac":
		return "flac", true
	case ".wav":
		return "wav", true
	case ".aac":
		return "aac", true
	}
	return "", false
}

// ffmpegArgs builds the argument list for one invocation. in and out must be
// absolute paths.
//
// Input hardening: besides the pinned demuxer, "-protocol_whitelist file"
// stops any demuxer from opening network or pipe resources, and the "file:"
// prefix makes ffmpeg treat the name literally, so names starting with "-"
// or containing ":" cannot be read as options or protocols.
//
// Output: only the first video/audio stream is kept (subtitle and data
// streams of legacy containers frequently break the MP4 muxer), and both
// maps are optional so an audio-only file with a video extension (.wmv/.asf
// music) still yields an audio rendition; ffmpeg itself fails when neither
// stream exists. "-max_muxing_queue_size" avoids the classic "too many
// packets buffered" failure of badly interleaved AVI/ASF files, and
// "+faststart" moves the moov atom to the front so playback and seeking work
// over HTTP Range without fetching the file tail. The muxer is explicit
// because the cache writes to a temporary name without a usable extension.
func ffmpegArgs(p plan, demuxer, in, out string, threads int) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file", "-f", demuxer, "-i", "file:" + in,
		"-sn", "-dn",
	}
	if p.kind == KindAudio {
		args = append(args, "-map", "0:a:0", "-vn")
	} else {
		args = append(args, "-map", "0:v:0?", "-map", "0:a:0?")
		args = append(args, videoCodecArgs(p.copyVideo, threads)...)
	}
	args = append(args, audioCodecArgs(p.copyAudio)...)
	return append(args,
		"-max_muxing_queue_size", "1024",
		"-movflags", "+faststart", "-f", "mp4", "file:"+out,
	)
}

// videoCodecArgs returns the video encoder settings: H.264 High profile,
// level 4.1, 8-bit 4:2:0 — the combination hardware decoders in browsers and
// on Android universally accept. "veryfast" keeps the first playback wait
// short at a modest size cost.
func videoCodecArgs(copyStream bool, threads int) []string {
	if copyStream {
		return []string{"-c:v", "copy"}
	}
	return []string{
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-profile:v", "high", "-level:v", "4.1", "-pix_fmt", "yuv420p",
		"-vf", videoFilter, "-threads", strconv.Itoa(threads),
	}
}

// audioCodecArgs returns the audio encoder settings: stereo AAC, because
// WMA Pro, AC-3 and DTS sources can be multichannel, which not every client
// decodes.
func audioCodecArgs(copyStream bool) []string {
	if copyStream {
		return []string{"-c:a", "copy"}
	}
	return []string{"-c:a", "aac", "-b:a", "160k", "-ac", "2"}
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
