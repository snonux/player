package thumb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Maker creates thumbnail files for media items. The scanner uses it to
// off-load thumbnail creation policy (mkdir of the .thumbnails directory,
// canonical thumbnail path derivation, generator invocation, and the
// failure-tolerant warn-and-continue behaviour) so the scanner only
// orchestrates the higher-level scan and does not own thumbnail policy.
//
// MakeVideo / MakeImage return the resolved thumbnail path on success, or
// an empty string (and nil error) when the underlying generator failed —
// matching the scanner's pre-extraction "skip-and-continue" semantics.
// A non-nil error is reserved for genuine filesystem problems such as
// failing to create the .thumbnails directory.
type Maker interface {
	// MakeVideo produces a thumbnail for a video file. duration is the
	// video duration in seconds and is forwarded to the underlying
	// Generator so it can pick a sensible frame.
	MakeVideo(ctx context.Context, srcPath, parent string, duration float64) (string, error)
	// MakeImage produces a thumbnail for an image file. No duration is
	// applicable so 0 is passed to the underlying Generator.
	MakeImage(ctx context.Context, srcPath, parent string) (string, error)
}

// MakerFS is the small filesystem surface FSMaker needs. It mirrors the
// scanner's FS interface for the single operation FSMaker performs
// (creating the .thumbnails directory) so tests can inject an in-memory
// fake without depending on the scanner package.
type MakerFS interface {
	MkdirAll(path string, perm os.FileMode) error
}

// osMakerFS delegates MkdirAll to the standard library; it is used as
// the default when NewFSMaker is called without an explicit MakerFS.
type osMakerFS struct{}

func (osMakerFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// FSMaker is the production Maker implementation. It wraps a Generator
// (which performs the actual frame extraction) together with a small
// filesystem dependency for directory creation, and a logger so the
// "skipped thumbnail" warning is consistent with the scanner's previous
// behaviour.
type FSMaker struct {
	gen    Generator
	fs     MakerFS
	logger *slog.Logger
}

var _ Maker = (*FSMaker)(nil)

// NewFSMaker constructs an FSMaker around gen. A nil fs defaults to the
// real OS filesystem; a nil logger defaults to slog.Default(). gen must
// not be nil — callers always have a Generator available in production
// and tests can pass MockGenerator.
func NewFSMaker(gen Generator, fs MakerFS, logger *slog.Logger) *FSMaker {
	if fs == nil {
		fs = osMakerFS{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FSMaker{gen: gen, fs: fs, logger: logger}
}

// MakeVideo creates a thumbnail for a video file inside parent/.thumbnails/.
// The path layout mirrors ThumbnailPathFor so importers, the scanner, and
// the resolver all agree on where thumbnails live. Generator failures are
// logged and swallowed so a single bad file does not abort the scan.
func (m *FSMaker) MakeVideo(ctx context.Context, srcPath, parent string, duration float64) (string, error) {
	return m.make(ctx, srcPath, parent, duration)
}

// MakeImage creates a thumbnail for an image file. Duration is irrelevant
// for static images so 0 is forwarded to the Generator.
func (m *FSMaker) MakeImage(ctx context.Context, srcPath, parent string) (string, error) {
	return m.make(ctx, srcPath, parent, 0)
}

// make is the shared implementation behind MakeVideo / MakeImage. It
// ensures the .thumbnails directory exists, derives the canonical
// thumbnail path via ThumbnailPathFor, and delegates the actual frame
// extraction to the Generator. Generator errors are logged and reported
// as ("", nil) so the scanner can continue with the next file.
func (m *FSMaker) make(ctx context.Context, srcPath, parent string, duration float64) (string, error) {
	dir := ThumbnailDir(parent)
	if err := m.fs.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir thumbnails %q: %w", dir, err)
	}
	thumbnailPath := ThumbnailPathFor(srcPath, parent)
	if err := m.gen.Generate(ctx, srcPath, thumbnailPath, duration); err != nil {
		m.logger.Warn("thumb maker skipping thumbnail", "path", srcPath, "err", err)
		return "", nil
	}
	return thumbnailPath, nil
}
