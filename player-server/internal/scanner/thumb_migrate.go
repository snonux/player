package scanner

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

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
//     the path the current scheme (thumb.Layout.PathFor) gives it. It
//     also picks rows whose thumbnail is missing or was never made, which
//     the same steps repair;
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
// path and is retried on the next rescan, and the only files ever deleted
// are generated thumbnails inside the set directory and the maker's stale
// temporary files (see sweepStaleTemporaries).
//
// Switching the thumbnail cache on (THUMBNAIL_CACHE_DIR) uses the very same
// steps: a thumbnail beside its source is then "not at its current path",
// so the first rescan generates every thumbnail anew in the cache and
// deletes the one in the set. A cache directory that was abandoned (the
// setting changed or removed) is unknown to the new Layout and its files
// are not served any more; the next rescan gives those rows a fresh
// thumbnail at the current path and leaves the abandoned directory alone
// (see lostElsewhere).

// thumbnailUpdater is the one store operation the thumbnail migration needs.
type thumbnailUpdater interface {
	UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error
}

// fileRemover deletes a file. It is kept apart from FS so that only the
// thumbSwitcher and sweepStaleTemporaries, not the discoverer or the probe
// workers, can delete.
type fileRemover interface {
	Remove(name string) error
}

// staleKind says why an indexed row gets a new thumbnail.
type staleKind int

const (
	// staleOldName: the generated thumbnail is stored under an older
	// naming scheme, or beside the source although a thumbnail cache is
	// now configured. This is the migration.
	staleOldName staleKind = iota + 1
	// staleMissing: the generated thumbnail is at its current path but the
	// file is gone.
	staleMissing
	// staleNone: there is no generated thumbnail: a video without one, or
	// an image standing in as its own. An earlier attempt failed.
	staleNone
	// staleLost: the stored generated thumbnail is somewhere the server
	// cannot show it from (see lostElsewhere), typically a thumbnail cache
	// directory that is no longer configured. Like staleNone the row gets
	// a new thumbnail and nothing is deleted.
	staleLost
)

// keepsOldFile reports whether a row of this kind has no old generated
// thumbnail that may be deleted once the row points at its new one.
func (k staleKind) keepsOldFile() bool {
	return k == staleNone || k == staleLost
}

// staleThumbnails returns, by relPath, the rows that get a thumbnail
// generated in this scan, and why. existing holds all rows of the set at
// setPath keyed by relPath (soft-deleted ones included, since trashed media
// can be restored), seenRel the relPaths of the media files found on disk,
// exists reports whether a file is there, and layout says where thumbnails
// are created now.
//
// Videos, audio and images whose source file is on disk qualify, SVG images
// excepted (an SVG always is its own thumbnail).
func staleThumbnails(existing map[string]model.Media, seenRel map[string]struct{}, setPath string, exists func(path string) bool, layout thumb.Layout) map[string]staleKind {
	stale := make(map[string]staleKind)
	for relPath, m := range existing {
		if m.Type != model.MediaTypeVideo && m.Type != model.MediaTypeImage && m.Type != model.MediaTypeAudio {
			continue
		}
		if _, onDisk := seenRel[relPath]; !onDisk || strings.EqualFold(filepath.Ext(relPath), ".svg") {
			continue
		}
		// Derive the source from relPath: AbsPath may be stale on old rows.
		src := filepath.Join(setPath, filepath.FromSlash(relPath))
		if m.Type == model.MediaTypeAudio && (m.ThumbnailPath == "" || !exists(m.ThumbnailPath)) {
			stale[relPath] = staleNone
			continue
		}
		if kind := staleKindOf(m.ThumbnailPath, src, setPath, existing, exists, layout); kind != 0 {
			stale[relPath] = kind
		}
	}
	return stale
}

