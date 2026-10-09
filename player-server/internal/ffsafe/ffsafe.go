// Package ffsafe builds the input and output arguments every ffmpeg/ffprobe
// invocation on user-supplied media must use. It is the one place that
// defines which demuxers may open such files.
//
// ffmpeg picks the demuxer by looking at file content (and, for still
// images, at the file name), not at what the server believes the file to
// be. A file called holiday.avi that actually contains an ffconcat or HLS
// playlist therefore makes ffmpeg open the files the playlist names, and
// their content ends up in the output — a frame or the metadata of media in
// another set, or of any other file ffmpeg can parse. The arguments built
// here close that hole while still accepting media whose extension does not
// match its real container.
//
// Use SourceArgs for a file of any supported type (probing, thumbnails),
// InputArgs where only audio/video makes sense (transcoding, remuxing), and
// OutputArg for a file ffmpeg writes.
package ffsafe

import (
	"fmt"
	"path/filepath"

	"codeberg.org/snonux/player/internal/mediatype"
)

// FormatWhitelist lists the demuxers allowed to open audio and video files:
// the real containers of the supported file types and nothing that can
// reference other files (concat, hls, dash, image2 sequences, tty, lavfi,
// ...). ffmpeg matches each entry against any of a demuxer's comma-separated
// names, so "matroska" covers "matroska,webm" and "mov" covers
// "mov,mp4,m4a,3gp,3g2,mj2". A name a given ffmpeg build does not know is
// harmless: the list is only compared with the demuxer that was detected.
//
// mpegts has no extension of its own among the supported types; it is here
// because MPEG-TS recordings named .mp4 are common enough that the server
// remuxes them on the fly (see probe.LooksLikeMPEGTS), and they must stay
// probeable.
//
// Still images are not on this list on purpose; see ImageInputArgs.
const FormatWhitelist = "avi,asf,flv,matroska,mov,mp3,ogg,flac,wav,aac,mpegts"

// SourceArgs returns the arguments that open path as input, whatever
// supported media type it is: ImageInputArgs for a file with an image
// extension, InputArgs for everything else. The extension decides, exactly
// as it decides the media type everywhere else in the server; the content
// then has to be what that type allows, or the call fails.
func SourceArgs(path string) ([]string, error) {
	if mediatype.IsImageExt(path) {
		return ImageInputArgs(path)
	}
	return InputArgs(path)
}

// InputArgs returns the arguments that open the audio or video file path as
// input, for ffmpeg and ffprobe alike. Append them where "-i <path>" would
// go.
//
//   - "-format_whitelist" rejects any demuxer not on FormatWhitelist, so a
//     playlist disguised as media fails with "Format not on whitelist". The
//     container is still auto-detected among the allowed ones, which keeps a
//     mislabeled but genuine file (an MP4 named .flv) working.
//   - "-protocol_whitelist file" stops demuxers from opening network, pipe
//     or other non-file resources.
//   - the "file:" prefix with an absolute path makes ffmpeg take the name
//     literally: names starting with "-" or containing ":" cannot be read
//     as options or protocols.
func InputArgs(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve ffmpeg input path: %w", err)
	}
	return inputArgs(FormatWhitelist, abs), nil
}

// ImageInputArgs returns the arguments that open the still image path as
// input. Unlike InputArgs it does not let ffmpeg detect the format: the
// demuxer is chosen here from the file's first bytes (see imageDemuxer) and
// forced with "-f", and the whitelist holds that one demuxer.
//
// Auto-detection cannot be made safe for images. For a name with an image
// extension ffmpeg prefers the image2 demuxer — always for JPEG, and for
// every type once the name contains a "%d"-style pattern — and image2 treats
// the NAME as a sequence pattern: "a%03d.png" makes it read the sibling
// files a000.png, a001.png, ... instead. The "file:" prefix does not stop
// that. The demuxers forced here read the bytes of the one opened file and
// nothing else, so a pattern in the name is taken literally.
//
// A file that is none of the supported image formats (a playlist named
// .jpg, for instance) fails with ErrNotAnImage before ffmpeg is started.
func ImageInputArgs(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve ffmpeg input path: %w", err)
	}
	demuxer, err := imageDemuxer(abs)
	if err != nil {
		return nil, err
	}
	return append([]string{"-f", demuxer}, inputArgs(demuxer, abs)...), nil
}

// OutputArg returns the argument naming path as an ffmpeg output file: the
// absolute path behind "file:", so that a name starting with "-" or
// containing ":" is not read as an option or a protocol. It does not switch
// off "%d" expansion by the image muxer; callers writing a single image add
// "-update 1" for that. (The transcoder, which also needs the absolute path
// to inspect its output, builds the same argument itself.)
func OutputArg(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve ffmpeg output path: %w", err)
	}
	return "file:" + abs, nil
}

// inputArgs assembles the hardened input options for an absolute path.
func inputArgs(formats, abs string) []string {
	return []string{
		"-protocol_whitelist", "file",
		"-format_whitelist", formats,
		"-i", "file:" + abs,
	}
}
