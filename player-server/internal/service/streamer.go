package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/probe"
)

// Compile-time check that *mediaStreamer satisfies MediaStreamer.
var _ MediaStreamer = (*mediaStreamer)(nil)

type mediaStreamer struct {
	remuxer   probe.Remuxer
	mediaRoot string // root directory that all streamed paths must stay under
}

// NewMediaStreamer creates the default service for preparing media files for
// HTTP streaming. mediaRoot, when non-empty, constrains every Open call to
// files underneath that directory; any path that resolves outside it is
// rejected to prevent filepath-traversal via a compromised AbsPath in the DB.
func NewMediaStreamer(remuxer probe.Remuxer, mediaRoot string) *mediaStreamer {
	return &mediaStreamer{remuxer: remuxer, mediaRoot: mediaRoot}
}

func (s *mediaStreamer) Open(ctx context.Context, file *FileResult, attachment bool) (*StreamResult, error) {
	if file == nil {
		return nil, ErrNotFound
	}

	// Guard against filepath-traversal: if a media root is configured, reject
	// any path that resolves outside it. This ensures that a compromised
	// AbsPath stored in the database cannot be used to serve arbitrary files.
	// The check itself lives in pathWithinRoot so the compatibility stream
	// applies the identical rule.
	if !pathWithinRoot(s.mediaRoot, file.Path) {
		return nil, fmt.Errorf("%w: path escapes media root", ErrForbidden)
	}

	f, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: open stream file: %w", ErrNotFound, err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: stat stream file: %w", ErrNotFound, err)
	}

	remuxed := !attachment && s.remuxer != nil && probe.LooksLikeMPEGTS(file.Path)
	contentType := mediatype.MIMETypeForExt(file.FileName)
	if remuxed {
		contentType = "video/mp4"
	}

	return &StreamResult{
		File:        f,
		Path:        file.Path,
		FileName:    file.FileName,
		Size:        stat.Size(),
		ModTime:     stat.ModTime(),
		ContentType: contentType,
		Attachment:  attachment,
		Remuxed:     remuxed,
		Duration:    file.Duration,
	}, nil
}

func (s *mediaStreamer) Remux(ctx context.Context, stream *StreamResult, w io.Writer) error {
	if stream == nil {
		return ErrNotFound
	}
	if s.remuxer == nil {
		return errors.New("remuxer not configured")
	}
	return s.remuxer.Remux(ctx, stream.Path, w)
}
