package scanner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// mockDirEntry implements os.DirEntry for tests.
type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string      { return m.name }
func (m mockDirEntry) IsDir() bool       { return m.isDir }
func (m mockDirEntry) Type() os.FileMode { return 0 }
func (m mockDirEntry) Info() (os.FileInfo, error) {
	return mockFileInfo{name: m.name, isDir: m.isDir}, nil
}

// mockFileInfo implements os.FileInfo for tests.
type mockFileInfo struct {
	name    string
	size    int64
	isDir   bool
	modTime time.Time
	mode    os.FileMode
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m mockFileInfo) ModTime() time.Time { return m.modTime }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

// walkEntry describes a single path yielded by mockFS.WalkDir.
type walkEntry struct {
	path  string
	isDir bool
}

// mockFS implements FS for tests.
type mockFS struct {
	entries   map[string][]os.DirEntry
	fileInfos map[string]os.FileInfo
	walkList  []walkEntry
	walkErr   error
	mkdirErr  error
}

func (m *mockFS) ReadDir(name string) ([]os.DirEntry, error) {
	if ents, ok := m.entries[name]; ok {
		return ents, nil
	}
	return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
}

func (m *mockFS) Stat(name string) (os.FileInfo, error) {
	if info, ok := m.fileInfos[name]; ok {
		return info, nil
	}
	return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
}

func (m *mockFS) MkdirAll(path string, perm os.FileMode) error { return m.mkdirErr }

