package service

import (
	"os"
	"path/filepath"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/thumb"
)

// removeOwnThumbnail deletes the generated thumbnail of a media item whose
// file and row are being deleted for good (garbage collection, podcast
// unsubscribe, a failed upload or download), and the .thumbnails directory
// once that leaves it empty. Without it every purged item would leave an
// orphaned JPEG behind, and an otherwise empty folder could not be removed.
//
// Only a thumbnail at the item's own current path (thumb.ThumbnailPathFor)
// is deleted, because only that path is known to belong to this item alone.
// Anything else is left in place: a cover image or an image serving as its
// own thumbnail is an ordinary file, and a thumbnail still stored under the
// stem-based naming of releases up to v0.2.2 may be shared with another
// item. A rescan migrates those, see docs/admin.md.
func removeOwnThumbnail(media *model.Media) {
	if media.AbsPath == "" || media.ThumbnailPath != thumb.ThumbnailPathFor(media.AbsPath) {
		return
	}
	removeAndLog(media.ThumbnailPath)
	// Non-recursive: fails, harmlessly, while other thumbnails remain.
	_ = os.Remove(filepath.Dir(media.ThumbnailPath))
}
