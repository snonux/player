// Package scanner implements media library scanning logic.
package scanner

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FS abstracts filesystem operations for testability.
type FS interface {
	ReadDir(name string) ([]os.DirEntry, error)
	Stat(name string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	WalkDir(root string, walkFn fs.WalkDirFunc) error
	// Rename and Remove exist only to move thumbnails written under an
	// older naming scheme to their current path (see thumb_migrate.go).
	Rename(oldPath, newPath string) error
	Remove(name string) error
}

// osFS delegates to the standard library.
type osFS struct{}

func (osFS) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }
func (osFS) Stat(name string) (os.FileInfo, error)      { return os.Stat(name) }
func (osFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (osFS) WalkDir(root string, walkFn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, walkFn)
}
func (osFS) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (osFS) Remove(name string) error             { return os.Remove(name) }
