// Package scanner implements media library scanning logic.
package scanner

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/mediatype"
)

// appleDoublePrefix is the filename prefix macOS uses for AppleDouble
// resource-fork sidecars created when writing to non-HFS volumes (e.g. SMB,
// FAT, ext4). These files mirror a real media file's name (e.g. "._foo.mp3"
// next to "foo.mp3") but contain only ~4 KB of HFS metadata, so probing or
// streaming them confuses ffprobe and ExoPlayer (UnrecognizedInputFormatException).
const appleDoublePrefix = "._"

// isAppleDoubleSidecar reports whether name is a macOS AppleDouble sidecar.
// Centralised here so every walker (media discovery, cover-image gathering,
// and future callers) skips them at the earliest possible layer instead of
// each downstream consumer having to know about the convention.
func isAppleDoubleSidecar(name string) bool {
	return strings.HasPrefix(name, appleDoublePrefix)
}

// fileDiscoverer walks a set directory and collects media file paths and
// cover image locations. It is responsible solely for filesystem traversal —
// it does not probe, store, or generate thumbnails.
type fileDiscoverer struct {
	fs FS
}

// newFileDiscoverer creates a fileDiscoverer backed by the given filesystem abstraction.
func newFileDiscoverer(fs FS) *fileDiscoverer {
	return &fileDiscoverer{fs: fs}
}

// Discover walks setPath and returns the absolute paths of all supported media
// files, skipping hidden directories, macOS AppleDouble sidecars, and the
// application-generated .cover.jpg artwork file.
func (d *fileDiscoverer) Discover(setPath string) ([]string, error) {
	var files []string
	walkErr := d.fs.WalkDir(setPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %q: %w", path, err)
		}
		// Skip hidden directories (e.g. .git, .thumbnails) to avoid
		// accidentally ingesting dot-prefixed paths.
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != setPath {
				return filepath.SkipDir
			}
			return nil
		}
		// AppleDouble sidecars share the same extension as the real file
		// they shadow, so the extension check below would otherwise accept
		// them. Filter them out before classification.
		if isAppleDoubleSidecar(entry.Name()) || entry.Name() == ".cover.jpg" {
			return nil
		}
		if !mediatype.IsSupportedExt(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

// gatherCoverImages walks the set directory and records the first cover image
// per directory. The returned map is keyed by absolute directory path; values
// are the image path relative to setPath so callers can join them as needed.
func (d *fileDiscoverer) gatherCoverImages(setPath string) map[string]string {
	coverImages := make(map[string]string)
	_ = d.fs.WalkDir(setPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !mediatype.IsCoverImageExt(path) {
			// Skip hidden subdirectories, but tolerate walk errors silently
			// since missing cover images are non-fatal.
			if entry != nil && entry.IsDir() && strings.HasPrefix(entry.Name(), ".") && path != setPath {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip AppleDouble sidecars: "._cover.jpg" is not a real image and
		// would otherwise be picked as a directory's cover art.
		if isAppleDoubleSidecar(entry.Name()) {
			return nil
		}
		relPath, _ := filepath.Rel(setPath, path)
		dir := filepath.Dir(path)
		if _, ok := coverImages[dir]; !ok {
			coverImages[dir] = relPath
		}
		return nil
	})
	return coverImages
}
