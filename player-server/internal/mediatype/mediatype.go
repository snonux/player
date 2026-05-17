// Package mediatype provides a single source of truth for mapping file
// extensions to media types, supported-extension checks, and MIME types.
package mediatype

import (
	"mime"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// --- extension sets -------------------------------------------------

var (
	videoExts = map[string]struct{}{
		".mp4": {}, ".mkv": {}, ".avi": {}, ".mov": {}, ".wmv": {}, ".flv": {}, ".webm": {},
	}

	audioExts = map[string]struct{}{
		".mp3": {}, ".wav": {}, ".flac": {}, ".aac": {}, ".ogg": {}, ".m4a": {}, ".wma": {}, ".m4b": {}, ".opus": {},
	}

	imageExts = map[string]struct{}{
		".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {}, ".bmp": {}, ".avif": {}, ".svg": {},
	}

	coverImageExts = map[string]struct{}{
		".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {},
	}
)

// TypeForExt returns the media type (video, audio, image) for a file name.
// If the extension is not recognized it falls back to video so that unknown
// uploads and scanned files are treated consistently as playable media.
func TypeForExt(name string) model.MediaType {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case isInSet(videoExts, ext):
		return model.MediaTypeVideo
	case isInSet(audioExts, ext):
		return model.MediaTypeAudio
	case isInSet(imageExts, ext):
		return model.MediaTypeImage
	default:
		return model.MediaTypeVideo
	}
}

// IsSupportedExt reports whether the file extension is a known media type.
func IsSupportedExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return isInSet(videoExts, ext) || isInSet(audioExts, ext) || isInSet(imageExts, ext)
}

// IsImageExt reports whether the file extension is a recognized image type.
func IsImageExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return isInSet(imageExts, ext)
}

// IsCoverImageExt reports whether the file extension is a recognized cover-art
// image type.  This is a subset of IsImageExt used by the scanner when looking
// for folder artwork.
func IsCoverImageExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return isInSet(coverImageExts, ext)
}

// MIMETypeForExt returns an HTTP Content-Type for a file name.
// It first consults the OS mime database, then falls back to a hard-coded
// mapping for known media extensions, and finally returns
// application/octet-stream.
func MIMETypeForExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	switch ext {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".flv":
		return "video/x-flv"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".wav":
		return "audio/wav"
	case ".aac", ".m4a":
		return "audio/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".m4b":
		return "audio/x-m4b"
	case ".wma":
		return "audio/x-ms-wma"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".avif":
		return "image/avif"
	case ".svg":
		return "image/svg+xml"
	}
	return "application/octet-stream"
}

func isInSet(set map[string]struct{}, ext string) bool {
	_, ok := set[ext]
	return ok
}
