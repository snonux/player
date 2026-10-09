package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/thumb"
)

// migrationFS is the filesystem surface the thumbnail migration needs. It is
// separate from FS so the discoverer and the probe workers, which only read,
// are not handed the ability to rename and delete files.
type migrationFS interface {
	Stat(name string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	Rename(oldPath, newPath string) error
	Remove(name string) error
}

// thumbnailUpdater is the one store operation the migration performs.
type thumbnailUpdater interface {
	UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error
}

// thumbMove describes one media row whose generated thumbnail is stored at a
// path the current naming scheme (thumb.ThumbnailPathFor) no longer produces.
type thumbMove struct {
	media model.Media
	src   string // absolute path of the source media file
	from  string // thumbnail path currently stored in the database
	to    string // thumbnail path under the current naming scheme
	// exclusive is true when no other row of the set stores from or could
	// have written it under the old naming, so the file is known to show
	// this row's media and can simply be renamed.
	exclusive bool
}

// thumbMigrator moves the thumbnails of one set's already indexed media to
// the current naming scheme and updates media.thumbnail_path accordingly.
//
// Releases up to v0.2.2 named a thumbnail after the source's stem, and the
// scanner kept all thumbnails of a set flat in one directory. "holiday.mp4"
// and "holiday.png", or "a/clip.mp4" and "b/clip.mp4", therefore shared one
// JPEG and every card showed whichever was written last. Stored paths keep
// resolving after an upgrade, so nothing breaks before the first rescan;
// the migration then gives every row its own thumbnail:
//
//   - a thumbnail only one row can have written is renamed, which is cheap
//     and keeps the frame a user picked via "regenerate thumbnail";
//   - a thumbnail several rows could have written cannot be attributed to
//     one of them, so each of those rows gets a fresh one from its source;
//   - an old file is deleted as soon as no row points at it any more.
//
// Every step is safe to interrupt and to repeat: a row is only switched to
// a thumbnail that exists, a row that cannot be migrated keeps its old,
// still working path and is retried on the next rescan, and only files
// inside the set directory are ever touched.
type thumbMigrator struct {
	store  thumbnailUpdater
	maker  thumb.Maker
	fs     migrationFS
	logger *slog.Logger
	// stored counts the rows pointing at each thumbnail path. It is kept
	// current while rows move, to know when an old file became unused and
	// to never write over a file another row still shows.
	stored map[string]int
}

// newThumbMigrator creates a migrator for a single run over one set.
func newThumbMigrator(store thumbnailUpdater, maker thumb.Maker, fsys migrationFS, logger *slog.Logger) *thumbMigrator {
	return &thumbMigrator{store: store, maker: maker, fs: fsys, logger: logger}
}

// run migrates the set at setPath. existing holds all of the set's rows
// keyed by relPath (soft-deleted ones included), seenRel the relPaths found
// on disk. Each planned row counts as one file in progress so the admin UI
// shows the migration moving instead of a scan stuck at zero files.
func (tm *thumbMigrator) run(ctx context.Context, existing map[string]model.Media, seenRel map[string]struct{}, setPath, setName string, progress *model.ScanProgress) {
	moves := tm.plan(existing, seenRel, setPath)
	if len(moves) == 0 {
		return
	}
	if progress != nil {
		progress.AddFilesTotal(len(moves))
	}
	var migrated int
	for _, mv := range moves {
		if ctx.Err() != nil {
			break
		}
		if tm.migrate(ctx, mv) {
			migrated++
		}
		if progress != nil {
			progress.IncrementFile()
		}
	}
	tm.logger.Info("scanner migrated thumbnails", "set", setName, "migrated", migrated, "pending", len(moves)-migrated)
}

// plan lists the rows whose generated thumbnail is not at its current path.
// Only video and image rows qualify, with a source file on disk (seenRel)
// and a stored thumbnail that is a generated one inside setPath: audio
// covers and images serving as their own thumbnail are ordinary files, and
// a stale path pointing outside the set (media root moved, database copied)
// must never make the scanner rename or delete files elsewhere.
//
// Rows with longer file names come first. A row's new path can be the old
// path of a longer-named row ("holiday.mp4" -> "holiday.mp4.jpg", which is
// the old thumbnail of "holiday.mp4.png"), never the reverse, so that order
// frees every destination before it is needed.
func (tm *thumbMigrator) plan(existing map[string]model.Media, seenRel map[string]struct{}, setPath string) []thumbMove {
	tm.stored = make(map[string]int, len(existing))
	for _, m := range existing {
		if m.ThumbnailPath != "" {
			tm.stored[m.ThumbnailPath]++
		}
	}
	claims := thumbnailClaims(existing, setPath)
	var moves []thumbMove
	for relPath, m := range existing {
		if _, onDisk := seenRel[relPath]; !onDisk || !hasGeneratedThumbnail(m) {
			continue
		}
		from := m.ThumbnailPath
		if !thumb.IsGenerated(from) || !isWithin(setPath, from) {
			continue
		}
		// Derive the source from relPath: AbsPath may be stale on old rows.
		src := filepath.Join(setPath, filepath.FromSlash(relPath))
		to := thumb.ThumbnailPathFor(src)
		if to == from {
			continue
		}
		moves = append(moves, thumbMove{media: m, src: src, from: from, to: to, exclusive: claims[from] == 1})
	}
	sort.Slice(moves, func(i, j int) bool {
		li, lj := len(filepath.Base(moves[i].src)), len(filepath.Base(moves[j].src))
		if li != lj {
			return li > lj
		}
		return moves[i].media.RelPath < moves[j].media.RelPath
	})
	return moves
}

// thumbnailClaims counts, per thumbnail path, the rows that store it or
// that the old naming would have made write it. A count of one means the
// file can only show that one row's media.
//
// The old names are included, not just what the database currently stores,
// because rows leave a shared file one by one: after "regenerate thumbnail"
// on holiday.mp4, or after an interrupted migration, holiday.png is the
// only row still storing holiday.jpg, yet the file may show the video.
// Rows whose source is gone or that are soft-deleted count as well.
func thumbnailClaims(existing map[string]model.Media, setPath string) map[string]int {
	claims := make(map[string]int, len(existing))
	for relPath, m := range existing {
		var paths []string
		if hasGeneratedThumbnail(m) {
			paths = thumb.LegacyPathsFor(filepath.Join(setPath, filepath.FromSlash(relPath)), setPath)
		}
		if m.ThumbnailPath != "" && !slices.Contains(paths, m.ThumbnailPath) {
			paths = append(paths, m.ThumbnailPath)
		}
		for _, p := range paths {
			claims[p]++
		}
	}
	return claims
}

// hasGeneratedThumbnail reports whether m is of a type thumbnails are
// generated for.
func hasGeneratedThumbnail(m model.Media) bool {
	return m.Type == model.MediaTypeVideo || m.Type == model.MediaTypeImage
}

// isWithin reports whether path lies below dir.
func isWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// migrate moves one row to its new thumbnail path and reports whether it
// did. The file is put in place first and the row is switched afterwards,
// so the database never points at a thumbnail that does not exist; if the
// switch fails, a rename is undone so the row's old path keeps working.
func (tm *thumbMigrator) migrate(ctx context.Context, mv thumbMove) bool {
	if tm.stored[mv.to] > 0 {
		// Another row still shows the file at the destination; writing
		// there would change that row's thumbnail. It normally moved away
		// earlier in this run. If it could not, retry on the next rescan.
		tm.logger.Warn("scanner thumbnail migration deferred, destination in use", "path", mv.src, "destination", mv.to)
		return false
	}
	renamed, ok := tm.place(ctx, mv)
	if !ok {
		return false
	}
	// The file is in place: finish the switch even if the scan was
	// cancelled meanwhile (TriggerRescan cancels the running scan).
	if err := tm.store.UpdateMediaThumbnail(context.WithoutCancel(ctx), mv.media.ID, mv.to); err != nil {
		tm.logger.Warn("scanner thumbnail migration update failed", "path", mv.src, "err", err)
		if renamed {
			tm.undoRename(mv)
		}
		return false
	}
	tm.stored[mv.from]--
	tm.stored[mv.to]++
	if tm.stored[mv.from] == 0 {
		// No row is left on the old file. A renamed file is already gone,
		// which is not an error.
		if err := tm.fs.Remove(mv.from); err != nil && !errors.Is(err, fs.ErrNotExist) {
			tm.logger.Warn("scanner stale thumbnail removal failed", "path", mv.from, "err", err)
		}
	}
	return true
}

// place makes sure mv's thumbnail exists at its new path. renamed reports
// that the old file was moved there (and must be moved back if the row
// cannot be switched); ok is false when the row has to keep its old path.
//
// An exclusive thumbnail is renamed. If the destination already exists it
// is this row's own thumbnail, left by a run that was interrupted before
// the row was switched, and is adopted as is. If the old file has vanished
// a new one is generated. Any other rename failure (read-only or foreign
// owned directory) is logged and left for a later rescan: generating would
// hit the same error at the cost of an ffmpeg run per row. A thumbnail
// that is not exclusive is always generated from the row's own source.
func (tm *thumbMigrator) place(ctx context.Context, mv thumbMove) (renamed, ok bool) {
	if !mv.exclusive {
		return false, tm.generate(ctx, mv)
	}
	if _, err := tm.fs.Stat(mv.to); err == nil {
		return false, true
	}
	err := tm.rename(mv.from, mv.to)
	switch {
	case err == nil:
		return true, true
	case errors.Is(err, fs.ErrNotExist):
		return false, tm.generate(ctx, mv)
	default:
		tm.logger.Warn("scanner thumbnail migration rename failed", "path", mv.src, "err", err)
		return false, false
	}
}

// rename moves a thumbnail file, creating the destination directory first.
func (tm *thumbMigrator) rename(from, to string) error {
	if err := tm.fs.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(to), err)
	}
	return tm.fs.Rename(from, to)
}

