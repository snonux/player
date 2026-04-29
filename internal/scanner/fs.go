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
}

// osFS delegates to the standard library.
type osFS struct{}

func (osFS) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }
func (osFS) Stat(name string) (os.FileInfo, error)       { return os.Stat(name) }
func (osFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (osFS) WalkDir(root string, walkFn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, walkFn)
}
