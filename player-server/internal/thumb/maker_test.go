package thumb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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

// Lstat reports any path that is not a file as a real directory.
func (f *fakeFS) Lstat(name string) (os.FileInfo, error) {
	if size, ok := f.files[name]; ok {
		return fakeInfo{size: size}, nil
	}
	return fakeInfo{dir: true}, nil
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

type fakeInfo struct {
	size int64
	dir  bool
}

func (i fakeInfo) Name() string       { return "" }
func (i fakeInfo) Size() int64        { return i.size }
func (i fakeInfo) Mode() os.FileMode  { return 0o644 }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.dir }
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

// makerFailure is one way a thumbnail can fail to be made. setup arranges
// the failure and returns the generator to run; wantGen says whether the
// generator may be started at all.
type makerFailure struct {
	name    string
	setup   func(f *fakeFS) *MockGenerator
	wantGen bool
}

func makerFailures() []makerFailure {
	boom := errors.New("boom")
	failing := func(partial int64) func(f *fakeFS) *MockGenerator {
		return func(f *fakeFS) *MockGenerator {
			return &MockGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
				if partial > 0 {
					f.files[out] = partial
				}
				return boom
			}}
		}
	}
	return []makerFailure{
		{"mkdir fails (unwritable folder)", func(f *fakeFS) *MockGenerator { f.mkdirErr = fs.ErrPermission; return writing(f, 9) }, false},
		{"temporary file cannot be created (read-only .thumbnails)", func(f *fakeFS) *MockGenerator { f.tempErr = fs.ErrPermission; return writing(f, 9) }, false},
		{"generator fails", failing(0), true},
		{"generator fails after partial output", failing(3), true},
		{"generator succeeds without output", func(f *fakeFS) *MockGenerator {
			return &MockGenerator{GenerateFunc: func(_ context.Context, _, out string, _ float64) error {
				delete(f.files, out)
				return nil
			}}
		}, true},
		{"generator succeeds with empty output", func(f *fakeFS) *MockGenerator { return writing(f, 0) }, true},
		{"rename into place fails", func(f *fakeFS) *MockGenerator { f.renameErr = boom; return writing(f, 9) }, true},
	}
}