// undoRename moves a renamed thumbnail back after its row could not be
// switched. Should that fail too, the row points at a missing file until
// the next rescan, which finds the thumbnail at its new path and adopts it.
func (tm *thumbMigrator) undoRename(mv thumbMove) {
	if err := tm.fs.Rename(mv.to, mv.from); err != nil {
		tm.logger.Error("scanner thumbnail migration rollback failed", "path", mv.src, "thumbnail", mv.to, "err", err)
	}
}

// generate creates mv's thumbnail at its new path from the source file and
// reports whether a usable file is there afterwards. The generator's word
// is not taken for it: ffmpeg can exit successfully without writing a
// frame. A missing or empty result counts as a failure, and whatever a
// failed or killed run left behind is removed so a later rescan does not
// mistake it for a finished thumbnail.
func (tm *thumbMigrator) generate(ctx context.Context, mv thumbMove) bool {
	var (
		path string
		err  error
	)
	if mv.media.Type == model.MediaTypeVideo {
		path, err = tm.maker.MakeVideo(ctx, mv.src, mv.media.Duration)
	} else {
		path, err = tm.maker.MakeImage(ctx, mv.src)
	}
	if err == nil && path == mv.to {
		if info, statErr := tm.fs.Stat(mv.to); statErr == nil && info.Size() > 0 {
			return true
		}
	}
	// The maker already logged a generator failure; err covers the rest.
	tm.logger.Warn("scanner thumbnail migration could not generate thumbnail", "path", mv.src, "err", err)
	if rmErr := tm.fs.Remove(mv.to); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		tm.logger.Warn("scanner partial thumbnail removal failed", "path", mv.to, "err", rmErr)
	}
	return false
}
