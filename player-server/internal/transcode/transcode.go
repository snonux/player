// Package transcode produces and caches browser/ExoPlayer-compatible
// renditions of media whose original container or codec cannot be decoded by
// the clients (AVI, WMV, FLV, WMA, ...).
//
// A rendition is a complete file on disk rather than a live pipe so the API
// layer can serve it with http.ServeContent and clients can seek with HTTP
// Range requests.
package transcode

import (
	"context"
	"errors"
)

// Kind selects the rendition profile.
type Kind int

const (
	// KindVideo produces H.264 video with AAC audio in an MP4 container.
	KindVideo Kind = iota
	// KindAudio produces AAC audio in an MP4 (M4A) container.
	KindAudio
)

// profileVersion is part of every rendition file name. Bump it whenever the
// ffmpeg arguments change so renditions made with the old profile stop being
// served and are eventually pruned.
const profileVersion = "v1"

var (
	// ErrPending reports that the rendition is still being produced. The
	// transcode keeps running in the background; callers should retry.
	ErrPending = errors.New("transcode in progress")
	// ErrSourceMissing reports that the source media file cannot be read.
	ErrSourceMissing = errors.New("transcode source missing")
)

// Source identifies the original media file a rendition is derived from.
type Source struct {
	MediaID int64
	Path    string
	Kind    Kind
}

// Rendition describes a finished, cached compatibility file.
type Rendition struct {
	// Path is the absolute location of the rendition inside the cache.
	Path string
	// ContentType is the HTTP Content-Type of the rendition.
	ContentType string
	// ETag is stable for a given source version and profile, so clients can
	// keep using If-Range across requests even though the cache updates the
	// file's mtime on every access for LRU bookkeeping.
	ETag string
}

// Runner converts one source file into a rendition file. Implementations must
// write the complete result to outputPath and honour ctx cancellation.
type Runner interface {
	Transcode(ctx context.Context, kind Kind, inputPath, outputPath string) error
}

// ext returns the rendition file extension for the kind.
func (k Kind) ext() string {
	if k == KindAudio {
		return ".m4a"
	}
	return ".mp4"
}

// contentType returns the HTTP Content-Type for the kind's rendition.
func (k Kind) contentType() string {
	if k == KindAudio {
		return "audio/mp4"
	}
	return "video/mp4"
}
