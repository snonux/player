package ffsafe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNotAnImage reports a file with an image extension whose content is
// none of the supported image formats. It is a property of the file, not a
// transient failure: retrying cannot help.
var ErrNotAnImage = errors.New("content is not a supported image format")

// sniffLen is how much of a file is read to recognise its format. Every
// binary signature sits in the first 12 bytes; the rest is for SVG, whose
// root element follows an XML declaration, a doctype and comments of any
// length.
const sniffLen = 4096

// imageFormat ties the leading bytes of an image file to the ffmpeg demuxer
// that reads it.
type imageFormat struct {
	demuxer string
	matches func(head []byte) bool
}

// imageFormats returns the supported still-image formats, one per image
// extension group in package mediatype. Every demuxer named here reads only
// the bytes of the file it is given: the "*_pipe" demuxers and gif parse a
// byte stream, and mov (AVIF is an ISO-BMFF file) does not follow external
// data references. image2 must never be added: it expands "%d" patterns in
// the file NAME and reads other files.
//
// The names exist in ffmpeg 6.1 (alpine:3.21, the runtime image) and 8.1
// alike. A signature only selects the demuxer; a wrong guess makes ffmpeg
// fail on that one file and cannot make it read another.
func imageFormats() []imageFormat {
	return []imageFormat{
		{"jpeg_pipe", hasPrefix("\xff\xd8\xff")},
		{"png_pipe", hasPrefix("\x89PNG\r\n\x1a\n")},
		{"gif", func(h []byte) bool { return hasPrefix("GIF87a")(h) || hasPrefix("GIF89a")(h) }},
		{"webp_pipe", func(h []byte) bool { return hasPrefix("RIFF")(h) && hasAt(h, 8, "WEBP") }},
		{"bmp_pipe", hasPrefix("BM")},
		{"mov", func(h []byte) bool { return hasAt(h, 4, "ftyp") }},
		{"svg_pipe", looksLikeSVG},
	}
}

// ImageDemuxers returns the names of all demuxers ImageInputArgs can force,
// for tests and diagnostics.
func ImageDemuxers() []string {
	formats := imageFormats()
	names := make([]string, len(formats))
	for i, f := range formats {
		names[i] = f.demuxer
	}
	return names
}

// imageDemuxer reads the start of the file at path and returns the demuxer
// for its format, or ErrNotAnImage.
func imageDemuxer(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open image: %w", err)
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, sniffLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read image: %w", err)
	}
	head = head[:n]
	for _, format := range imageFormats() {
		if format.matches(head) {
			return format.demuxer, nil
		}
	}
	return "", fmt.Errorf("%s: %w", path, ErrNotAnImage)
}

// hasPrefix returns a matcher for a signature at the start of the file.
func hasPrefix(signature string) func([]byte) bool {
	return func(head []byte) bool { return hasAt(head, 0, signature) }
}

// hasAt reports whether signature sits at offset in head.
func hasAt(head []byte, offset int, signature string) bool {
	return len(head) >= offset+len(signature) && string(head[offset:offset+len(signature)]) == signature
}

// looksLikeSVG reports whether head is the start of an XML document with an
// <svg> element: markup from the first character on (after an optional
// byte-order mark and whitespace) and the root element within the sniffed
// part.
func looksLikeSVG(head []byte) bool {
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	head = bytes.TrimLeft(head, " \t\r\n")
	return bytes.HasPrefix(head, []byte("<")) && bytes.Contains(head, []byte("<svg"))
}