// staleKindOf classifies the thumbnail path stored for the media file src,
// returning 0 when it is fine as it is.
//
// A stored path counts as a generated thumbnail when it is directly inside
// a .thumbnails directory of the set and is not itself the source file of
// an indexed media item (see isGeneratedIn). Such a thumbnail is stale when
// it is not at its current path, or is there but missing on disk. The
// latter repairs rows no other step would: a thumbnail deleted by hand, or
// lost to the one race the migration leaves open (see
// thumbSwitcher.reserved). It costs one stat per indexed video and image.
//
// No thumbnail at all, or the image itself, means generation failed when
// the file was indexed (unwritable folder, a frame that could not be
// extracted). It is tried again once per rescan. For a file ffmpeg cannot
// read at all that attempt is wasted on every rescan: one ffmpeg run for
// an image or a video of up to a second, two for a longer video (the
// seeked run, then the first-frame fallback in thumb.FFmpegGenerator).
//
// The row's own path in the thumbnail cache is a generated thumbnail too,
// although it is outside the set. It is checked first and can only be
// missing: a Layout with a cache creates thumbnails nowhere else.
//
// A generated thumbnail outside both (a .thumbnails directory elsewhere:
// an abandoned or renamed cache directory, a media root that moved) is
// replaced when the server cannot show it, see lostElsewhere.
//
// Anything else is an ordinary file that was chosen as the thumbnail, or a
// path in another set that still exists, and is left alone: a stale
// absolute path (media root moved, database copied from another host) must
// never lead to files elsewhere being replaced or deleted.
func staleKindOf(stored, src, setPath string, existing map[string]model.Media, exists func(path string) bool, layout thumb.Layout) staleKind {
	switch {
	case stored == "" || stored == src:
		return staleNone
	case layout.IsCachedFor(stored, src):
		if !exists(stored) {
			return staleMissing
		}
		return 0
	case !isGeneratedIn(setPath, stored, existing):
		if lostElsewhere(stored, setPath, exists, layout) {
			return staleLost
		}
		return 0
	}
	current, ok := layout.PathFor(src)
	switch {
	case !ok:
		return 0 // the layout has no place for src; nothing to move to
	case stored != current:
		return staleOldName
	case !exists(stored):
		return staleMissing
	}
	return 0
}

// lostElsewhere reports whether stored is a generated thumbnail outside the
// set that the server cannot show: its file is gone, or it lies outside
// both the media root and the current thumbnail cache, from where nothing
// is ever served. That is what the thumbnails of a cache directory look
// like after THUMBNAIL_CACHE_DIR was changed or removed.
//
// Such a row (staleLost) is treated like one whose thumbnail was never
// made: a new thumbnail is generated at the current path and the row
// pointed at it, while the file at the stored path, if any, is neither
// replaced nor deleted.
func lostElsewhere(stored, setPath string, exists func(path string) bool, layout thumb.Layout) bool {
	if !thumb.IsGenerated(stored) || within(setPath, stored) {
		return false
	}
	return !exists(stored) || !layout.Serves(stored)
}

// isGeneratedIn reports whether path is a generated thumbnail inside the set
// at setPath and not the source file of one of the set's indexed media. The
// scanner never indexes anything below a hidden directory, but rows of
// files in a directory called .thumbnails can exist from uploads and older
// releases, and such a file may be stored as a thumbnail.
func isGeneratedIn(setPath, path string, existing map[string]model.Media) bool {
	if !thumb.IsGenerated(path) || !within(setPath, path) {
		return false
	}
	rel, err := filepath.Rel(setPath, path)
	if err != nil {
		return false
	}
	_, isMedia := existing[filepath.ToSlash(rel)]
	return !isMedia
}

// within reports whether path lies inside dir, lexically.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// staleTemporaryAge is how old a maker's temporary file must be before a
// scan deletes it. A generation takes seconds; an hour leaves a wide margin
// for one that is still running (uploads and "regenerate thumbnail" are not
// serialised with scans).
const staleTemporaryAge = time.Hour

// sweepStaleTemporaries deletes temporary files a thumb.FSMaker left in the
// .thumbnails directories it generates into. The maker removes its
// temporary file itself, so one only stays behind when the server is killed
// in the middle of a generation; nothing else would ever remove it.
//
// Without a thumbnail cache those are the directories next to the media
// files. With one, it is the part of the cache mirroring the set at
// setPath, walked as a whole: a folder whose media is gone can still hold
// a temporary, which would keep the folder from ever being removed. The
// directories next to the media are not looked at then, let alone cleaned:
// nothing in the media tree is the server's to delete with a cache.
func (s *FSScanner) sweepStaleTemporaries(setPath string, files []string) {
	cutoff := s.clock.Now().Add(-staleTemporaryAge)
	if cacheDir, ok := s.layout.CachedDir(setPath); ok {
		s.sweepCacheTree(cacheDir, cutoff)
		return
	}
	swept := make(map[string]struct{})
	for _, f := range files {
		current, ok := s.layout.PathFor(f)
		if !ok {
			continue
		}
		dir := filepath.Dir(current)
		if _, done := swept[dir]; done {
			continue
		}
		swept[dir] = struct{}{}
		s.sweepTemporariesIn(dir, cutoff)
	}
}

