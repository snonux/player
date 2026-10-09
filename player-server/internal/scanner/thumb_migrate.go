package scanner

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/thumb"
)

// This file holds the migration of thumbnails written under an older naming
// scheme. Releases up to v0.2.2 named a thumbnail after the source's stem,
// and the scanner kept all thumbnails of a set flat in one directory, so
// "holiday.mp4" and "holiday.png", or "a/clip.mp4" and "b/clip.mp4", shared
// one JPEG and every card showed whichever was written last.
//
// Stored paths keep resolving after an upgrade, so nothing breaks before
// the first rescan. A rescan then treats every such row the same way:
//
//  1. staleThumbnails picks the rows whose generated thumbnail is not at
//     the path the current scheme (thumb.ThumbnailPathFor) gives it;
//  2. a probeWorker generates a fresh thumbnail from the row's own source
//     at that path (probeWorker.refreshThumbnail), in the same worker pool
//     that probes new files;
//  3. the scanWriter hands the result to thumbSwitcher, which points the
//     row at the new file and deletes the old one once no row uses it.
//
// Old thumbnails are never renamed or reused. Which media an old file shows
// cannot be known (that is the bug), and a file found at the new path may be
// a leftover of anything, so the only thumbnail trusted is one generated
// from the source in this run. The price is one generator run per migrated
// row, once, and that a frame picked earlier with "regenerate thumbnail" is
// replaced by a new random one.
//
// The steps are safe to interrupt and to repeat. A row is switched only
// after its new thumbnail is complete (thumb.Maker replaces the destination
// atomically), a row that cannot be migrated keeps its old, still working
// path and is retried on the next rescan, and only files inside the set
// directory are ever deleted.

// thumbnailUpdater is the one store operation the thumbnail migration needs.
type thumbnailUpdater interface {
	UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error
}

// fileRemover deletes a file. It is kept apart from FS so that only the
// thumbSwitcher, not the discoverer or the probe workers, can delete.
type fileRemover interface {
	Remove(name string) error
}

// staleThumbnails returns the relPaths of the rows whose generated thumbnail
// has to be migrated. existing holds all rows of the set at setPath keyed by
// relPath (soft-deleted ones included, since trashed media can be restored),
// seenRel the relPaths of the media files found on disk.
//
// A row qualifies when it is a video or image whose source file is on disk
// and whose stored thumbnail
//   - is a generated one: directly inside a .thumbnails directory, and not
//     itself the source file of a media item (a media file may live in a
//     directory of that name and serve as its own thumbnail);
//   - lies inside the set, so a stale absolute path (media root moved,
//     database copied from another host) never leads to files elsewhere
//     being deleted;
//   - is not already at its current path.
//
// Audio covers and images serving as their own thumbnail are ordinary
// files, not generated thumbnails, and are left alone.
func staleThumbnails(existing map[string]model.Media, seenRel map[string]struct{}, setPath string) map[string]struct{} {
	stale := make(map[string]struct{})
	for relPath, m := range existing {
		if m.Type != model.MediaTypeVideo && m.Type != model.MediaTypeImage {
			continue
		}
		if _, onDisk := seenRel[relPath]; !onDisk {
			continue
		}
		if !isGeneratedIn(setPath, m.ThumbnailPath, existing, seenRel) {
			continue
		}
		// Derive the source from relPath: AbsPath may be stale on old rows.
		src := filepath.Join(setPath, filepath.FromSlash(relPath))
		if thumb.ThumbnailPathFor(src) != m.ThumbnailPath {
			stale[relPath] = struct{}{}
		}
	}
	return stale
}

// isGeneratedIn reports whether path is a generated thumbnail inside the set
// at setPath and not the source file of one of the set's media.
func isGeneratedIn(setPath, path string, existing map[string]model.Media, seenRel map[string]struct{}) bool {
	if !thumb.IsGenerated(path) {
		return false
	}
	rel, err := filepath.Rel(setPath, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	rel = filepath.ToSlash(rel)
	_, isRow := existing[rel]
	_, isFile := seenRel[rel]
	return !isRow && !isFile
}

// thumbSwitcher finishes the migration of one row: it points the row at its
// freshly generated thumbnail and deletes the old file once it is unused.
// It is driven by the scanWriter goroutine only, so it needs no locking and
// its database writes are serialised with all other writes of the scan.
type thumbSwitcher struct {
	store   thumbnailUpdater
	remover fileRemover
	logger  *slog.Logger
	// stored counts the rows (soft-deleted included) pointing at each
	// thumbnail path; kept current as rows are switched.
	stored map[string]int
	// reserved holds the current thumbnail path of every media file on
	// disk. Such a path is never deleted even when no row stores it yet:
	// it can be the old thumbnail of one row and the new one of another
	// ("holiday.mp4.jpg" for holiday.mp4.png and holiday.mp4), and that
	// other row, or a new file being probed right now, is about to use it.
	reserved map[string]struct{}
}

// newThumbSwitcher creates the switcher for one set scan. existing are the
// set's rows at the start of the scan, files the media files found on disk.
func newThumbSwitcher(store thumbnailUpdater, remover fileRemover, logger *slog.Logger, existing map[string]model.Media, files []string) *thumbSwitcher {
	ts := &thumbSwitcher{
		store:    store,
		remover:  remover,
		logger:   logger,
		stored:   make(map[string]int, len(existing)),
		reserved: make(map[string]struct{}, len(files)),
	}
	for _, m := range existing {
		if m.ThumbnailPath != "" {
			ts.stored[m.ThumbnailPath]++
		}
	}
	for _, f := range files {
		ts.reserved[thumb.ThumbnailPathFor(f)] = struct{}{}
	}
	return ts
}

// apply switches the row in result to its new thumbnail. If the update
// fails the row keeps its old path; the new file stays where it is and is
// simply overwritten when the next rescan retries the row.
func (ts *thumbSwitcher) apply(ctx context.Context, result fileResult) {
	from, to := result.replaced, result.media.ThumbnailPath
	if err := ts.store.UpdateMediaThumbnail(ctx, result.media.ID, to); err != nil {
		ts.logger.Warn("scanner thumbnail migration update failed", "path", result.path, "err", err)
		return
	}
	ts.stored[from]--
	ts.stored[to]++
	if _, isReserved := ts.reserved[from]; isReserved || ts.stored[from] > 0 {
		return
	}
	if err := ts.remover.Remove(from); err != nil && !errors.Is(err, fs.ErrNotExist) {
		ts.logger.Warn("scanner stale thumbnail removal failed", "path", from, "err", err)
	}
}
