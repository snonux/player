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
// source file (basename without extension + ".jpg"). For dotfiles (e.g.
// ".bashrc") filepath.Ext returns the leading-dot name itself; TrimSuffix
// then yields an empty base, producing just ".jpg" — matching the historical
// behaviour of the inlined helpers being replaced.
func ThumbnailNameFor(srcPath string) string {
	base := filepath.Base(srcPath)
	return strings.TrimSuffix(base, filepath.Ext(base)) + thumbExt
}

// ThumbnailPathFor returns the full path to the thumbnail for srcPath stored
// under parent/.thumbnails/. parent is the directory that should contain the
// thumbnail folder (typically the set directory or the source's parent dir).
func ThumbnailPathFor(srcPath, parent string) string {
	return filepath.Join(ThumbnailDir(parent), ThumbnailNameFor(srcPath))
}
