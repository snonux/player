package thumb

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DirName is the on-disk directory name used to store generated thumbnails.
// It is a hidden subdirectory placed alongside the source media files.
const DirName = ".thumbnails"

// thumbExt is the canonical extension used for all generated thumbnails.
// Thumbnails are always JPEGs regardless of the source media type.
const thumbExt = ".jpg"

// maxNameLen is the longest file name (in bytes) common filesystems accept
// (NAME_MAX on ext4, XFS, ZFS, NFS). A source name may itself be that long,
// so appending ".jpg" must not push the thumbnail name over the limit.
const maxNameLen = 255

// nameHashLen is the number of hex digits of the source name's SHA-256 kept
// in a shortened thumbnail name.
const nameHashLen = 16

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
// each other's thumbnail. Dotfiles need no special case (".bashrc" ->
// ".bashrc.jpg"); the previous stem-based scheme collapsed every dotfile to
// a bare ".jpg".
//
// A source name too long to take the extra ".jpg" (252 bytes and up) is cut
// at a rune boundary and suffixed with a hash of the full name, so it stays
// within maxNameLen and still differs from its equally long neighbours.
func ThumbnailNameFor(srcPath string) string {
	base := filepath.Base(srcPath)
	if len(base)+len(thumbExt) <= maxNameLen {
		return base + thumbExt
	}
	sum := sha256.Sum256([]byte(base))
	suffix := "~" + hex.EncodeToString(sum[:])[:nameHashLen] + thumbExt
	cut := maxNameLen - len(suffix)
	for cut > 0 && !utf8.RuneStart(base[cut]) {
		cut--
	}
	return base[:cut] + suffix
}

// ThumbnailPathFor returns the full path to the generated thumbnail of
// srcPath: ThumbnailNameFor(srcPath) inside the .thumbnails directory next
// to the source. Scanner, upload/import and thumbnail regeneration all use
// this one layout, and because the thumbnail sits in the source's own
// directory, two media files can only share a thumbnail path if they share
// a directory and a basename, i.e. if they are the same file.
func ThumbnailPathFor(srcPath string) string {
	return filepath.Join(ThumbnailDir(filepath.Dir(srcPath)), ThumbnailNameFor(srcPath))
}

// IsGenerated reports whether path lies directly inside a .thumbnails
// directory, i.e. is a thumbnail this package generated. It is false for
// thumbnails that are ordinary files: an audio cover image, or an image
// serving as its own thumbnail.
func IsGenerated(path string) bool {
	return filepath.Base(filepath.Dir(path)) == DirName
}

// LegacyPathsFor returns the thumbnail paths releases up to v0.2.2 may have
// written for srcPath, a media file inside the set directory setPath. Those
// releases named a thumbnail after the source's stem only ("holiday.jpg").
// Uploads put it in .thumbnails next to the source, the scanner in the
// set's own .thumbnails whatever the source's subfolder, so several sources
// could map to one file. The paths are distinct; for a source directly in
// setPath both layouts coincide and one path is returned.
func LegacyPathsFor(srcPath, setPath string) []string {
	base := filepath.Base(srcPath)
	name := strings.TrimSuffix(base, filepath.Ext(base)) + thumbExt
	beside := filepath.Join(ThumbnailDir(filepath.Dir(srcPath)), name)
	inSet := filepath.Join(ThumbnailDir(setPath), name)
	if beside == inSet {
		return []string{beside}
	}
	return []string{beside, inSet}
}
