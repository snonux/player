package thumb

import "testing"

func TestThumbnailDir(t *testing.T) {
	tests := []struct {
		name   string
		parent string
		want   string
	}{
		{"simple", "/media/set1", "/media/set1/.thumbnails"},
		{"trailing slash", "/media/set1/", "/media/set1/.thumbnails"},
		{"double trailing slash", "/media/set1//", "/media/set1/.thumbnails"},
		{"relative", "set1", "set1/.thumbnails"},
		{"empty parent", "", ".thumbnails"},
		{"root", "/", "/.thumbnails"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThumbnailDir(tt.parent); got != tt.want {
				t.Errorf("ThumbnailDir(%q) = %q, want %q", tt.parent, got, tt.want)
			}
		})
	}
}

func TestThumbnailNameFor(t *testing.T) {
	tests := []struct {
		name    string
		srcPath string
		want    string
	}{
		{"video mp4", "/media/set1/clip.mp4", "clip.mp4.jpg"},
		{"extension case is preserved", "/media/set1/photo.JPG", "photo.JPG.jpg"},
		{"jpeg source still gets its own name", "/media/set1/photo.jpg", "photo.jpg.jpg"},
		{"no extension", "/media/set1/README", "README.jpg"},
		{"multi-dot filename", "/media/set1/my.movie.final.mkv", "my.movie.final.mkv.jpg"},
		{"dotfile keeps its name", "/media/set1/.bashrc", ".bashrc.jpg"},
		{"just basename", "clip.mp4", "clip.mp4.jpg"},
		{"path with trailing slash", "/media/set1/clip.mp4/", "clip.mp4.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThumbnailNameFor(tt.srcPath); got != tt.want {
				t.Errorf("ThumbnailNameFor(%q) = %q, want %q", tt.srcPath, got, tt.want)
			}
		})
	}
}

// TestThumbnailNameFor_UniquePerSource is the regression test for the stem
// collision: sources that differ only in their extension (or that look like
// another source's thumbnail name) must each get their own thumbnail name.
func TestThumbnailNameFor_UniquePerSource(t *testing.T) {
	sources := []string{
		"holiday.mp4", "holiday.png", "holiday.MP4", "holiday",
		"sample.mp4", "sample.mkv",
		"holiday.mp4.jpg", "holiday.mp4.png", // look like thumbnail names themselves
		".mp4", ".png", // dotfiles used to collapse to ".jpg"
	}
	seen := make(map[string]string, len(sources))
	for _, src := range sources {
		name := ThumbnailNameFor("/media/set1/" + src)
		if other, dup := seen[name]; dup {
			t.Errorf("%q and %q share thumbnail name %q", other, src, name)
		}
		seen[name] = src
	}
}

func TestThumbnailPathFor(t *testing.T) {
	tests := []struct {
		name    string
		srcPath string
		parent  string
		want    string
	}{
		{"video", "/media/set1/clip.mp4", "/media/set1", "/media/set1/.thumbnails/clip.mp4.jpg"},
		{"image trailing slash parent", "/media/set1/photo.jpg", "/media/set1/", "/media/set1/.thumbnails/photo.jpg.jpg"},
		{"no extension", "/media/set1/RAW", "/media/set1", "/media/set1/.thumbnails/RAW.jpg"},
		{"nested source mirrors its folder", "/media/set1/a/clip.mp4", "/media/set1", "/media/set1/.thumbnails/a/clip.mp4.jpg"},
		{"deeply nested source", "/media/set1/a/b/clip.mp4", "/media/set1", "/media/set1/.thumbnails/a/b/clip.mp4.jpg"},
		{"relative nested", "set1/a/clip.mp4", "set1", "set1/.thumbnails/a/clip.mp4.jpg"},
		{"folder name starting with dots is not an escape", "/media/set1/..a/clip.mp4", "/media/set1", "/media/set1/.thumbnails/..a/clip.mp4.jpg"},
		// Negative cases: a source that is not below parent must not escape
		// parent/.thumbnails via "..", it falls back to the bare name.
		{"src elsewhere", "/uploads/raw/photo.png", "/media/set1", "/media/set1/.thumbnails/photo.png.jpg"},
		{"src in sibling of parent", "/media/set2/clip.mp4", "/media/set1", "/media/set1/.thumbnails/clip.mp4.jpg"},
		{"src one level above parent", "/media/clip.mp4", "/media/set1", "/media/set1/.thumbnails/clip.mp4.jpg"},
		{"relative src, unrelated relative parent", "clip.mp4", "set1", "set1/.thumbnails/clip.mp4.jpg"},
		{"absolute src, relative parent", "/media/set1/clip.mp4", "set1", "set1/.thumbnails/clip.mp4.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThumbnailPathFor(tt.srcPath, tt.parent); got != tt.want {
				t.Errorf("ThumbnailPathFor(%q, %q) = %q, want %q", tt.srcPath, tt.parent, got, tt.want)
			}
		})
	}
}

