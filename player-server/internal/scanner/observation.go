package scanner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/snonux/player/internal/mediatype"
)

// SourceSignature observes source metadata without probing, indexing or writing.
// It retains a constant-size hash, not a map of every file. Directory mtimes are
// deliberately omitted: generating .thumbnails must not trigger a new scan.
func (s *FSScanner) SourceSignature(ctx context.Context, fallback string) (string, error) {
	roots, err := s.scanRoots(ctx, fallback)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, root := range roots {
		_, _ = fmt.Fprintf(h, "root:%d:%q\n", root.id, root.path)
		// Configured roots are trusted and accepted by scanner ReadDir even
		// when symlinks. Resolve only that root; nested symlinks stay skipped.
		walkRoot, err := filepath.EvalSymlinks(root.path)
		if err != nil {
			return "", fmt.Errorf("resolve observed library: %w", err)
		}
		walkRoot, err = filepath.Abs(walkRoot)
		if err != nil {
			return "", fmt.Errorf("absolute observed library: %w", err)
		}
		configuredRoot, err := filepath.Abs(root.path)
		if err != nil {
			return "", fmt.Errorf("absolute configured library: %w", err)
		}
		ignored := append([]string(nil), s.ignoredPaths...)
		for _, path := range s.ignoredPaths {
			if rel, err := filepath.Rel(configuredRoot, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				ignored = append(ignored, filepath.Join(walkRoot, rel))
			}
		}
		err = s.fs.WalkDir(walkRoot, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if ignoredPath(path, ignored) || (path != walkRoot && strings.HasPrefix(entry.Name(), ".")) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				_, _ = fmt.Fprintf(h, "dir:%q\n", path)
				return nil
			}
			if entry.Type()&fs.ModeSymlink != 0 || isAppleDoubleSidecar(entry.Name()) {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !mediatype.IsSupportedExt(path) && ext != ".lrc" && ext != ".txt" && ext != ".m3u" && ext != ".m3u8" {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				_, _ = fmt.Fprintf(h, "file:%q:%d:%d\n", path, info.Size(), info.ModTime().UnixNano())
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("observe library: %w", err)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func ignoredPath(path string, ignored []string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range ignored {
		rel, err := filepath.Rel(root, abs)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
