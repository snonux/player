package service

import (
	"path/filepath"
	"sync"
)

// PathRoots is the allowlist of independent media library roots. Streaming,
// writes and GC accept paths under any registered root (plus thumbnail cache).
type PathRoots struct {
	mu    sync.RWMutex
	roots []string
}

// NewPathRoots builds an allowlist from absolute or relative root paths.
func NewPathRoots(roots ...string) *PathRoots {
	p := &PathRoots{}
	p.Set(roots...)
	return p
}

// Set replaces the allowlist. Empty entries are ignored.
func (p *PathRoots) Set(roots ...string) {
	clean := make([]string, 0, len(roots))
	seen := map[string]struct{}{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			abs = filepath.Clean(root)
		} else {
			abs = filepath.Clean(abs)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		clean = append(clean, abs)
	}
	p.mu.Lock()
	p.roots = clean
	p.mu.Unlock()
}

// All returns a copy of the configured roots.
func (p *PathRoots) All() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, len(p.roots))
	copy(out, p.roots)
	return out
}

// ContainsLexical reports whether path is under any root (streamer/compat).
func (p *PathRoots) ContainsLexical(path string) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.roots) == 0 {
		return pathWithinRoot("", path)
	}
	for _, root := range p.roots {
		if pathWithinRoot(root, path) {
			return true
		}
	}
	return false
}

// ContainsConfined reports whether path is confined under any root (writes/GC).
func (p *PathRoots) ContainsConfined(path string) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, root := range p.roots {
		if confinedPath(root, path) {
			return true
		}
	}
	return false
}

// Primary returns the first root (default MEDIA_ROOT), or empty.
func (p *PathRoots) Primary() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.roots) == 0 {
		return ""
	}
	return p.roots[0]
}
