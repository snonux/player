package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snonux/player/internal/thumb"
)

// confinedPath checks both the lexical path and existing symlink ancestors.
// Missing suffixes are allowed so callers can safely create new directories.
func confinedPath(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil || !pathWithin(root, path) {
		return false
	}
	realRoot, ok := resolvedPath(root)
	if !ok {
		return false
	}
	resolved, ok := resolvedPath(path)
	return ok && pathWithin(realRoot, resolved)
}

func resolvedPath(path string) (string, bool) {
	ancestor := path
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, true
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", false
		}
		if info, statErr := os.Lstat(ancestor); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", false
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
}

// lexicalPathWithin compares absolute forms without resolving inaccessible
// filesystem ancestors. It is used only when GC discards database state.
func lexicalPathWithin(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	return err == nil && pathWithin(root, path)
}

func coverDirectory(root, setPath, folder string) (string, error) {
	setDir := filepath.Join(root, filepath.FromSlash(setPath))
	dir := filepath.Join(setDir, filepath.FromSlash(strings.Trim(folder, "/")))
	if !confinedPath(root, setDir) || (dir != setDir && !pathWithin(setDir, dir)) || !confinedPath(root, dir) {
		return "", ErrForbidden
	}
	return dir, nil
}

func readonlyError(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS)
}

func mutationError(err error) error {
	if readonlyError(err) {
		return ErrReadOnlySet
	}
	return fmt.Errorf("write media directory: %w", err)
}

func writableMediaDir(root, dir string) error {
	if !confinedPath(root, dir) {
		return ErrForbidden
	}
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return mutationError(err)
	}
	return errors.Join(f.Close(), os.Remove(f.Name()))
}

// coverDestination keeps generated cover reads and writes on the same layout
// and refuses both lexical escapes and escaping symbolic links.
func coverDestination(root string, layout thumb.Layout, setPath, folder string) (string, string, error) {
	dir, err := coverDirectory(root, setPath, folder)
	if err != nil {
		return "", "", err
	}
	path, ok := layout.CoverPathFor(dir)
	if !ok {
		return "", "", ErrForbidden
	}
	if layout.CacheDir() != "" {
		root = layout.CacheDir()
	}
	if !confinedPath(root, path) {
		return "", "", ErrForbidden
	}
	return dir, path, nil
}