func (m *mockFS) WalkDir(root string, walkFn fs.WalkDirFunc) error {
	if m.walkErr != nil {
		return m.walkErr
	}
	var skipDirs []string
	for _, e := range m.walkList {
		cleanRoot := filepath.Clean(root)
		cleanPath := filepath.Clean(e.path)
		if !strings.HasPrefix(cleanPath, cleanRoot) {
			continue
		}
		skipped := false
		for _, sd := range skipDirs {
			if strings.HasPrefix(cleanPath, sd) {
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		de := mockDirEntry{name: filepath.Base(e.path), isDir: e.isDir}
		err := walkFn(e.path, de, nil)
		if err == filepath.SkipDir {
			skipDirs = append(skipDirs, cleanPath)
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func newTestScanner(store repository.ScannerStore, prober probe.Prober, gen thumb.Generator, clk clock.Clock, filesystem FS) *FSScanner {
	return &FSScanner{
		store:    store,
		prober:   prober,
		thumbGen: gen,
		clock:    clk,
		fs:       filesystem,
	}
}

func TestFSScanner_Scan(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &clock.MockClock{T: now}
	ctx := context.Background()

	t.Run("empty root", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {},
			},
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) { return nil, nil }
		s := newTestScanner(store, &probe.MockProber{}, &thumb.MockGenerator{}, clk, mfs)
		if err := s.Scan(ctx, "/media", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("creates new set with video and audio", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {mockDirEntry{name: "Movies", isDir: true}},
			},
			fileInfos: map[string]os.FileInfo{
				"/media/Movies/video.mp4": mockFileInfo{name: "video.mp4", size: 1000},
				"/media/Movies/song.mp3":  mockFileInfo{name: "song.mp3", size: 500},
			},
			walkList: []walkEntry{
				{path: "/media/Movies", isDir: true},
				{path: "/media/Movies/video.mp4", isDir: false},
				{path: "/media/Movies/song.mp3", isDir: false},
			},
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) { return nil, nil }
		var createdSetID int64 = 7
		store.SetRepo.CreateSetFunc = func(_ context.Context, set *model.Set) (int64, error) {
			if set.Name != "Movies" || set.RootPath != "Movies" {
				t.Errorf("unexpected set: %+v", set)
			}
			return createdSetID, nil
		}
		store.MediaRepo.ListMediaFunc = func(_ context.Context, filter repository.MediaFilter) ([]model.Media, error) {
			if filter.SetID == nil || *filter.SetID != createdSetID {
				t.Errorf("unexpected filter: %+v", filter)
			}
			return nil, nil
		}

		var created []model.Media
		store.MediaRepo.CreateMediaFunc = func(_ context.Context, m *model.Media) (int64, error) {
			created = append(created, *m)
			return int64(len(created)), nil
		}

		prober := &probe.MockProber{
			ProbeFunc: func(_ context.Context, path string) (*model.Metadata, error) {
				if strings.HasSuffix(path, ".mp4") {
					return &model.Metadata{Duration: 120, Codec: "h264", Resolution: "1920x1080", Bitrate: 1000}, nil
				}
				return &model.Metadata{Duration: 180, Codec: "mp3", Bitrate: 256}, nil
			},
		}

		genCalled := false
		gen := &thumb.MockGenerator{
			GenerateFunc: func(_ context.Context, inputPath, outputPath string, duration float64) error {
				genCalled = true
				if !strings.HasSuffix(inputPath, ".mp4") {
					t.Errorf("unexpected thumbnail input: %s", inputPath)
				}
				return nil
			},
		}

		s := newTestScanner(store, prober, gen, clk, mfs)
		if err := s.Scan(ctx, "/media", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(created) != 2 {
			t.Fatalf("expected 2 media created, got %d", len(created))
		}

		vid := created[0]
		if vid.Type != model.MediaTypeVideo || vid.FileName != "video.mp4" || vid.RelPath != "video.mp4" {
			t.Errorf("unexpected video media: %+v", vid)
		}
		if vid.ThumbnailPath == "" {
			t.Error("expected thumbnail path for video")
		}
		if vid.FileSizeBytes != 1000 {
			t.Errorf("expected file size 1000, got %d", vid.FileSizeBytes)
		}

		aud := created[1]
		if aud.Type != model.MediaTypeAudio || aud.FileName != "song.mp3" || aud.RelPath != "song.mp3" {
			t.Errorf("unexpected audio media: %+v", aud)
		}
		if aud.ThumbnailPath != "" {
			t.Error("expected no thumbnail path for audio")
		}
		if aud.FileSizeBytes != 500 {
			t.Errorf("expected file size 500, got %d", aud.FileSizeBytes)
		}

		if !genCalled {
			t.Error("expected thumbnail generation to be called")
		}
	})

	t.Run("skips existing media", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {mockDirEntry{name: "Music", isDir: true}},
			},
			fileInfos: map[string]os.FileInfo{
				"/media/Music/track.mp3": mockFileInfo{name: "track.mp3", size: 300},
			},
			walkList: []walkEntry{
				{path: "/media/Music", isDir: true},
				{path: "/media/Music/track.mp3", isDir: false},
			},
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) {
			return []model.Set{{ID: 1, Name: "Music", RootPath: "Music"}}, nil
		}
		store.MediaRepo.ListMediaFunc = func(_ context.Context, filter repository.MediaFilter) ([]model.Media, error) {
			return []model.Media{{ID: 10, SetID: 1, RelPath: "track.mp3"}}, nil
		}
		var created int
		store.MediaRepo.CreateMediaFunc = func(_ context.Context, m *model.Media) (int64, error) {
			created++
			return 0, nil
		}

		s := newTestScanner(store, &probe.MockProber{}, &thumb.MockGenerator{}, clk, mfs)
		if err := s.Scan(ctx, "/media", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created != 0 {
			t.Fatalf("expected 0 new media, got %d", created)
		}
	})

	t.Run("nested directories", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {mockDirEntry{name: "Series", isDir: true}},
			},
			fileInfos: map[string]os.FileInfo{
				"/media/Series/season1/ep1.mp4": mockFileInfo{name: "ep1.mp4", size: 2000},
			},
			walkList: []walkEntry{
				{path: "/media/Series", isDir: true},
				{path: "/media/Series/season1", isDir: true},
				{path: "/media/Series/season1/ep1.mp4", isDir: false},
			},
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) { return nil, nil }
		store.SetRepo.CreateSetFunc = func(_ context.Context, set *model.Set) (int64, error) { return 3, nil }
		store.MediaRepo.ListMediaFunc = func(_ context.Context, filter repository.MediaFilter) ([]model.Media, error) { return nil, nil }

		var created model.Media
		store.MediaRepo.CreateMediaFunc = func(_ context.Context, m *model.Media) (int64, error) {
			created = *m
			return 1, nil
		}

		prober := &probe.MockProber{
			ProbeFunc: func(_ context.Context, path string) (*model.Metadata, error) {
				return &model.Metadata{Duration: 45}, nil
			},
		}

		s := newTestScanner(store, prober, &thumb.MockGenerator{}, clk, mfs)
		if err := s.Scan(ctx, "/media", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created.RelPath != filepath.Join("season1", "ep1.mp4") {
			t.Errorf("unexpected nested rel path: %s", created.RelPath)
		}
	})

	t.Run("read dir error", func(t *testing.T) {
		mfs := &mockFS{entries: map[string][]os.DirEntry{}}
		s := newTestScanner(repository.NewMockStore(), &probe.MockProber{}, &thumb.MockGenerator{}, clk, mfs)
		err := s.Scan(ctx, "/media", nil)
		if err == nil {
			t.Fatal("expected error for missing root")
		}
	})

	t.Run("stat error", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {mockDirEntry{name: "Set", isDir: true}},
			},
			walkList: []walkEntry{
				{path: "/media/Set", isDir: true},
				{path: "/media/Set/file.mp4", isDir: false},
			},
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) { return nil, nil }
		store.SetRepo.CreateSetFunc = func(_ context.Context, set *model.Set) (int64, error) { return 1, nil }
		store.MediaRepo.ListMediaFunc = func(_ context.Context, filter repository.MediaFilter) ([]model.Media, error) { return nil, nil }

		s := newTestScanner(store, &probe.MockProber{}, &thumb.MockGenerator{}, clk, mfs)
		err := s.Scan(ctx, "/media", nil)
		if err == nil {
			t.Fatal("expected error for stat failure")
		}
	})

	t.Run("probe error", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {mockDirEntry{name: "Set", isDir: true}},
			},
			fileInfos: map[string]os.FileInfo{
				"/media/Set/bad.mp4": mockFileInfo{name: "bad.mp4", size: 100},
			},
			walkList: []walkEntry{
				{path: "/media/Set", isDir: true},
				{path: "/media/Set/bad.mp4", isDir: false},
			},
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) { return nil, nil }
		store.SetRepo.CreateSetFunc = func(_ context.Context, _ *model.Set) (int64, error) { return 1, nil }
		store.MediaRepo.ListMediaFunc = func(_ context.Context, _ repository.MediaFilter) ([]model.Media, error) { return nil, nil }
		prober := &probe.MockProber{
			ProbeFunc: func(_ context.Context, _ string) (*model.Metadata, error) {
				return nil, errors.New("probe failed")
			},
		}

		s := newTestScanner(store, prober, &thumb.MockGenerator{}, clk, mfs)
		// Unprobeable files are skipped with a log instead of failing the whole scan.
		err := s.Scan(ctx, "/media", nil)
		if err != nil {
			t.Fatalf("unexpected error for probe failure; expected skip, got: %v", err)
		}
		if store.MediaRepo.CreateMediaFunc != nil {
			// no media should have been created for the bad file
		}
	})

	t.Run("thumbnail generation error", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {mockDirEntry{name: "Set", isDir: true}},
			},
			fileInfos: map[string]os.FileInfo{
				"/media/Set/video.mp4": mockFileInfo{name: "video.mp4", size: 100},
			},
			walkList: []walkEntry{
				{path: "/media/Set", isDir: true},
				{path: "/media/Set/video.mp4", isDir: false},
			},
			mkdirErr: nil,
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) { return nil, nil }
		store.SetRepo.CreateSetFunc = func(_ context.Context, _ *model.Set) (int64, error) { return 1, nil }
		store.MediaRepo.ListMediaFunc = func(_ context.Context, _ repository.MediaFilter) ([]model.Media, error) { return nil, nil }
		prober := &probe.MockProber{
			ProbeFunc: func(_ context.Context, _ string) (*model.Metadata, error) {
				return &model.Metadata{Duration: 60}, nil
			},
		}
		gen := &thumb.MockGenerator{
			GenerateFunc: func(_ context.Context, _, _ string, _ float64) error {
				return errors.New("thumb failed")
			},
		}

		s := newTestScanner(store, prober, gen, clk, mfs)
		// Thumbnail generation errors are skipped so the scan continues.
		err := s.Scan(ctx, "/media", nil)
		if err != nil {
			t.Fatalf("unexpected error for thumbnail failure; expected skip, got: %v", err)
		}
	})

	t.Run("walk error", func(t *testing.T) {
		mfs := &mockFS{
			entries: map[string][]os.DirEntry{
				"/media": {mockDirEntry{name: "Set", isDir: true}},
			},
			walkErr: errors.New("walk failed"),
		}
		store := repository.NewMockStore()
		store.SetRepo.ListSetsFunc = func(_ context.Context) ([]model.Set, error) {
			return []model.Set{{ID: 1, Name: "Set", RootPath: "Set"}}, nil
		}
		store.MediaRepo.ListMediaFunc = func(_ context.Context, _ repository.MediaFilter) ([]model.Media, error) { return nil, nil }

		s := newTestScanner(store, &probe.MockProber{}, &thumb.MockGenerator{}, clk, mfs)
		err := s.Scan(ctx, "/media", nil)
		if err == nil {
			t.Fatal("expected error for walk failure")
		}
	})
}

