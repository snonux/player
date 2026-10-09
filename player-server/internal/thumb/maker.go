package thumb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Maker creates thumbnail files for media items. The scanner uses it to
// off-load thumbnail creation policy (canonical thumbnail path derivation,
// creating the .thumbnails directory, generator invocation, verifying the
// result, and the failure-tolerant warn-and-continue behaviour) so the
// scanner only orchestrates the scan and does not own thumbnail policy.
//
// MakeVideo / MakeImage return the path of the finished thumbnail, or an
// empty string when none could be made. A missing thumbnail is never an
// error for the caller: the reason is logged and the media item is simply
// handled without one, so a single bad file or an unwritable folder cannot
// fail a scan.
type Maker interface {
	// MakeVideo produces a thumbnail for a video file. duration is the
	// video duration in seconds and is forwarded to the underlying
	// Generator so it can pick a sensible frame.
	MakeVideo(ctx context.Context, srcPath string, duration float64) string
	// MakeImage produces a thumbnail for an image file. No duration is
	// applicable so 0 is passed to the underlying Generator.
	MakeImage(ctx context.Context, srcPath string) string
}

// MakerFS is the small filesystem surface FSMaker needs, so tests can
// inject failures without depending on the scanner package.
type MakerFS interface {
	MkdirAll(path string, perm os.FileMode) error
	// CreateTemp creates an empty, uniquely named file in dir whose name
	// ends in the thumbnail extension, and returns its path.
	CreateTemp(dir string) (string, error)
	Stat(name string) (os.FileInfo, error)
	// Lstat is Stat that does not follow a symbolic link.
	Lstat(name string) (os.FileInfo, error)
	Rename(oldPath, newPath string) error
	Remove(name string) error
}

// tempPrefix starts the name of every temporary file the maker generates
// into; see IsTemporary.
const tempPrefix = ".tmp-"

// IsTemporary reports whether name (a file name, not a path) is that of a
// temporary file an FSMaker generates into before renaming it into place.
// A maker removes its temporary file itself; one can only be left behind
// when the process is killed mid-generation, which is why the scanner
// sweeps old ones (see scanner.sweepStaleTemporaries).
func IsTemporary(name string) bool {
	return strings.HasPrefix(name, tempPrefix) && strings.HasSuffix(name, thumbExt)
}

// osMakerFS delegates to the standard library; it is used as the default
// when NewFSMaker is called without an explicit MakerFS.
type osMakerFS struct{}

func (osMakerFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osMakerFS) Stat(name string) (os.FileInfo, error)        { return os.Stat(name) }
func (osMakerFS) Lstat(name string) (os.FileInfo, error)       { return os.Lstat(name) }
func (osMakerFS) Rename(oldPath, newPath string) error         { return os.Rename(oldPath, newPath) }
func (osMakerFS) Remove(name string) error                     { return os.Remove(name) }

