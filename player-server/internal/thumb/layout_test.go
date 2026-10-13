package thumb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLayout_PathFor(t *testing.T) {
	cache := NewLayout("/media", "/thumbs")
	tests := []struct {
		name   string
		layout Layout
		src    string
		want   string
	}{
		{"no cache: beside the source", Layout{}, "/media/set/a/clip.mp4", "/media/set/a/.thumbnails/clip.mp4.jpg"},
		{"empty cache dir means no cache", NewLayout("/media", ""), "/media/set/clip.mp4", "/media/set/.thumbnails/clip.mp4.jpg"},
		{"cache mirrors the media tree", cache, "/media/set/a/clip.mp4", "/thumbs/set/a/.thumbnails/clip.mp4.jpg"},
		{"file directly in a set", cache, "/media/set/clip.mp4", "/thumbs/set/.thumbnails/clip.mp4.jpg"},
		{"set name with a space", cache, "/media/Music Videos/x.mkv", "/thumbs/Music Videos/.thumbnails/x.mkv.jpg"},
		{"unclean source path", cache, "/media/set/../set/./clip.mp4", "/thumbs/set/.thumbnails/clip.mp4.jpg"},
		{"unclean roots", NewLayout("/media/", "/thumbs/x/.."), "/media/set/clip.mp4", "/thumbs/set/.thumbnails/clip.mp4.jpg"},
		// Never mapped into (or out of) the cache: these stay beside the source.
		{"source outside the media root", cache, "/elsewhere/clip.mp4", "/elsewhere/.thumbnails/clip.mp4.jpg"},
		{"source above the media root", cache, "/clip.mp4", "/.thumbnails/clip.mp4.jpg"},
		{"sibling sharing the root's prefix", cache, "/media-old/set/clip.mp4", "/media-old/set/.thumbnails/clip.mp4.jpg"},
		{"dot-dot out of the media root", cache, "/media/../etc/clip.mp4", "/etc/.thumbnails/clip.mp4.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.layout.PathFor(tt.src)
			if got != tt.want {
				t.Errorf("PathFor(%q) = %q, want %q", tt.src, got, tt.want)
			}
			if !IsGenerated(got) {
				t.Errorf("%q is not recognised as a generated thumbnail", got)
			}
			if !tt.layout.Holds(got, tt.src) {
				t.Errorf("layout does not hold its own path %q", got)
			}
		})
	}
}

func TestLayout_RelativeRootsMatchRelativeSources(t *testing.T) {
	l := NewLayout("media", "thumbs")
	got, ok := l.Cached(filepath.Join("media", "set", "clip.mp4"))
	want, _ := filepath.Abs(filepath.Join("thumbs", "set", DirName, "clip.mp4.jpg"))
	if !ok || got != want {
		t.Errorf("Cached = %q, %v; want %q", got, ok, want)
	}
}

func TestLayout_Holds(t *testing.T) {
	l := NewLayout("/media", "/thumbs")
	const src = "/media/set/a/clip.mp4"
	tests := []struct {
		name  string
		l     Layout
		thumb string
		want  bool
	}{
		{"beside the source", l, "/media/set/a/.thumbnails/clip.mp4.jpg", true},
		{"in the cache", l, "/thumbs/set/a/.thumbnails/clip.mp4.jpg", true},
		{"empty", l, "", false},
		{"the source itself", l, src, false},
		{"another file's thumbnail in the cache", l, "/thumbs/set/a/.thumbnails/other.mp4.jpg", false},
		{"same name in another folder of the cache", l, "/thumbs/set/b/.thumbnails/clip.mp4.jpg", false},
		{"old stem name beside the source", l, "/media/set/a/.thumbnails/clip.jpg", false},
		{"cache path without a cache", Layout{}, "/thumbs/set/a/.thumbnails/clip.mp4.jpg", false},
		{"another cache directory", l, "/old/set/a/.thumbnails/clip.mp4.jpg", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.Holds(tt.thumb, src); got != tt.want {
				t.Errorf("Holds(%q) = %v, want %v", tt.thumb, got, tt.want)
			}
		})
	}
	if l.IsCachedFor("/media/set/a/.thumbnails/clip.mp4.jpg", src) {
		t.Error("a thumbnail beside the source counts as cached")
	}
}

