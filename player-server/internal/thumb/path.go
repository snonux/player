package thumb

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
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
// this one layout. The thumbnail sits in the source's own directory and
// carries the source's full name, so two different media files get two
// different thumbnail paths. The one exception is theoretical: names
// shortened by ThumbnailNameFor are told apart by a hash only.
func ThumbnailPathFor(srcPath string) string {
	return filepath.Join(ThumbnailDir(filepath.Dir(srcPath)), ThumbnailNameFor(srcPath))
}

// IsGenerated reports whether path lies directly inside a .thumbnails
// directory, which is where this package puts generated thumbnails. It is
// false for thumbnails that are ordinary files elsewhere: an audio cover
// image, or an image serving as its own thumbnail.
//
// The directory name alone does not prove a file is disposable: a media
// file can itself be stored in a directory called .thumbnails. Callers
// that delete or replace files must also rule out that path is the source
// file of a media item.
func IsGenerated(path string) bool {
	return filepath.Base(filepath.Dir(path)) == DirName
}
