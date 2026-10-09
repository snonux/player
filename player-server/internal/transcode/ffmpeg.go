package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/ffsafe"
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

	// Limits of the video rendition: at most 1920x1080 at H.264 level 4.1,
	// which hardware decoders in browsers and on Android universally
	// accept. The encoder enforces them; a stream is only copied when it
	// already satisfies them.
	maxVideoWidth  = 1920
	maxVideoHeight = 1080
	maxH264Level   = 41

	// videoFilter prepares frames for the H.264 encoder:
	//   - yadif deinterlaces only frames flagged as interlaced (old TV/DV
	//     captures in AVI), leaving progressive material untouched;
	//   - scale caps the picture at maxVideoWidth x maxVideoHeight (bounding
	//     CPU time, memory and rendition size for oversized sources) without
	//     ever upscaling, and rounds to even dimensions as yuv420p requires.
	videoFilter = "yadif=deint=interlaced," +
		"scale=w='min(1920,iw)':h='min(1080,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2"

	// noSpaceMessage is what ffmpeg prints when a write fails with ENOSPC.
	noSpaceMessage = "No space left on device"
)

// FFmpegRunner implements Runner with the ffmpeg command-line tool.
type FFmpegRunner struct {
	binary string
	// probeBinary is ffprobe, used to see which streams can be copied and
	// to verify copies. Empty disables stream copy.
	probeBinary string
	// nice is the path of the nice(1) wrapper, empty when unavailable.
	nice string
	// threads caps decoder, filter and encoder threads of one job.
	threads int
}

var _ Runner = (*FFmpegRunner)(nil)