// sweepCacheTree sweeps the .thumbnails directories of the thumbnail cache
// folder dir and of every folder below it. Symbolic links below dir are not
// descended into (a directory entry that is a link is not a directory).
// Only the set being scanned is covered: what is cached for a set whose
// directory is gone from the media root stays until it is removed by hand.
func (s *FSScanner) sweepCacheTree(dir string, cutoff time.Time) {
	entries, err := s.fs.ReadDir(dir)
	if err != nil {
		return // usually: nothing cached for this set yet
	}
	for _, entry := range entries {
		switch {
		case !entry.IsDir():
		case entry.Name() == thumb.DirName:
			s.sweepTemporariesIn(filepath.Join(dir, entry.Name()), cutoff)
		default:
			s.sweepCacheTree(filepath.Join(dir, entry.Name()), cutoff)
		}
	}
}

// sweepTemporariesIn deletes the maker's temporary files in dir that were
// last modified before cutoff.
func (s *FSScanner) sweepTemporariesIn(dir string, cutoff time.Time) {
	entries, err := s.fs.ReadDir(dir)
	if err != nil {
		return // usually: no .thumbnails directory here
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || entry.IsDir() || !thumb.IsTemporary(entry.Name()) || !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := s.remover.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.log().Warn("scanner stale temporary thumbnail removal failed", "path", path, "err", err)
		}
	}
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
	// reserved holds the current thumbnail path of every media file found
	// on disk by this scan. Such a path is never deleted even when no row
	// stores it yet: it can be the old thumbnail of one row and the new
	// one of another ("holiday.mp4.jpg" for holiday.mp4.png and
	// holiday.mp4), and that other row, or a new file being probed right
	// now, is about to use it.
	//
	// stored and reserved are snapshots from the start of the set's scan.
	// A file uploaded during the scan is in neither; if its thumbnail path
	// happens to be the old thumbnail of a row migrated in this very scan,
	// that thumbnail is deleted here. The next rescan notices the missing
	// file and regenerates it (see staleThumbnails). Asking the database
	// before every deletion instead would cost a query over the whole set
	// per migrated row.
	reserved map[string]struct{}
}

// newThumbSwitcher creates the switcher for one set scan. existing are the
// set's rows at the start of the scan, files the media files found on disk,
// and layout says where their thumbnails are created.
func newThumbSwitcher(store thumbnailUpdater, remover fileRemover, logger *slog.Logger, existing map[string]model.Media, files []string, layout thumb.Layout) *thumbSwitcher {
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
		if current, ok := layout.PathFor(f); ok {
			ts.reserved[current] = struct{}{}
		}
	}
	return ts
}

// apply switches the row in result to its new thumbnail. If the update
// fails the row keeps its old path; the new file stays where it is and is
// simply overwritten when the next rescan retries the row.
//
// A row whose thumbnail was regenerated at the path it already stored (the
// file was missing) needs neither an update nor a cleanup. result.replaced
// is empty when the row had no generated thumbnail before; then there is
// no old file to delete.
func (ts *thumbSwitcher) apply(ctx context.Context, result fileResult) {
	from, to := result.replaced, result.media.ThumbnailPath
	if from == to {
		return
	}
	if err := ts.store.UpdateMediaThumbnail(ctx, result.media.ID, to); err != nil {
		ts.logger.Warn("scanner thumbnail update failed", "path", result.path, "err", err)
		return
	}
	ts.stored[to]++
	if from == "" {
		return
	}
	ts.stored[from]--
	if _, isReserved := ts.reserved[from]; isReserved || ts.stored[from] > 0 {
		return
	}
	if err := ts.remover.Remove(from); err != nil && !errors.Is(err, fs.ErrNotExist) {
		ts.logger.Warn("scanner stale thumbnail removal failed", "path", from, "err", err)
	}
}