func TestFSScanner_collectFiles(t *testing.T) {
	t.Run("collects supported media files excluding hidden directories", func(t *testing.T) {
		mfs := &mockFS{
			walkList: []walkEntry{
				{path: "/music", isDir: true},
				{path: "/music/track.mp3", isDir: false},
				{path: "/music/cover.jpg", isDir: false},
				{path: "/music/.hidden", isDir: true},
				{path: "/music/.hidden/secret.mp3", isDir: false},
			},
		}
		s := newTestScanner(nil, nil, nil, nil, mfs)
		files, err := s.collectFiles("/music")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("expected 2 supported files, got %v", files)
		}
		if files[0] != "/music/track.mp3" || files[1] != "/music/cover.jpg" {
			t.Fatalf("unexpected files: %v", files)
		}
	})

	t.Run("skips dot directories", func(t *testing.T) {
		mfs := &mockFS{
			walkList: []walkEntry{
				{path: "/music", isDir: true},
				{path: "/music/.hidden", isDir: true},
				{path: "/music/.hidden/secret.mp3", isDir: false},
			},
		}
		s := newTestScanner(nil, nil, nil, nil, mfs)
		files, err := s.collectFiles("/music")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Fatalf("expected 0 files from dot dir, got %v", files)
		}
	})

	t.Run("walk error", func(t *testing.T) {
		mfs := &mockFS{walkErr: errors.New("walk failed")}
		s := newTestScanner(nil, nil, nil, nil, mfs)
		_, err := s.collectFiles("/bad")
		if err == nil {
			t.Fatal("expected error for walk failure")
		}
	})

	t.Run("ignores unsupported extensions", func(t *testing.T) {
		mfs := &mockFS{
			walkList: []walkEntry{
				{path: "/stuff", isDir: true},
				{path: "/stuff/file.txt", isDir: false},
				{path: "/stuff/notes.md", isDir: false},
				{path: "/stuff/track.mp3", isDir: false},
			},
		}
		s := newTestScanner(nil, nil, nil, nil, mfs)
		files, err := s.collectFiles("/stuff")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 || files[0] != "/stuff/track.mp3" {
			t.Fatalf("expected only mp3, got %v", files)
		}
	})
}

