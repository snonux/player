package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/snonux/player/internal/fileversion"
	"github.com/snonux/player/internal/mediatype"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/thumb"
)

// Compile-time check that *mediaStreamer satisfies MediaStreamer.
var _ MediaStreamer = (*mediaStreamer)(nil)

type mediaStreamer struct {
	remuxer   probe.Remuxer
	mediaRoot string // root directory that all streamed paths must stay under
	roots     *PathRoots
	// thumbCache is the one directory outside mediaRoot files are served
	// from: the thumbnail cache (see WithThumbnailCache). Empty: none.
	thumbCache string
}

// NewMediaStreamer creates the default service for preparing media files for
// HTTP streaming. mediaRoot, when non-empty, constrains every Open call to
// files underneath that directory; any path that resolves outside it is
// rejected to prevent filepath-traversal via a compromised AbsPath in the DB.
func NewMediaStreamer(remuxer probe.Remuxer, mediaRoot string) *mediaStreamer {
	return &mediaStreamer{remuxer: remuxer, mediaRoot: mediaRoot}
}

// WithPathRoots allows streaming from any registered music library root.
func (s *mediaStreamer) WithPathRoots(roots *PathRoots) *mediaStreamer {
	s.roots = roots
	return s
}

// WithThumbnailCache also allows generated thumbnails below dir, the
// configured thumbnail cache (THUMBNAIL_CACHE_DIR), to be opened. Without
// it they would be refused for lying outside the media root. Only files
// directly inside a .thumbnails directory qualify, which is all the cache
// ever holds. An empty dir changes nothing.
func (s *mediaStreamer) WithThumbnailCache(dir string) *mediaStreamer {
	if dir == "" {
		return s
	}
	// Stored cache paths are absolute (thumb.NewLayout), so compare against
	// the absolute directory even when the setting is relative.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	s.thumbCache = dir
	return s
}

// servable reports whether path may be opened: it lies below a media
// library root, or it is a generated thumbnail in the thumbnail cache.
func (s *mediaStreamer) servable(path string) bool {
	if s.roots != nil {
		if s.roots.ContainsLexical(path) {
			return true
		}
	} else if pathWithinRoot(s.mediaRoot, path) {
		return true
	}
	return s.thumbCache != "" && thumb.IsGenerated(filepath.Clean(path)) && pathWithinRoot(s.thumbCache, path)
}

func (s *mediaStreamer) Open(ctx context.Context, file *FileResult, attachment bool) (*StreamResult, error) {
	if file == nil {
		return nil, ErrNotFound
	}

	// Guard against filepath-traversal: if a media root is configured, reject
	// any path that resolves outside it. This ensures that a compromised
	// AbsPath stored in the database cannot be used to serve arbitrary files.
	// The check itself lives in pathWithinRoot so the compatibility stream
	// applies the identical rule. The thumbnail cache is the one exception
	// (see servable).
	if !s.servable(file.Path) {
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
		Revision:    fileversion.FromInfo(stat),
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
