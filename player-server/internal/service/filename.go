package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// uniqueFilename returns a non-conflicting full path by appending (n)
// if a file with the same name already exists in dir.
func uniqueFilename(dir, filename string) string {
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == "" {
		return ""
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}

	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