// textGen writes a small file, standing in for ffmpeg.
func textGen() *MockGenerator {
	return &MockGenerator{GenerateFunc: func(_ context.Context, in, out string, _ float64) error {
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}}
}

// TestFSMaker_Cache_ReadOnlySource: with a cache the maker writes nothing
// next to the source, so the source's folder may be read-only.
func TestFSMaker_Cache_ReadOnlySource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root, cache := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "set", "album")
	src := filepath.Join(dir, "clip.mp4")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	m := NewFSMaker(textGen(), nil, nil).WithLayout(NewLayout(root, cache))
	got, err := m.Make(context.Background(), src, 1)
	want := filepath.Join(cache, "set", "album", DirName, "clip.mp4.jpg")
	if err != nil || got != want {
		t.Fatalf("Make = %q, %v; want %q", got, err, want)
	}
	if content, err := os.ReadFile(got); err != nil || string(content) != "thumb of "+src {
		t.Errorf("cached thumbnail holds %q (err %v)", content, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("source folder was written to: %v", entries)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(got), ".tmp-*")); len(left) != 0 {
		t.Errorf("temporary files left in the cache: %v", left)
	}
}

// TestFSMaker_Cache_Unwritable: a cache that cannot be written is reported;
// the maker does not quietly write next to the source instead.
func TestFSMaker_Cache_Unwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root, cache := t.TempDir(), t.TempDir()
	src := filepath.Join(root, "set", "clip.mp4")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cache, 0o755) })
	runs := 0
	gen := &MockGenerator{GenerateFunc: func(context.Context, string, string, float64) error {
		runs++
		return nil
	}}

	m := NewFSMaker(gen, nil, nil).WithLayout(NewLayout(root, cache))
	if got, err := m.Make(context.Background(), src, 1); err == nil {
		t.Fatalf("Make = %q, want an error", got)
	}
	if got := m.MakeVideo(context.Background(), src, 1); got != "" {
		t.Errorf("MakeVideo = %q, want no thumbnail", got)
	}
	if runs != 0 {
		t.Errorf("generator ran %d times for an unwritable cache", runs)
	}
	if _, err := os.Stat(ThumbnailDir(filepath.Dir(src))); err == nil {
		t.Error("the maker fell back to a .thumbnails directory beside the source")
	}
}

// TestFSMaker_Remove_PrunesCacheFolders: removing the last thumbnail of a
// folder removes the emptied folders in the cache, up to but not including
// the cache directory, and nothing that still holds a thumbnail.
func TestFSMaker_Remove_PrunesCacheFolders(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	layout := NewLayout(root, cache)
	m := NewFSMaker(nil, nil, nil).WithLayout(layout)
	write := func(src string) string {
		t.Helper()
		path, ok := layout.Cached(filepath.Join(root, filepath.FromSlash(src)))
		if !ok {
			t.Fatalf("no cache path for %q", src)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("jpg"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	deep := write("set/a/b/clip.mp4")
	sibling := write("set/a/other.mp4")
	alone := write("gone/x/clip.mp4")

	m.Remove(deep)
	if _, err := os.Stat(filepath.Join(cache, "set", "a", "b")); err == nil {
		t.Error("emptied folder set/a/b was left in the cache")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Errorf("a remaining thumbnail was removed: %v", err)
	}

	m.Remove(alone)
	if _, err := os.Stat(filepath.Join(cache, "gone")); err == nil {
		t.Error("emptied set folder was left in the cache")
	}
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("the cache directory itself was removed: %v", err)
	}
}

// TestFSMaker_Remove_NeverPrunesOutsideTheCache: beside the source only the
// .thumbnails directory goes; the (now empty) media folder is not the
// maker's to delete, cache or not.
func TestFSMaker_Remove_NeverPrunesOutsideTheCache(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "set", "album")
	path := filepath.Join(dir, DirName, "clip.mp4.jpg")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*FSMaker{NewFSMaker(nil, nil, nil), NewFSMaker(nil, nil, nil).WithLayout(NewLayout(root, cache))} {
		m.Remove(path)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("the media folder was removed: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Error("the emptied .thumbnails directory was left behind")
	}
}
