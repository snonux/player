package thumb

import (
	"errors"
	"fmt"
	"os"
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
// A Layout with a cache still knows the place beside the source, so a
// thumbnail written there before the cache was configured is recognised as
// belonging to its media file (Holds) and moved over by the next rescan.
// The reverse does not hold: without a cache, or with another cache
// directory, a path in a former cache is just a path outside the media
// root.
type Layout struct {
	// mediaRoot is absolute, or empty when unknown (the zero Layout).
	mediaRoot string
	// cacheDir is absolute, or empty without a cache.
	cacheDir string
}

// NewLayout returns the Layout of the media tree at mediaRoot with the
// thumbnail cache at cacheDir. An empty cacheDir means no cache: thumbnails
// go beside their source. Both are made absolute, so the paths a Layout
// hands out compare equal to the ones stored for media scanned below the
// same media root.
//
// The cache must be disjoint from the media root. Configuration loading
// rejects anything else; should such a pair get here all the same, the
// cache is ignored: a cache inside the root would be scanned as media, and
// a cache above it would make every media folder look like a cache folder
// to the code that removes emptied ones.
//
// An empty mediaRoot is not the working directory but "unknown": the result
// is the zero Layout, whatever cacheDir says.
func NewLayout(mediaRoot, cacheDir string) Layout {
	if mediaRoot == "" {
		return Layout{}
	}
	l := Layout{mediaRoot: absOrClean(mediaRoot)}
	if cacheDir == "" {
		return l
	}
	cache := absOrClean(cacheDir)
	if cache == l.mediaRoot || below(l.mediaRoot, cache) || below(cache, l.mediaRoot) {
		return l
	}
	l.cacheDir = cache
	return l
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
// in the cache when there is one, beside the source otherwise.
//
// ok is false when there is a cache but srcPath is not below the media
// root. Such a file has no place in the cache, and its thumbnail is not
// put beside it either: with a cache configured nothing is written next to
// media, least of all next to a file a stale database row points at
// somewhere else on the host. The server itself only indexes files below
// the root.
func (l Layout) PathFor(srcPath string) (path string, ok bool) {
	if l.cacheDir == "" {
		return l.Beside(srcPath), true
	}
	return l.Cached(srcPath)
}

// InMediaRoot reports whether path lies below the media root. A Layout
// that does not know its media root (the zero Layout) says yes.
func (l Layout) InMediaRoot(path string) bool {
	return l.mediaRoot == "" || below(l.mediaRoot, absOrClean(path))
}

// CacheDir returns the absolute thumbnail cache directory in use, or "" when
// thumbnails live beside their source. It is what everything else that
// needs the directory (the file streamer, the startup check) must go by,
// not the raw setting, which the Layout may have had to ignore.
func (l Layout) CacheDir() string {
	return l.cacheDir
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

// Holds reports whether thumbPath is one of the places this Layout knows
// for the thumbnail of srcPath: beside the source, or in its cache.
func (l Layout) Holds(thumbPath, srcPath string) bool {
	return thumbPath != "" && (thumbPath == l.Beside(srcPath) || l.IsCachedFor(thumbPath, srcPath))
}

// inCache reports whether dir lies strictly below the cache directory.
func (l Layout) inCache(dir string) bool {
	return l.cacheDir != "" && below(l.cacheDir, dir)
}

// below reports whether path lies strictly below base; both are absolute.
func below(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != "." && !escapes(rel)
}

// escapes reports whether the relative path rel leads out of its base.
func escapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

// CheckCacheDir reports whether dir can be used as the thumbnail cache: it
// is created if missing, and a file is created in it and removed again. It
// is meant for a startup check; the maker itself creates what it needs.
func CheckCacheDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create thumbnail cache directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".preflight-*")
	if err != nil {
		return fmt.Errorf("thumbnail cache directory not writable: %w", err)
	}
	return errors.Join(f.Close(), os.Remove(f.Name()))
}
