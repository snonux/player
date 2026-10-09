// Package ffsafe builds the input arguments every ffmpeg/ffprobe invocation
// on user-supplied media must use.
//
// ffmpeg picks the demuxer by looking at file content, not at the file name.
// A file called holiday.avi that actually contains an ffconcat (or similar)
// playlist therefore makes ffmpeg open the files the playlist names, and
// their content ends up in the output — media of another set, or any other
// file ffmpeg can parse. The arguments built here close that hole while
// still accepting media whose extension does not match its real container.
package ffsafe

import (
	"fmt"
	"path/filepath"
)

// FormatWhitelist lists the demuxers allowed to open user media: the real
// containers of the supported file types and nothing that can reference
// other files (concat, hls, dash, image2 sequences, tty, lavfi, ...).
// ffmpeg matches each entry against any of a demuxer's comma-separated
// names, so "matroska" covers "matroska,webm" and "mov" covers
// "mov,mp4,m4a,3gp,3g2,mj2".
const FormatWhitelist = "avi,asf,flv,matroska,mov,mp3,ogg,flac,wav,aac"

// InputArgs returns the arguments that open path as input, for ffmpeg and
// ffprobe alike. Append them where "-i <path>" would go.
//
//   - "-format_whitelist" rejects any demuxer not on FormatWhitelist, so a
//     playlist disguised as media fails with "Format not on whitelist". The
//     container is still auto-detected among the allowed ones, which keeps a
//     mislabeled but genuine file (an MP4 named .flv) working.
//   - "-protocol_whitelist file" stops demuxers from opening network, pipe
//     or other non-file resources.
//   - the "file:" prefix with an absolute path makes ffmpeg take the name
//     literally: names starting with "-" or containing ":" or "%" cannot be
//     read as options, protocols or sequence patterns.
func InputArgs(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve ffmpeg input path: %w", err)
	}
	return []string{
		"-protocol_whitelist", "file",
		"-format_whitelist", FormatWhitelist,
		"-i", "file:" + abs,
	}, nil
}
