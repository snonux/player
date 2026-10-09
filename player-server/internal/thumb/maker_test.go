package thumb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFS is an in-memory MakerFS: files maps a path to its size. Each
// operation can be made to fail.
type fakeFS struct {
	files     map[string]int64
	mkdirs    []string
	temps     int
	mkdirErr  error
	tempErr   error
	renameErr error
	removeErr error
}

func newFakeFS() *fakeFS { return &fakeFS{files: map[string]int64{}} }

func (f *fakeFS) MkdirAll(path string, _ os.FileMode) error {
	f.mkdirs = append(f.mkdirs, path)
	return f.mkdirErr
}

func (f *fakeFS) CreateTemp(dir string) (string, error) {
	if f.tempErr != nil {
		return "", f.tempErr
	}
	f.temps++
	name := filepath.Join(dir, fmt.Sprintf(".tmp-%d.jpg", f.temps))
	f.files[name] = 0
	return name, nil
}

func (f *fakeFS) Stat(name string) (os.FileInfo, error) {
	size, ok := f.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return fakeInfo{size: size}, nil
}

func (f *fakeFS) Rename(oldPath, newPath string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	f.files[newPath] = f.files[oldPath]
	delete(f.files, oldPath)
	return nil
}

func (f *fakeFS) Remove(name string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	if _, ok := f.files[name]; !ok {
		return fs.ErrNotExist
	}
	delete(f.files, name)
	return nil
}

// temporaries returns the leftover temporary files.
func (f *fakeFS) temporaries() []string {
	var left []string
	for name := range f.files {
		if strings.HasPrefix(filepath.Base(name), ".tmp-") {
			left = append(left, name)
		}
	}
	return left
}

type fakeInfo struct{ size int64 }

func (i fakeInfo) Name() string       { return "" }
func (i fakeInfo) Size() int64        { return i.size }
func (i fakeInfo) Mode() os.FileMode  { return 0o644 }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return false }
func (i fakeInfo) Sys() any           { return nil }

// writing returns a generator that "writes" size bytes to its output.
func writing(fsys *fakeFS, size int64) *MockGenerator {
	return &MockGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
		fsys.files[out] = size
		return nil
	}}
}

func TestFSMaker_MakeVideo_GeneratesIntoTempThenRenames(t *testing.T) {
	fsys := newFakeFS()
	var gotInput, gotOutput string
	var gotDuration float64
	gen := &MockGenerator{GenerateFunc: func(_ context.Context, in, out string, dur float64) error {
		gotInput, gotOutput, gotDuration = in, out, dur
		fsys.files[out] = 42
		return nil
	}}
	m := NewFSMaker(gen, fsys, nil)

	got := m.MakeVideo(context.Background(), "/set/movie.mp4", 12.5)
	wantPath := filepath.Join("/set", DirName, "movie.mp4.jpg")
	if got != wantPath {
		t.Fatalf("path = %q, want %q", got, wantPath)
	}
	if gotInput != "/set/movie.mp4" || gotDuration != 12.5 {
		t.Fatalf("input = %q, duration = %v", gotInput, gotDuration)
	}
	// The generator must not write to the final path directly: a killed
	// run would leave a truncated thumbnail there.
	if gotOutput == wantPath || filepath.Dir(gotOutput) != filepath.Dir(wantPath) || filepath.Ext(gotOutput) != ".jpg" {
		t.Fatalf("generator output = %q, want a temporary .jpg next to %q", gotOutput, wantPath)
	}
	if fsys.files[wantPath] != 42 || len(fsys.temporaries()) != 0 {
		t.Fatalf("files after make = %v, want only the finished thumbnail", fsys.files)
	}
	if len(fsys.mkdirs) != 1 || fsys.mkdirs[0] != filepath.Join("/set", DirName) {
		t.Fatalf("mkdir calls = %v", fsys.mkdirs)
	}
}

func TestFSMaker_MakeImage_PassesZeroDuration(t *testing.T) {
	fsys := newFakeFS()
	gotDuration := -1.0
	gen := &MockGenerator{GenerateFunc: func(_ context.Context, _, out string, dur float64) error {
		gotDuration = dur
		fsys.files[out] = 1
		return nil
	}}
	if got := NewFSMaker(gen, fsys, nil).MakeImage(context.Background(), "/p/img.png"); got == "" {
		t.Fatal("expected a thumbnail path")
	}
	if gotDuration != 0 {
		t.Fatalf("duration = %v, want 0", gotDuration)
	}
}

