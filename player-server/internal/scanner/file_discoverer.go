// Package scanner implements media library scanning logic.
package scanner

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/mediatype"
)

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
// files, skipping hidden directories (names starting with ".").
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
		relPath, _ := filepath.Rel(setPath, path)
		dir := filepath.Dir(path)
		if _, ok := coverImages[dir]; !ok {
			coverImages[dir] = relPath
		}
		return nil
	})
	return coverImages
}