// TestFSMaker_Failures: whatever goes wrong, Make reports an error and the
// scanner-facing MakeVideo "no thumbnail"; no temporary file is left, and a
// thumbnail already at the destination stays exactly as it was.
func TestFSMaker_Failures(t *testing.T) {
	for _, tt := range makerFailures() {
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
			m := NewFSMaker(gen, fsys, nil)
			if got, err := m.Make(context.Background(), "/s/v.mp4", 1); got != "" || err == nil {
				t.Fatalf("Make = %q, %v; want no path and an error", got, err)
			}
			if got := m.MakeVideo(context.Background(), "/s/v.mp4", 1); got != "" {
				t.Fatalf("MakeVideo = %q, want none", got)
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

// TestFSMaker_ReserveSurvivesDirectoryRemoval: Remove deletes an emptied
// .thumbnails directory, possibly between a concurrent Make creating the
// directory and creating its temporary file. Make then creates it again.
func TestFSMaker_ReserveSurvivesDirectoryRemoval(t *testing.T) {
	fsys := &vanishingDirFS{fakeFS: newFakeFS(), failures: 1}
	if got, err := NewFSMaker(writing(fsys.fakeFS, 5), fsys, nil).Make(context.Background(), "/s/v.mp4", 1); err != nil || got == "" {
		t.Fatalf("Make = %q, %v; want it to retry and succeed", got, err)
	}
	if len(fsys.mkdirs) != 2 {
		t.Errorf("directory created %d times, want 2", len(fsys.mkdirs))
	}

	fsys = &vanishingDirFS{fakeFS: newFakeFS(), failures: 5}
	if _, err := NewFSMaker(writing(fsys.fakeFS, 5), fsys, nil).Make(context.Background(), "/s/v.mp4", 1); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Make error = %v, want it to give up with the not-exist error", err)
	}
}

// vanishingDirFS fails the first CreateTemp calls as if the directory had
// just been removed.
type vanishingDirFS struct {
	*fakeFS
	failures int
}

func (f *vanishingDirFS) CreateTemp(dir string) (string, error) {
	if f.failures > 0 {
		f.failures--
		return "", fs.ErrNotExist
	}
	return f.fakeFS.CreateTemp(dir)
}

// TestFSMaker_Remove: the thumbnail goes, and its directory once empty;
// anything not directly inside a .thumbnails directory is refused.
func TestFSMaker_Remove(t *testing.T) {
	dir := t.TempDir()
	write := func(path string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	exists := func(path string) bool { _, err := os.Stat(path); return err == nil }
	m := NewFSMaker(nil, nil, nil) // a maker without generator can still remove

	a := write(filepath.Join(dir, DirName, "a.mp4.jpg"))
	b := write(filepath.Join(dir, DirName, "b.mp4.jpg"))
	media := write(filepath.Join(dir, "media.jpg"))
	nested := write(filepath.Join(dir, DirName, "sub", "c.jpg"))

	m.Remove(a)
	if exists(a) || !exists(b) || !exists(filepath.Dir(b)) {
		t.Fatalf("after removing a: a=%v b=%v dir=%v", exists(a), exists(b), exists(filepath.Dir(b)))
	}
	m.Remove(a) // already gone: no error, nothing else touched
	m.Remove(media)
	m.Remove(nested)
	m.Remove("")
	if !exists(media) || !exists(nested) {
		t.Fatal("a file outside a .thumbnails directory was removed")
	}
	if err := os.RemoveAll(filepath.Dir(nested)); err != nil {
		t.Fatal(err)
	}
	m.Remove(b)
	if exists(filepath.Join(dir, DirName)) {
		t.Error("emptied .thumbnails directory was left behind")
	}
}

// TestFSMaker_RemoveFailureIsLoggedOnly: a thumbnail that cannot be removed
// stays, and so does its directory.
func TestFSMaker_RemoveFailureIsLoggedOnly(t *testing.T) {
	fsys := newFakeFS()
	path := filepath.Join("/s", DirName, "v.mp4.jpg")
	fsys.files[path] = 1
	fsys.removeErr = fs.ErrPermission
	NewFSMaker(nil, fsys, nil).Remove(path)
	if _, ok := fsys.files[path]; !ok {
		t.Error("file vanished despite the failing remove")
	}
}

func TestIsTemporary(t *testing.T) {
	dir := t.TempDir()
	tmp, err := osMakerFS{}.CreateTemp(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		filepath.Base(tmp): true, // what the maker really creates
		".tmp-123.jpg":     true,
		"clip.mp4.jpg":     false,
		".tmp-123.png":     false,
		"x.tmp-123.jpg":    false,
		"":                 false,
	} {
		if got := IsTemporary(name); got != want {
			t.Errorf("IsTemporary(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestFSMaker_RemoveKeepsSymlinkedDirectory: a .thumbnails that is a
// symbolic link (thumbnails kept on another volume) must survive its last
// thumbnail being removed; os.Remove would unlink the link itself.
func TestFSMaker_RemoveKeepsSymlinkedDirectory(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	link := filepath.Join(dir, DirName)
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	thumbnail := filepath.Join(link, "clip.mp4.jpg")
	if err := os.WriteFile(thumbnail, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	NewFSMaker(nil, nil, nil).Remove(thumbnail)
	if _, err := os.Stat(thumbnail); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("thumbnail not removed: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the symbolic link was removed: %v", err)
	}
	if _, err := os.Stat(elsewhere); err != nil {
		t.Errorf("the link's target was removed: %v", err)
	}
}

// ffmpegClip creates a test video with the real ffmpeg, or skips the test
// where ffmpeg is missing or cannot encode one. args follow the inputs.
func ffmpegClip(t *testing.T, path string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	args = append([]string{"-loglevel", "error"}, append(args, "-y", path)...)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("cannot create a test video with this ffmpeg: %v: %s", err, out)
	}
}

// TestFFmpeg_ShortAndAudioTailedClips runs the real ffmpeg on the clips a
// random seek used to fail on: a video shorter than the old minimum offset
// of one second, a short one, and one whose audio track outlasts the video,
// so that its reported duration lies behind the last video frame. Every
// single run must produce a thumbnail; the offset is random, hence the
// repetitions.
func TestFFmpeg_ShortAndAudioTailedClips(t *testing.T) {
	video := func(seconds string) []string {
		return []string{"-f", "lavfi", "-i", "testsrc=d=" + seconds + ":s=64x64"}
	}
	clips := []struct {
		name     string
		duration float64 // as ffprobe reports it: the container's
		args     []string
	}{
		{"0.6 s video", 0.6, video("0.6")},
		{"2 s video", 2, video("2")},
		{"2 s video with 3 s audio", 3, append(video("2"), "-f", "lavfi", "-i", "sine=d=3")},
		{"1 s video with 8 s audio", 8, append(video("1"), "-f", "lavfi", "-i", "sine=d=8")},
	}
	const runs = 10
	m := NewFSMaker(NewFFmpegGenerator(), nil, nil)
	for _, clip := range clips {
		t.Run(clip.name, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "clip.mp4")
			ffmpegClip(t, src, clip.args...)
			failures := 0
			for i := 0; i < runs; i++ {
				got, err := m.Make(context.Background(), src, clip.duration)
				if err != nil || got == "" {
					failures++
					t.Logf("run %d: %v", i, err)
				}
			}
			if failures != 0 {
				t.Errorf("%d of %d runs produced no thumbnail", failures, runs)
			}
		})
	}
}

// TestFFmpeg_PercentInNames runs the real ffmpeg on a video whose name and
// directory contain "%03d". ffmpeg expands that in an OUTPUT file name as an
// image sequence pattern: it then writes a differently named file, or none,
// and can still exit successfully. Skipped where ffmpeg is not installed.
//
// Only the output side is covered here, with a video as input. A "%d" in
// the name of a source IMAGE is covered by TestFFmpeg_RealHostileNames.
func TestFFmpeg_PercentInNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d%03d")
	src := filepath.Join(dir, "a%03d.mp4")
	ffmpegClip(t, src, "-f", "lavfi", "-i", "testsrc=d=6:s=64x64")

	const duration = 6
	got, err := NewFSMaker(NewFFmpegGenerator(), nil, nil).Make(context.Background(), src, duration)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if want := filepath.Join(dir, DirName, "a%03d.mp4.jpg"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || info.Size() == 0 {
		t.Fatalf("thumbnail missing or empty: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(got))
	if err != nil || len(entries) != 1 {
		t.Errorf("thumbnail directory holds %d entries (err %v), want only the thumbnail", len(entries), err)
	}

	// The generator on its own, onto a final path with a pattern in it, as
	// set covers are written.
	cover := filepath.Join(dir, "cover%03d.jpg")
	if err := NewFFmpegGenerator().Generate(context.Background(), src, cover, duration); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if info, err := os.Stat(cover); err != nil || info.Size() == 0 {
		t.Fatalf("ffmpeg did not write the literal output name: %v", err)
	}
}
