package service

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/thumb"
)

// ThumbnailMaker is what the services need to create and delete generated
// thumbnails. thumb.FSMaker implements it: the same implementation the
// scanner uses, so every writer of thumbnails generates into a temporary
// file, verifies the result and renames it into place.
type ThumbnailMaker interface {
	// Make creates the thumbnail of srcPath (duration 0 for an image) and
	// returns its path. A returned path names a complete, non-empty file.
	Make(ctx context.Context, srcPath string, duration float64) (string, error)
	thumbnailRemover
}

// thumbnailRemover deletes a generated thumbnail and its directory once
// empty. Failures are logged by the implementation.
type thumbnailRemover interface {
	Remove(thumbPath string)
}

// newThumbnailMaker wraps gen in the shared thumb.FSMaker. It returns nil
// for a nil generator, which services built without one (tests, mostly)
// treat as "thumbnails cannot be generated".
func newThumbnailMaker(gen thumb.Generator, logger *slog.Logger) ThumbnailMaker {
	if gen == nil {
		return nil
	}
	return thumb.NewFSMaker(gen, nil, logger)
}

// newThumbnailRemover returns a remover for services that delete thumbnails
// but may have no generator, such as the garbage collector.
func newThumbnailRemover(logger *slog.Logger) thumbnailRemover {
	return thumb.NewFSMaker(nil, nil, logger)
}

// removeOwnThumbnail deletes the generated thumbnail of a media item whose
// file and row are being deleted for good (garbage collection, podcast
// unsubscribe, an undone download). Without it every purged item would
// leave an orphaned JPEG behind, and an otherwise empty folder could not be
// removed.
//
// Only a thumbnail at the item's own current path (thumb.ThumbnailPathFor)
// is deleted, because only that path is known to belong to this item alone.
// Anything else is left in place: a cover image or an image serving as its
// own thumbnail is an ordinary file, and a thumbnail still stored under the
// stem-based naming of releases up to v0.2.2 may be shared with another
// item, which cannot be checked from here. Such a thumbnail stays behind
// as an orphan when its item is purged before a rescan has migrated it;
// see docs/admin.md.
func removeOwnThumbnail(rm thumbnailRemover, media *model.Media) {
	if media.AbsPath == "" || media.ThumbnailPath != thumb.ThumbnailPathFor(media.AbsPath) {
		return
	}
	rm.Remove(media.ThumbnailPath)
}

// setDirOf returns the directory of the set media belongs to, derived from
// the row itself: its absolute path minus its set-relative path. ok is false
// when the two do not fit together or the result is not below mediaRoot,
// i.e. when the row's paths are stale and say nothing reliable about where
// its set lives.
func setDirOf(mediaRoot string, media *model.Media) (dir string, ok bool) {
	rel := filepath.FromSlash(media.RelPath)
	dir, ok = strings.CutSuffix(media.AbsPath, string(filepath.Separator)+rel)
	if !ok || rel == "" || !pathWithin(mediaRoot, dir) {
		return "", false
	}
	return dir, true
}

// pathWithin reports whether path lies below dir.
func pathWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
