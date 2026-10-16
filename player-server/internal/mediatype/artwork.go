package mediatype

import (
	"path/filepath"
	"strings"
)

// IsNamedCoverImage recognizes source artwork shared by a folder's audio files.
func IsNamedCoverImage(path string) bool {
	if !IsCoverImageExt(path) {
		return false
	}
	name := strings.ToLower(filepath.Base(path))
	switch strings.TrimSuffix(name, filepath.Ext(name)) {
	case "cover", "front", "folder", "album":
		return true
	default:
		return false
	}
}

// CoverImageRank orders sibling artwork names, with Beets' cover.jpg first.
// The scanner also accepts generated covers and unnamed images as fallbacks.
// Callers comparing artwork from different books should rank each folder
// separately, so one book's front.png is as eligible as another's cover.jpg.
func CoverImageRank(path string) int {
	name := strings.ToLower(filepath.Base(path))
	if name == "cover.jpg" {
		return 0
	}
	switch strings.TrimSuffix(name, filepath.Ext(name)) {
	case "cover":
		return 1
	case "front":
		return 2
	case "folder":
		return 3
	case "album":
		return 4
	case ".cover":
		return 5
	default:
		return 6
	}
}