// TestFSMaker_Failures: whatever goes wrong, the maker reports "no
// thumbnail" instead of an error, leaves no temporary file, and leaves a
// thumbnail already at the destination exactly as it was.
func TestFSMaker_Failures(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		setup   func(f *fakeFS) *MockGenerator
		wantGen bool // whether the generator may be started at all
	}{
		{"mkdir fails (unwritable folder)", func(f *fakeFS) *MockGenerator { f.mkdirErr = fs.ErrPermission; return writing(f, 9) }, false},
		{"temporary file cannot be created (read-only .thumbnails)", func(f *fakeFS) *MockGenerator { f.tempErr = fs.ErrPermission; return writing(f, 9) }, false},
		{"generator fails", func(*fakeFS) *MockGenerator {
			return &MockGenerator{GenerateFunc: func(context.Context, string, string, float64) error { return boom }}
		}, true},
		{"generator fails after partial output", func(f *fakeFS) *MockGenerator {
			return &MockGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
				f.files[out] = 3
				return boom
			}}
		}, true},
		{"generator succeeds without output", func(f *fakeFS) *MockGenerator {
			return &MockGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
				delete(f.files, out)
				return nil
			}}
		}, true},
		{"generator succeeds with empty output", func(f *fakeFS) *MockGenerator { return writing(f, 0) }, true},
		{"rename into place fails", func(f *fakeFS) *MockGenerator { f.renameErr = boom; return writing(f, 9) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := newFakeFS()
			dst := ThumbnailPathFor("/s/v.mp4")
			fsys.files[dst] = 777 // a thumbnail from an earlier run
			inner := tt.setup(fsys)
			started := false
			gen := &MockGenerator{GenerateFunc: func(ctx context.Context, in, out string, d float64) error {
				started = true
				return inner.Generate(ctx, in, out, d)
			}}
			if got := NewFSMaker(gen, fsys, nil).MakeVideo(context.Background(), "/s/v.mp4", 1); got != "" {
				t.Fatalf("path = %q, want none", got)
			}
			if started != tt.wantGen {
				t.Errorf("generator started = %v, want %v", started, tt.wantGen)
			}
			if fsys.files[dst] != 777 {
				t.Errorf("existing thumbnail was changed: size %d", fsys.files[dst])
			}
			if left := fsys.temporaries(); len(left) != 0 {
				t.Errorf("temporary files left behind: %v", left)
			}
		})
	}
}

// TestFSMaker_TempRemovalFailure: failing to clean up the temporary file is
// logged, nothing more; the result is still "no thumbnail".
func TestFSMaker_TempRemovalFailure(t *testing.T) {
	fsys := newFakeFS()
	fsys.removeErr = fs.ErrPermission
	gen := &MockGenerator{GenerateFunc: func(context.Context, string, string, float64) error { return errors.New("boom") }}
	if got := NewFSMaker(gen, fsys, nil).MakeVideo(context.Background(), "/s/v.mp4", 1); got != "" {
		t.Fatalf("path = %q, want none", got)
	}
}

// TestFSMaker_ReplacesExistingThumbnail: a successful run replaces whatever
// is at the destination, e.g. a leftover of unknown origin.
func TestFSMaker_ReplacesExistingThumbnail(t *testing.T) {
	fsys := newFakeFS()
	dst := ThumbnailPathFor("/s/v.mp4")
	fsys.files[dst] = 777
	if got := NewFSMaker(writing(fsys, 5), fsys, nil).MakeVideo(context.Background(), "/s/v.mp4", 1); got != dst {
		t.Fatalf("path = %q, want %q", got, dst)
	}
	if fsys.files[dst] != 5 {
		t.Errorf("destination holds %d bytes, want the new 5", fsys.files[dst])
	}
}

// TestFSMaker_NestedSourceGetsOwnThumbnail: same-named files in two folders
// of a set must not overwrite each other; each thumbnail goes into the
// .thumbnails directory of its own folder, which the maker creates.
func TestFSMaker_NestedSourceGetsOwnThumbnail(t *testing.T) {
	fsys := newFakeFS()
	m := NewFSMaker(writing(fsys, 1), fsys, nil)

	a := m.MakeVideo(context.Background(), "/set/a/clip.mp4", 1)
	b := m.MakeVideo(context.Background(), "/set/b/clip.mp4", 1)
	if want := filepath.Join("/set", "a", DirName, "clip.mp4.jpg"); a != want {
		t.Fatalf("path = %q, want %q", a, want)
	}
	if b == "" || a == b {
		t.Fatalf("second path = %q, want its own", b)
	}
	if len(fsys.mkdirs) != 2 || fsys.mkdirs[0] != filepath.Dir(a) || fsys.mkdirs[1] != filepath.Dir(b) {
		t.Fatalf("mkdir calls = %v", fsys.mkdirs)
	}
}

// TestFSMaker_RealFilesystem runs the default MakerFS: the finished
// thumbnail must be an ordinary readable file (os.CreateTemp alone would
// leave it 0600) and nothing else may remain in the directory.
func TestFSMaker_RealFilesystem(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	gen := &MockGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
		return os.WriteFile(out, []byte("jpeg"), 0o600) // overwrites the reserved file
	}}
	got := NewFSMaker(gen, nil, nil).MakeVideo(context.Background(), src, 1)
	if got != ThumbnailPathFor(src) {
		t.Fatalf("path = %q", got)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o044 != 0o044 {
		t.Errorf("thumbnail mode = %v, want it readable by group and others", perm)
	}
	entries, err := os.ReadDir(filepath.Dir(got))
	if err != nil || len(entries) != 1 {
		t.Errorf("thumbnail directory holds %d entries (err %v), want 1", len(entries), err)
	}
}

// TestFSMaker_RealUnwritableFolder: in a read-only folder no thumbnail is
// made and the generator is not even started.
func TestFSMaker_RealUnwritableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	started := false
	gen := &MockGenerator{GenerateFunc: func(context.Context, string, string, float64) error {
		started = true
		return nil
	}}
	if got := NewFSMaker(gen, nil, nil).MakeImage(context.Background(), filepath.Join(dir, "img.png")); got != "" {
		t.Fatalf("path = %q, want none", got)
	}
	if started {
		t.Error("generator was started for an unwritable folder")
	}
}
