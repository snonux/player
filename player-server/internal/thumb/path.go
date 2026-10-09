package thumb

import (
	"path/filepath"
	"strings"
)

// DirName is the on-disk directory name used to store generated thumbnails.
// It is a hidden subdirectory placed alongside the source media files.
const DirName = ".thumbnails"

// thumbExt is the canonical extension used for all generated thumbnails.
// Thumbnails are always JPEGs regardless of the source media type.
const thumbExt = ".jpg"

// ThumbnailDir returns the conventional thumbnail directory inside parent.
// The directory is a hidden ".thumbnails" subfolder sitting next to the
// source media. filepath.Join handles any trailing slashes on parent.
func ThumbnailDir(parent string) string {
	return filepath.Join(parent, DirName)
}

// ThumbnailNameFor returns the canonical thumbnail filename for the given
// source file: its full basename, original extension included, plus ".jpg"
// ("holiday.mp4" -> "holiday.mp4.jpg").
//
// Keeping the extension is what makes the name unique per source file:
// "holiday.mp4" and "holiday.png" used to share "holiday.jpg" and overwrote
// each other's thumbnail. The mapping is injective, so two different
// basenames can never yield the same name. Dotfiles need no special case
// (".bashrc" -> ".bashrc.jpg"); the previous stem-based scheme collapsed
// every dotfile to a bare ".jpg".
func ThumbnailNameFor(srcPath string) string {
	return filepath.Base(srcPath) + thumbExt
}

// ThumbnailPathFor returns the full path to the thumbnail for srcPath stored
// under parent/.thumbnails/. parent is the directory that owns the thumbnail
// folder: the source's own directory for uploads, the set directory for the
// scanner.
//
// When srcPath lies in a subdirectory of parent, that subdirectory is
// mirrored below .thumbnails ("set/a/clip.mp4" with parent "set" ->
// "set/.thumbnails/a/clip.mp4.jpg"), so same-named files in different
// folders of one set do not collide either. A srcPath outside parent falls
// back to the bare name directly inside parent/.thumbnails/.
func ThumbnailPathFor(srcPath, parent string) string {
	rel, err := filepath.Rel(parent, srcPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = filepath.Base(srcPath)
	}
	return filepath.Join(ThumbnailDir(parent), filepath.Dir(rel), ThumbnailNameFor(rel))
}

// ParentOf returns the directory owning the .thumbnails tree that thumbPath
// lives in, i.e. the parent to pass to ThumbnailPathFor to rebuild a path in
// the same tree. ok is false when thumbPath is not inside a .thumbnails
// directory, which means it is not a generated thumbnail (for example an
// audio cover image, or an image that serves as its own thumbnail).
func ParentOf(thumbPath string) (parent string, ok bool) {
	dir := filepath.Dir(thumbPath)
	for {
		if filepath.Base(dir) == DirName {
			return filepath.Dir(dir), true
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", false
		}
		dir = next
	}
}
