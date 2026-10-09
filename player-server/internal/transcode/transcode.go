// Package transcode produces and caches client-compatible renditions of media
// whose original container or codec the web or Android player cannot decode
// (AVI, WMV, FLV, WMA, ...).
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
const profileVersion = "v3"

// Errors returned by Cache.Ensure. None of them wraps a context error unless
// the caller's own context ended, so "the client went away" can never be
// confused with "the job was stopped".
var (
	// ErrPending reports that the rendition is still being produced. The
	// transcode keeps running in the background; callers should retry.
	ErrPending = errors.New("transcode in progress")
	// ErrBusy reports that no new transcode can be started right now (queue
	// full, or the requester already has its share of jobs). Retry later.
	ErrBusy = errors.New("transcoder busy")
	// ErrClosed reports that the cache is shutting down.
	ErrClosed = errors.New("transcoder shutting down")
	// ErrSourceMissing reports that the source media file cannot be read.
	ErrSourceMissing = errors.New("transcode source missing")
	// ErrSourceChanged reports that the source file is still being written
	// (it was modified moments ago, or changed while it was transcoded, as
	// during an upload or copy). Nothing was published; retry later.
	ErrSourceChanged = errors.New("transcode source is still changing")
	// ErrNoSpace reports that the cache volume is too full right now to
	// write the rendition. It heals when space is freed, so it is never
	// remembered.
	ErrNoSpace = errors.New("transcode cache volume is full")
	// ErrTooLarge reports that the rendition of this source cannot fit the
	// cache budget (Options.MaxBytes) at all. Unlike ErrNoSpace this does
	// not heal by itself, so it is remembered until the source changes.
	ErrTooLarge = errors.New("rendition exceeds the transcode cache budget")
	// ErrFailedRecently reports that this media item failed to transcode a
	// while ago and is not retried yet (negative cache with backoff).
	ErrFailedRecently = errors.New("transcode failed recently")
	// ErrAborted reports that the job was stopped by shutdown or because its
	// media item was removed.
	ErrAborted = errors.New("transcode aborted")
)

// Source identifies the original media file a rendition is derived from.
type Source struct {
	MediaID int64
	// Path is the source file; callers must have resolved symlinks and
	// verified it lies inside the media root.
	Path string
	Kind Kind
	// Requester identifies who asked (e.g. "user:5") for the per-requester
	// job limit. Empty disables that limit.
	Requester string
}

// Job is one transcode handed to a Runner.
type Job struct {
	Source Source
	// Output is the file to write; it has no meaningful extension.
	Output string
	// MaxBytes caps the output size. A Runner must not write (much) more;
	// the cache treats an output that reaches the cap as "no space".
	MaxBytes int64
}

// Rendition describes a finished, cached compatibility file.
type Rendition struct {
	// Path is the absolute location of the rendition inside the cache.
	Path string
	// ContentType is the HTTP Content-Type of the rendition.
	ContentType string
	// ETag identifies these exact bytes: it combines the rendition name
	// (source version + profile) with the file's creation time, so a
	// rendition that was evicted and transcoded again gets a new ETag and a
	// client can never splice two different encodes with If-Range.
	ETag string
}

// Runner converts one source file into a rendition file. Implementations must
// write the complete result to job.Output and honour ctx cancellation.
type Runner interface {
	Transcode(ctx context.Context, job Job) error
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
