package thumb

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLayout_PathFor(t *testing.T) {
	cache := NewLayout("/media", "/thumbs")
	tests := []struct {
		name   string
		layout Layout
		src    string
		want   string // "" = the layout has no place for it
	}{
		{"zero layout: beside the source", Layout{}, "/media/set/a/clip.mp4", "/media/set/a/.thumbnails/clip.mp4.jpg"},
		{"no cache: beside the source", NewLayout("/media", ""), "/media/set/clip.mp4", "/media/set/.thumbnails/clip.mp4.jpg"},
		{"no cache: beside a source outside the root too", NewLayout("/media", ""), "/elsewhere/clip.mp4", "/elsewhere/.thumbnails/clip.mp4.jpg"},
		{"cache mirrors the media tree", cache, "/media/set/a/clip.mp4", "/thumbs/set/a/.thumbnails/clip.mp4.jpg"},
		{"file directly in a set", cache, "/media/set/clip.mp4", "/thumbs/set/.thumbnails/clip.mp4.jpg"},
		{"set name with a space", cache, "/media/Music Videos/x.mkv", "/thumbs/Music Videos/.thumbnails/x.mkv.jpg"},
		{"unclean source path", cache, "/media/set/../set/./clip.mp4", "/thumbs/set/.thumbnails/clip.mp4.jpg"},
		{"unclean roots", NewLayout("/media/", "/thumbs/x/.."), "/media/set/clip.mp4", "/thumbs/set/.thumbnails/clip.mp4.jpg"},
		// With a cache, a source outside the media root gets no thumbnail
		// at all: neither mapped into the cache nor written beside it.
		{"source outside the media root", cache, "/elsewhere/clip.mp4", ""},
		{"source above the media root", cache, "/clip.mp4", ""},
		{"sibling sharing the root's prefix", cache, "/media-old/set/clip.mp4", ""},
		{"dot-dot out of the media root", cache, "/media/../etc/clip.mp4", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.layout.PathFor(tt.src)
			if ok != (tt.want != "") || got != tt.want {
				t.Fatalf("PathFor(%q) = %q, %v; want %q", tt.src, got, ok, tt.want)
			}
			if !ok {
				return
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

// TestNewLayout_OverlappingCacheIsIgnored: a cache that is, contains or lies
// inside the media root would turn media folders into cache folders (or the
// cache into a set). Configuration loading rejects it; the Layout itself
// never uses it.
func TestNewLayout_OverlappingCacheIsIgnored(t *testing.T) {
	const src = "/data/media/set/clip.mp4"
	for _, cache := range []string{"/data", "/", "/data/media", "/data/media/", "/data/media/.cache", "/data/x/../media/c"} {
		l := NewLayout("/data/media", cache)
		if got, ok := l.Cached(src); ok {
			t.Errorf("cache %q: Cached = %q, want no cache", cache, got)
		}
		if got, ok := l.PathFor(src); !ok || got != ThumbnailPathFor(src) {
			t.Errorf("cache %q: PathFor = %q, %v; want the path beside the source", cache, got, ok)
		}
		for _, dir := range []string{"/data/media/set", "/data/media", "/data/other"} {
			if l.inCache(dir) {
				t.Errorf("cache %q: %q counts as a cache folder", cache, dir)
			}
		}
	}
	// A sibling that only shares the root's prefix is a proper cache.
	if _, ok := NewLayout("/data/media", "/data/media-thumbs").Cached(src); !ok {
		t.Error("a disjoint sibling directory was not accepted as cache")
	}
}

// TestNewLayout_EmptyMediaRoot: an unset media root is "unknown", not the
// working directory, and such a layout has no cache.
func TestNewLayout_EmptyMediaRoot(t *testing.T) {
	l := NewLayout("", "/thumbs")
	if l != (Layout{}) || l.CacheDir() != "" {
		t.Errorf("NewLayout(\"\", cache) = %+v, want the zero Layout", l)
	}
	if got := NewLayout("/media", "/thumbs/").CacheDir(); got != "/thumbs" {
		t.Errorf("CacheDir = %q, want /thumbs", got)
	}
	if got := NewLayout("/media", "/media/x").CacheDir(); got != "" {
		t.Errorf("CacheDir of an ignored cache = %q, want none", got)
	}
}

func TestLayout_ServesAndCachedDir(t *testing.T) {
	l := NewLayout("/media", "/thumbs")
	for path, want := range map[string]bool{
		"/media/set/.thumbnails/x.jpg":  true,
		"/thumbs/set/.thumbnails/x.jpg": true,
		"/thumbs":                       false,
		"/thumbs-old/set/.thumbnails/x": false,
		"/thumbs/../etc/.thumbnails/x":  false,
		"/elsewhere/.thumbnails/x.jpg":  false,
		"/media/../elsewhere/x.jpg":     false,
	} {
		if got := l.Serves(path); got != want {
			t.Errorf("Serves(%q) = %v, want %v", path, got, want)
		}
	}
	if NewLayout("/media", "").Serves("/thumbs/set/.thumbnails/x.jpg") {
		t.Error("without a cache a former cache path counts as served")
	}

	if got, ok := l.CachedDir("/media/set/a"); !ok || got != "/thumbs/set/a" {
		t.Errorf("CachedDir = %q, %v; want /thumbs/set/a", got, ok)
	}
	for _, dir := range []string{"/elsewhere", "/", "/media/../etc", "/media-old/set"} {
		if got, ok := l.CachedDir(dir); ok {
			t.Errorf("CachedDir(%q) = %q, want none", dir, got)
		}
	}
	if _, ok := NewLayout("/media", "").CachedDir("/media/set"); ok {
		t.Error("CachedDir without a cache")
	}
}

func TestLayout_InMediaRoot(t *testing.T) {
	l := NewLayout("/media", "/thumbs")
	for path, want := range map[string]bool{
		"/media/set/.thumbnails/x.jpg":  true,
		"/media":                        false,
		"/thumbs/set/.thumbnails/x.jpg": false,
		"/media-old/set/x.jpg":          false,
		"/media/../etc/x.jpg":           false,
	} {
		if got := l.InMediaRoot(path); got != want {
			t.Errorf("InMediaRoot(%q) = %v, want %v", path, got, want)
		}
	}
	if !(Layout{}).InMediaRoot("/anywhere/x.jpg") {
		t.Error("a layout without a media root must not rule paths out")
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

// TestFSMaker_Cache_SourceOutsideMediaRoot: with a cache configured a file
// outside the media root (a stale row of a copied database, say) gets no
// thumbnail; in particular none is written next to it.
func TestFSMaker_Cache_SourceOutsideMediaRoot(t *testing.T) {
	root, cache, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	src := filepath.Join(elsewhere, "clip.mp4")
	if err := os.WriteFile(src, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewFSMaker(textGen(), nil, nil).WithLayout(NewLayout(root, cache))
	if got, err := m.Make(context.Background(), src, 1); !errors.Is(err, ErrOutsideMediaRoot) {
		t.Fatalf("Make = %q, %v; want ErrOutsideMediaRoot", got, err)
	}
	if got := m.MakeImage(context.Background(), src); got != "" {
		t.Errorf("MakeImage = %q, want no thumbnail", got)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 1 {
		t.Errorf("the foreign directory was written to: %v", entries)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("the cache was written to: %v", entries)
	}
}

// missingParentFS fails MkdirAll a number of times with err ("no such file
// or directory" unless set): what a concurrent Remove pruning the folders
// above causes.
type missingParentFS struct {
	*fakeFS
	failures int
	err      error
}

func (f *missingParentFS) MkdirAll(path string, perm os.FileMode) error {
	if f.failures > 0 {
		f.failures--
		cause := f.err
		if cause == nil {
			cause = fs.ErrNotExist
		}
		return &os.PathError{Op: "mkdir", Path: path, Err: cause}
	}
	return f.fakeFS.MkdirAll(path, perm)
}

// TestFSMaker_ReserveSurvivesParentRemoval: pruning emptied cache folders
// can remove a parent while MkdirAll is walking down it. That is retried,
// and only given up on when it keeps happening.
func TestFSMaker_ReserveSurvivesParentRemoval(t *testing.T) {
	fsys := &missingParentFS{fakeFS: newFakeFS(), failures: reserveAttempts - 1}
	if got, err := NewFSMaker(writing(fsys.fakeFS, 5), fsys, nil).Make(context.Background(), "/s/v.mp4", 1); err != nil || got == "" {
		t.Fatalf("Make = %q, %v; want it to retry and succeed", got, err)
	}
	fsys = &missingParentFS{fakeFS: newFakeFS(), failures: reserveAttempts}
	if _, err := NewFSMaker(writing(fsys.fakeFS, 5), fsys, nil).Make(context.Background(), "/s/v.mp4", 1); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Make error = %v, want it to give up with the not-exist error", err)
	}
}

// TestFSMaker_ReserveRetriesFileExists: os.MkdirAll reports "file exists"
// when a folder it found present is pruned before it looks again. That is
// the same race and retried the same way. A cause that stays (a dangling
// symbolic link in the way) ends as a mkdir error, and an error of another
// kind is not retried at all.
func TestFSMaker_ReserveRetriesFileExists(t *testing.T) {
	fsys := &missingParentFS{fakeFS: newFakeFS(), failures: reserveAttempts - 1, err: fs.ErrExist}
	if got, err := NewFSMaker(writing(fsys.fakeFS, 5), fsys, nil).Make(context.Background(), "/s/v.mp4", 1); err != nil || got == "" {
		t.Fatalf("Make = %q, %v; want it to retry and succeed", got, err)
	}

	fsys = &missingParentFS{fakeFS: newFakeFS(), failures: reserveAttempts, err: fs.ErrExist}
	_, err := NewFSMaker(writing(fsys.fakeFS, 5), fsys, nil).Make(context.Background(), "/s/v.mp4", 1)
	if !errors.Is(err, fs.ErrExist) || !strings.HasPrefix(err.Error(), "mkdir thumbnails") {
		t.Fatalf("Make error = %v, want a mkdir error wrapping file-exists", err)
	}

	fsys = &missingParentFS{fakeFS: newFakeFS(), failures: reserveAttempts, err: syscall.ENOTDIR}
	if _, err := NewFSMaker(writing(fsys.fakeFS, 5), fsys, nil).Make(context.Background(), "/s/v.mp4", 1); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("Make error = %v, want ENOTDIR", err)
	}
	if fsys.failures != reserveAttempts-1 {
		t.Errorf("a regular file in the way was retried %d times, want no retry", reserveAttempts-1-fsys.failures)
	}
}

// TestFSMaker_Cache_ConcurrentMakeAndRemove runs the race for real: one
// goroutine keeps creating and removing a thumbnail (which prunes the
// folders above it) while another creates thumbnails in a neighbouring
// folder of the same cache subtree. No Make may fail.
func TestFSMaker_Cache_ConcurrentMakeAndRemove(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	m := NewFSMaker(textGen(), nil, nil).WithLayout(NewLayout(root, cache))
	a := filepath.Join(root, "set", "x", "a", "clip.mp4")
	b := filepath.Join(root, "set", "x", "b", "clip.mp4")
	const rounds = 2000
	errs := make(chan error, 2)
	loop := func(src string, remove bool) {
		for i := 0; i < rounds; i++ {
			dst, err := m.Make(context.Background(), src, 1)
			if err != nil {
				errs <- err
				return
			}
			if remove {
				m.Remove(dst)
			}
		}
		errs <- nil
	}
	go loop(a, true)
	go loop(b, true)
	// Wait for both before failing: a goroutine still writing would break
	// the removal of the test's temporary directories.
	first, second := <-errs, <-errs
	if err := errors.Join(first, second); err != nil {
		t.Fatalf("Make failed while a neighbour was being removed: %v", err)
	}
}

// TestFSMaker_Remove_KeepsSymlinkedCacheFolder: a cache folder that is a
// symbolic link (part of the cache kept on another volume) is not unlinked
// when the folder below it becomes empty.
func TestFSMaker_Remove_KeepsSymlinkedCacheFolder(t *testing.T) {
	root, cache, target := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Symlink(target, filepath.Join(cache, "set")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	thumbnail := filepath.Join(cache, "set", "a", DirName, "clip.mp4.jpg")
	if err := os.MkdirAll(filepath.Dir(thumbnail), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumbnail, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	NewFSMaker(nil, nil, nil).WithLayout(NewLayout(root, cache)).Remove(thumbnail)
	if _, err := os.Lstat(filepath.Join(cache, "set")); err != nil {
		t.Errorf("the symlinked cache folder was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "a")); err == nil {
		t.Error("the emptied folder below the link was left behind")
	}
}

// TestFSMaker_Remove_OverlappingCacheNeverPrunesMedia: even if a cache
// directory containing the media root reached the maker, removing a
// thumbnail must not take the emptied media folders with it.
func TestFSMaker_Remove_OverlappingCacheNeverPrunesMedia(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "media")
	thumbnail := filepath.Join(root, "set", "album", DirName, "clip.mp4.jpg")
	if err := os.MkdirAll(filepath.Dir(thumbnail), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumbnail, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	NewFSMaker(nil, nil, nil).WithLayout(NewLayout(root, base)).Remove(thumbnail)
	if _, err := os.Stat(filepath.Join(root, "set", "album")); err != nil {
		t.Errorf("an emptied media folder was removed: %v", err)
	}
}

func TestCheckCacheDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "cache")
	if err := CheckCacheDir(dir); err != nil {
		t.Fatalf("CheckCacheDir on a creatable directory: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the check left files behind: %v", entries)
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := CheckCacheDir(dir); err == nil {
		t.Error("an unwritable cache directory passed the check")
	}
	if err := CheckCacheDir(filepath.Join(dir, "below")); err == nil {
		t.Error("a cache directory that cannot be created passed the check")
	}
}