// NewFFmpegRunner creates a Runner that invokes "ffmpeg" from PATH.
//
// maxJobs is the number of jobs that may run in parallel; the CPUs the
// process may actually use are divided among them. runtime.GOMAXPROCS
// reflects a container CPU limit (Go 1.25+), so in a pod limited to one CPU
// a job gets one thread instead of one per core of the node. Jobs also run
// under nice(1) when that is installed.
//
// Memory is not divided: one 1080p job needs roughly 400 MB (decoder,
// filter and x264 frame buffers), so the container memory limit must allow
// that per parallel job on top of the server itself.
func NewFFmpegRunner(maxJobs int) *FFmpegRunner {
	nice, _ := exec.LookPath("nice")
	return &FFmpegRunner{
		binary:      "ffmpeg",
		probeBinary: "ffprobe",
		nice:        nice,
		threads:     max(1, runtime.GOMAXPROCS(0)/max(1, maxJobs)),
	}
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

// Transcode runs ffmpeg and writes the rendition to job.Output.
//
// Streams that already are what the encoder would produce are copied instead
// of re-encoded, which turns minutes into seconds. A copy is only trusted
// after ffprobe confirmed the result; if ffmpeg rejects the copy or the
// result looks wrong, the job is repeated with full encoding.
func (f *FFmpegRunner) Transcode(ctx context.Context, job Job) error {
	input, err := ffsafe.InputArgs(job.Source.Path)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(job.Output)
	if err != nil {
		return fmt.Errorf("resolve transcode output: %w", err)
	}

	src := f.probe(ctx, job.Source.Path)
	if p := planFor(job.Source.Kind, src); p.copyVideo || p.copyAudio {
		// A copied video stream is as large as it is in the source, so a
		// source over the cap cannot produce a rendition under it. Refuse
		// before writing gigabytes only to hit "-fs".
		if p.copyVideo && job.MaxBytes > 0 && fileSize(job.Source.Path) > job.MaxBytes {
			return fmt.Errorf("%w: stream copy of a %d byte source", ErrTooLarge, fileSize(job.Source.Path))
		}
		err := f.run(ctx, f.args(p, input, out, job.MaxBytes))
		if err == nil && f.copyLooksRight(ctx, job.Source.Kind, src, out) {
			return nil
		}
		// A stopped job or a full disk would fail the encode as well.
		if ctx.Err() != nil || errors.Is(err, ErrNoSpace) {
			return err
		}
	}
	return f.run(ctx, f.args(plan{kind: job.Source.Kind}, input, out, job.MaxBytes))
}

// fileSize returns the size of a file, or 0 when it cannot be read.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
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
	err := cmd.Run()
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		// Prefer the context error so callers can tell a stopped job from
		// a file ffmpeg could not convert.
		return fmt.Errorf("ffmpeg transcode: %w", ctx.Err())
	case strings.Contains(stderr.String(), noSpaceMessage):
		return fmt.Errorf("%w: ffmpeg: %s", ErrNoSpace, noSpaceMessage)
	default:
		return fmt.Errorf("ffmpeg transcode: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
}

// plan says which streams of a source can be copied instead of re-encoded.
type plan struct {
	kind      Kind
	copyVideo bool
	copyAudio bool
}

// planFor decides the stream-copy optimisation. A stream is copied only when
// it already meets every guarantee the encode path gives:
//
//   - video: H.264, 8-bit 4:2:0, within the 1080p / level 4.1 cap. AVI is
//     excluded because it stores no presentation timestamps; copying H.264
//     with B-frames out of AVI yields broken timing.
//   - audio: AAC-LC with at most two channels (HE-AAC and multichannel AAC
//     do not decode everywhere and are re-encoded to stereo AAC-LC).
//
// Unknown values (probe failed) never qualify.
func planFor(kind Kind, src mediaInfo) plan {
	plainH264 := src.videoCodec == "h264" && (src.pixFmt == "yuv420p" || src.pixFmt == "yuvj420p")
	withinCap := src.width > 0 && src.width <= maxVideoWidth && src.height > 0 && src.height <= maxVideoHeight &&
		src.level > 0 && src.level <= maxH264Level
	return plan{
		kind:      kind,
		copyVideo: kind == KindVideo && plainH264 && withinCap && !strings.Contains(src.format, "avi"),
		copyAudio: src.audioCodec == "aac" && src.audioProfile == "LC" && src.channels >= 1 && src.channels <= 2,
	}
}

// args builds the argument list for one invocation. input comes from
// ffsafe.InputArgs (see there for the input hardening); out is absolute.
//
// Threads: "-threads" before the input caps the decoder, "-filter_threads"
// the filter graph and "-threads" after the codec the encoder. Capping only
// the encoder leaves decoding and scaling free to use every core.
//
// Output: only the first video/audio stream is kept (subtitle and data
// streams of legacy containers frequently break the MP4 muxer), and both
// maps are optional so an audio-only file with a video extension (.wmv/.asf
// music) still yields an audio rendition; ffmpeg itself fails when neither
// stream exists. "-max_muxing_queue_size" avoids the classic "too many
// packets buffered" failure of badly interleaved AVI/ASF files, "-fs" stops
// the output at the size the cache allows (so a rendition can never fill the
// volume), and "+faststart" moves the moov atom to the front so playback and
// seeking work over HTTP Range without fetching the file tail. The muxer is
// explicit because the cache writes to a temporary name without a usable
// extension.
func (f *FFmpegRunner) args(p plan, input []string, out string, maxBytes int64) []string {
	threads := strconv.Itoa(f.threads)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-threads", threads}
	args = append(args, input...)
	args = append(args, "-sn", "-dn")
	if p.kind == KindAudio {
		args = append(args, "-map", "0:a:0", "-vn")
	} else {
		args = append(args, "-map", "0:v:0?", "-map", "0:a:0?")
		args = append(args, videoCodecArgs(p.copyVideo, threads)...)
	}
	args = append(args, audioCodecArgs(p.copyAudio)...)
	args = append(args, "-max_muxing_queue_size", "1024")
	if maxBytes > 0 {
		args = append(args, "-fs", strconv.FormatInt(maxBytes, 10))
	}
	return append(args, "-movflags", "+faststart", "-f", "mp4", "file:"+out)
}

// videoCodecArgs returns the video encoder settings: H.264 High profile,
// level 4.1, 8-bit 4:2:0. "veryfast" keeps the first playback wait short at
// a modest size cost.
func videoCodecArgs(copyStream bool, threads string) []string {
	if copyStream {
		return []string{"-c:v", "copy"}
	}
	return []string{
		"-filter_threads", threads,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-profile:v", "high", "-level:v", "4.1", "-pix_fmt", "yuv420p",
		"-vf", videoFilter, "-threads", threads,
	}
}

// audioCodecArgs returns the audio encoder settings: stereo AAC-LC, because
// WMA Pro, AC-3 and DTS sources can be multichannel, which not every client
// decodes. 128 kbit/s is transparent enough for stereo AAC and limits how
// much a low-bitrate legacy file (speech in WMA at 32 kbit/s, say) grows.
func audioCodecArgs(copyStream bool) []string {
	if copyStream {
		return []string{"-c:a", "copy"}
	}
	return []string{"-c:a", "aac", "-b:a", "128k", "-ac", "2"}
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
