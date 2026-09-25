package thumb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"codeberg.org/snonux/player/internal/model"
)

// ErrNotFound is returned by a Resolver when a media item has no thumbnail
// (and no acceptable fallback) available. Callers translate this sentinel
// to their domain-specific not-found error (e.g. service.ErrNotFound).
var ErrNotFound = errors.New("thumbnail not found")

// ResolvedFile describes a thumbnail file that has been located on (or
// abstractly resolved from) some backing store. It contains everything a
// caller needs to construct a HTTP file response without itself touching
// the filesystem.
type ResolvedFile struct {
	Path     string
	FileName string
	FileSize int64
}

// Resolver abstracts the "given a media item, return a thumbnail file" step.
// Pulling this out of browseService lets the service layer avoid direct
// os.Stat calls and lets tests inject a fake resolver instead of writing
// temporary files.
type Resolver interface {
	// Resolve returns a ResolvedFile for the media's thumbnail. If the
	// media has no thumbnail and no fallback is available, it returns
	// ErrNotFound. Any other error indicates a real I/O problem.
	Resolve(media *model.Media) (*ResolvedFile, error)
}

// FSResolver is the production Resolver that stats files on the local
// filesystem. It encapsulates the original logic that lived in
// browseService.GetThumbnail: prefer the generated thumbnail, fall back to
// the original file for images so that cover.jpg-style assets still render.
type FSResolver struct{}

// NewFSResolver returns a Resolver backed by os.Stat against real paths.
func NewFSResolver() *FSResolver {
	return &FSResolver{}
}

// Resolve looks up the thumbnail file on disk for media, falling back to
// the original AbsPath for image media when the generated thumbnail is
// missing. A missing or empty thumbnail path with no fallback yields
// ErrNotFound so callers can map it to a 404 cleanly. This includes a
// thumbnail path that is still recorded in the database but whose file was
// deleted from disk. Other stat failures (for example permission denied) are
// real I/O problems and are returned wrapped, not as ErrNotFound.
func (FSResolver) Resolve(media *model.Media) (*ResolvedFile, error) {
	if media == nil || media.ThumbnailPath == "" {
		return nil, ErrNotFound
	}
	file, err := statFile(media.ThumbnailPath)
	if err == nil {
		return file, nil
	}
	what := "thumbnail"
	if media.Type == model.MediaTypeImage && errors.Is(err, os.ErrNotExist) {
		// Generated thumbnail missing: for images, fall back to the
		// original file so cover.jpg / folder.jpg etc. still render.
		what = "original image"
		file, err = statFile(media.AbsPath)
		if err == nil {
			return file, nil
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotFound, what, err)
	}
	return nil, fmt.Errorf("stat %s: %w", what, err)
}

// statFile describes path as a ResolvedFile, or returns the os.Stat error.
func statFile(path string) (*ResolvedFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &ResolvedFile{Path: path, FileName: filepath.Base(path), FileSize: info.Size()}, nil
}
