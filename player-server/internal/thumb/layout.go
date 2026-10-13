package thumb

import (
	"path/filepath"
	"strings"
)

// Layout decides where the generated thumbnail of a media file lives.
// There are two places, and one Layout uses exactly one of them for the
// thumbnails it creates:
//
//   - beside the source, in its .thumbnails directory (ThumbnailPathFor).
//     This is the default and what the zero Layout does;
//   - in the thumbnail cache, when one is configured (THUMBNAIL_CACHE_DIR):
//     a directory outside the media root that mirrors the media tree
//     ("<cache>/<set>/<folder>/.thumbnails/<name>"). Nothing is then written
//     into the media tree for thumbnails, which is what allows a set to be
//     a read-only mount of another application's library, and lets the
//     thumbnails live on a volume of their own.
//
// A cached thumbnail keeps the .thumbnails parent directory, so IsGenerated
// holds for both places, and it keeps the media-relative directory, so two
// files of the same name in different folders never share a thumbnail.
//
// Both places stay known whichever is in use, so a thumbnail written under
// the other setting is still recognised as belonging to its media file
// (Holds) and is moved over by the next rescan.
type Layout struct {
	mediaRoot string
	cacheDir  string
}

// NewLayout returns the Layout of the media tree at mediaRoot with the
// thumbnail cache at cacheDir. An empty cacheDir means no cache: thumbnails
// go beside their source. Both are made absolute, so the paths a Layout
// hands out compare equal to the ones stored for media scanned below the
// same media root.
func NewLayout(mediaRoot, cacheDir string) Layout {
	if cacheDir == "" {
		return Layout{}
	}
	return Layout{mediaRoot: absOrClean(mediaRoot), cacheDir: absOrClean(cacheDir)}
}

// absOrClean makes path absolute. filepath.Abs only fails when the working
// directory cannot be determined; the cleaned path is the best left then.
func absOrClean(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// PathFor returns the path at which the thumbnail of srcPath is created:
// in the cache when there is one, beside the source otherwise. A source
// outside the media root has no place in the cache and keeps its thumbnail
// beside it; the server itself only ever indexes files below the root.
func (l Layout) PathFor(srcPath string) string {
	if cached, ok := l.Cached(srcPath); ok {
		return cached
	}
	return l.Beside(srcPath)
}

// Beside returns the thumbnail path of srcPath next to the source.
func (l Layout) Beside(srcPath string) string {
	return ThumbnailPathFor(srcPath)
}

// Cached returns the thumbnail path of srcPath in the thumbnail cache. ok
// is false without a cache, and for a source that is not below the media
// root: such a file has no place in a tree mirroring the media root, and
// must never be mapped to a path outside the cache.
func (l Layout) Cached(srcPath string) (path string, ok bool) {
	if l.cacheDir == "" {
		return "", false
	}
	rel, err := filepath.Rel(l.mediaRoot, filepath.Dir(absOrClean(srcPath)))
	if err != nil || escapes(rel) {
		return "", false
	}
	return filepath.Join(ThumbnailDir(filepath.Join(l.cacheDir, rel)), ThumbnailNameFor(srcPath)), true
}

// IsCachedFor reports whether thumbPath is the cache path of srcPath's
// thumbnail. Such a file belongs to that one media file: unlike a
// thumbnail beside the source it can never be a leftover of an older
// naming scheme shared with another item.
func (l Layout) IsCachedFor(thumbPath, srcPath string) bool {
	cached, ok := l.Cached(srcPath)
	return ok && thumbPath == cached
}

// Holds reports whether thumbPath is one of the two places the thumbnail of
// srcPath can live, beside the source or in the cache, whichever of them
// PathFor currently picks.
func (l Layout) Holds(thumbPath, srcPath string) bool {
	return thumbPath != "" && (thumbPath == l.Beside(srcPath) || l.IsCachedFor(thumbPath, srcPath))
}

// inCache reports whether dir lies strictly below the cache directory.
func (l Layout) inCache(dir string) bool {
	if l.cacheDir == "" {
		return false
	}
	rel, err := filepath.Rel(l.cacheDir, dir)
	return err == nil && rel != "." && !escapes(rel)
}

// escapes reports whether the relative path rel leads out of its base.
func escapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}