// CreateTemp reserves a hidden temporary file. os.CreateTemp makes it
// private (0600); it is opened up to the usual file mode because the
// generator writes the thumbnail into this very file, which is then renamed
// into place and must stay readable like any other thumbnail.
func (osMakerFS) CreateTemp(dir string) (string, error) {
	f, err := os.CreateTemp(dir, tempPrefix+"*"+thumbExt)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := errors.Join(f.Chmod(0o644), f.Close()); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// FSMaker is the production Maker implementation. It wraps a Generator
// (which performs the actual frame extraction) together with a small
// filesystem dependency, and a logger for the "skipped thumbnail" warning.
type FSMaker struct {
	gen    Generator
	fs     MakerFS
	logger *slog.Logger
}

var _ Maker = (*FSMaker)(nil)

// NewFSMaker constructs an FSMaker around gen. A nil fs defaults to the
// real OS filesystem; a nil logger defaults to slog.Default(). gen may be
// nil only for a maker that is never asked to make anything and is used
// solely to Remove thumbnails (the garbage collector has no generator).
func NewFSMaker(gen Generator, fs MakerFS, logger *slog.Logger) *FSMaker {
	if fs == nil {
		fs = osMakerFS{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FSMaker{gen: gen, fs: fs, logger: logger}
}

// MakeVideo creates a thumbnail for a video file in the .thumbnails
// directory next to it. The path comes from ThumbnailPathFor so importers,
// the scanner, and the resolver all agree on where thumbnails live.
func (m *FSMaker) MakeVideo(ctx context.Context, srcPath string, duration float64) string {
	return m.makeOrSkip(ctx, srcPath, duration)
}

// MakeImage creates a thumbnail for an image file. Duration is irrelevant
// for static images so 0 is forwarded to the Generator.
func (m *FSMaker) MakeImage(ctx context.Context, srcPath string) string {
	return m.makeOrSkip(ctx, srcPath, 0)
}

// makeOrSkip is Make for the scanner: a failure is logged and reported as
// "" (see Maker).
func (m *FSMaker) makeOrSkip(ctx context.Context, srcPath string, duration float64) string {
	dst, err := m.Make(ctx, srcPath, duration)
	if err != nil {
		m.logger.Warn("thumb maker skipping thumbnail", "path", srcPath, "err", err)
		return ""
	}
	return dst
}

// Make creates the thumbnail of srcPath (duration 0 for an image) and
// returns its path, or the reason none could be made. It is what callers
// use that must tell their user about a failure: upload, podcast download
// and "regenerate thumbnail". A returned path always names a complete,
// non-empty file.
//
// The thumbnail is generated into a temporary file in the destination
// directory and renamed into place only once it is complete. A thumbnail
// already at the destination is therefore either left untouched (on any
// failure, including a generator killed halfway) or replaced atomically,
// never truncated or half-written. Reserving the temporary file first also
// makes an unwritable folder fail before the generator is started, so such
// a folder costs no ffmpeg run however often it is rescanned. The
// temporary file's name is chosen here, not derived from the media name.
func (m *FSMaker) Make(ctx context.Context, srcPath string, duration float64) (string, error) {
	dst := ThumbnailPathFor(srcPath)
	tmp, err := m.reserve(filepath.Dir(dst))
	if err != nil {
		return "", err
	}
	if err = m.render(ctx, srcPath, tmp, duration); err == nil {
		err = m.fs.Rename(tmp, dst)
	}
	if err != nil {
		if rmErr := m.fs.Remove(tmp); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			m.logger.Warn("thumb maker left a temporary file behind", "path", tmp, "err", rmErr)
		}
		return "", err
	}
	return dst, nil
}

// reserve creates the thumbnail directory if needed and an empty temporary
// file in it for the generator to write to.
//
// Remove deletes a .thumbnails directory once it is empty, possibly right
// between the two steps here when another item of the same folder is being
// purged. The directory is then simply created again; once the temporary
// file exists the directory is no longer empty and cannot be removed.
func (m *FSMaker) reserve(dir string) (string, error) {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if err = m.fs.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir thumbnails %q: %w", dir, err)
		}
		var tmp string
		if tmp, err = m.fs.CreateTemp(dir); err == nil {
			return tmp, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			break
		}
	}
	return "", fmt.Errorf("reserve thumbnail file in %q: %w", dir, err)
}

// Remove deletes the generated thumbnail at thumbPath, and its .thumbnails
// directory once that leaves it empty, so purged media leaves nothing
// behind that would keep an otherwise empty folder in place. A thumbnail
// that is already gone is fine; any other failure is logged, since the
// callers are cleaning up and have nothing better to do about it.
//
// Deciding that the thumbnail is no longer needed is the caller's job. As a
// last line of defence Remove refuses any path that is not directly inside
// a .thumbnails directory, so it can never delete a media file elsewhere.
func (m *FSMaker) Remove(thumbPath string) {
	if !IsGenerated(thumbPath) {
		return
	}
	if err := m.fs.Remove(thumbPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.logger.Warn("thumbnail removal failed", "path", thumbPath, "err", err)
		return
	}
	// Only a real directory is removed: a .thumbnails that is a symbolic
	// link (thumbnails kept on another volume) would be unlinked by the
	// same call, emptied target or not. The removal is non-recursive and
	// fails, harmlessly, while other thumbnails remain.
	dir := filepath.Dir(thumbPath)
	if info, err := m.fs.Lstat(dir); err == nil && info.IsDir() {
		_ = m.fs.Remove(dir)
	}
}

// render runs the generator and checks that it really produced something.
// The generator's word is not taken for it: ffmpeg can exit successfully
// without writing a frame, which would otherwise leave an empty thumbnail.
func (m *FSMaker) render(ctx context.Context, srcPath, out string, duration float64) error {
	if err := m.gen.Generate(ctx, srcPath, out, duration); err != nil {
		return err
	}
	info, err := m.fs.Stat(out)
	if err != nil {
		return fmt.Errorf("generator wrote no thumbnail: %w", err)
	}
	if info.Size() == 0 {
		return errors.New("generator wrote an empty thumbnail")
	}
	return nil
}