// TestThumbnailPathFor_NoCollisionAcrossFolders covers the scanner layout,
// where one set directory owns the thumbnails of all its subfolders.
func TestThumbnailPathFor_NoCollisionAcrossFolders(t *testing.T) {
	sources := []string{
		"/media/set1/clip.mp4", "/media/set1/clip.png",
		"/media/set1/a/clip.mp4", "/media/set1/b/clip.mp4", "/media/set1/a/b/clip.mp4",
	}
	seen := make(map[string]string, len(sources))
	for _, src := range sources {
		p := ThumbnailPathFor(src, "/media/set1")
		if other, dup := seen[p]; dup {
			t.Errorf("%q and %q share thumbnail path %q", other, src, p)
		}
		seen[p] = src
	}
}

func TestParentOf(t *testing.T) {
	tests := []struct {
		name      string
		thumbPath string
		want      string
		wantOK    bool
	}{
		{"flat thumbnail", "/media/set1/.thumbnails/clip.mp4.jpg", "/media/set1", true},
		{"old stem-named thumbnail", "/media/set1/.thumbnails/clip.jpg", "/media/set1", true},
		{"mirrored subfolder", "/media/set1/.thumbnails/a/b/clip.mp4.jpg", "/media/set1", true},
		{"relative", "set1/.thumbnails/clip.jpg", "set1", true},
		{"innermost tree wins", "/m/.thumbnails/x/.thumbnails/c.jpg", "/m/.thumbnails/x", true},
		{"image used as its own thumbnail", "/media/set1/photo.jpg", "", false},
		{"audio cover", "/media/set1/album/cover.jpg", "", false},
		{"file merely named like the directory", "/media/set1/.thumbnails", "", false},
		{"similar directory name", "/media/set1/.thumbnails2/clip.jpg", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParentOf(tt.thumbPath)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("ParentOf(%q) = %q, %v, want %q, %v", tt.thumbPath, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestParentOf_RoundTrip checks that a path built by ThumbnailPathFor is
// recognised as already canonical, which is what stops the scanner from
// migrating the same thumbnail again on every rescan.
func TestParentOf_RoundTrip(t *testing.T) {
	for _, src := range []string{"/media/set1/clip.mp4", "/media/set1/a/b/clip.mp4"} {
		p := ThumbnailPathFor(src, "/media/set1")
		parent, ok := ParentOf(p)
		if !ok || ThumbnailPathFor(src, parent) != p {
			t.Errorf("round trip of %q via %q failed: parent=%q ok=%v", src, p, parent, ok)
		}
	}
}

// TestDirNameConstant guards the on-disk convention; many tests in
// internal/service compare against the literal ".thumbnails" name.
func TestDirNameConstant(t *testing.T) {
	if DirName != ".thumbnails" {
		t.Errorf("DirName = %q, want %q (changing this breaks on-disk layout)", DirName, ".thumbnails")
	}
}
