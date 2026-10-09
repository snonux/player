package thumb

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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

// TestThumbnailNameFor_LongNames: a source name may be as long as the
// filesystem allows (255 bytes), so the thumbnail name must be shortened
// instead of exceeding the limit, and stay unique and deterministic.
func TestThumbnailNameFor_LongNames(t *testing.T) {
	long := func(n int, fill, ext string) string {
		return strings.Repeat(fill, (n-len(ext))/len(fill)) + ext
	}
	t.Run("longest name that still fits is kept verbatim", func(t *testing.T) {
		src := long(251, "a", ".mp4")
		if got := ThumbnailNameFor(src); got != src+".jpg" || len(got) != 255 {
			t.Errorf("got %d bytes, want the name plus .jpg (255 bytes)", len(got))
		}
	})
	for _, n := range []int{252, 253, 254, 255} {
		src := long(n, "a", ".mp4")
		got := ThumbnailNameFor("/media/set1/" + src)
		if len(got) > 255 || !strings.HasSuffix(got, ".jpg") {
			t.Errorf("source of %d bytes: thumbnail name has %d bytes, suffix ok=%v", n, len(got), strings.HasSuffix(got, ".jpg"))
		}
		if got != ThumbnailNameFor(src) {
			t.Errorf("source of %d bytes: name is not deterministic", n)
		}
	}
	t.Run("long names differing only at the end stay distinct", func(t *testing.T) {
		a, b := long(255, "a", ".mp4"), long(255, "a", ".mkv")
		if ThumbnailNameFor(a) == ThumbnailNameFor(b) {
			t.Error("two long sources share a thumbnail name")
		}
	})
	// 3-byte runes: the byte budget (234) is a multiple of three, so an odd
	// number of leading ASCII bytes puts the cut inside a rune and forces
	// the name to be shortened further, to the previous rune boundary.
	for _, lead := range []string{"", "x", "xy"} {
		src := lead + strings.Repeat("\u20ac", 84) + ".mp4"
		got := ThumbnailNameFor(src)
		if len(got) > 255 || !utf8.ValidString(got) {
			t.Errorf("lead %q: name has %d bytes, valid UTF-8 = %v", lead, len(got), utf8.ValidString(got))
		}
	}
	t.Run("cut inside a rune backs off to its start", func(t *testing.T) {
		got := ThumbnailNameFor("x" + strings.Repeat("\u20ac", 84) + ".mp4")
		// 1 + 77*3 = 232 bytes fit the 234 byte budget; one more rune would not.
		if want := "x" + strings.Repeat("\u20ac", 77) + "~"; !strings.HasPrefix(got, want) {
			t.Errorf("name starts with %q, want prefix of 77 whole runes", got[:10])
		}
	})
}

func TestThumbnailPathFor(t *testing.T) {
	tests := []struct {
		name    string
		srcPath string
		want    string
	}{
		{"video", "/media/set1/clip.mp4", "/media/set1/.thumbnails/clip.mp4.jpg"},
		{"jpeg source", "/media/set1/photo.jpg", "/media/set1/.thumbnails/photo.jpg.jpg"},
		{"no extension", "/media/set1/RAW", "/media/set1/.thumbnails/RAW.jpg"},
		{"nested source stays in its own folder", "/media/set1/a/clip.mp4", "/media/set1/a/.thumbnails/clip.mp4.jpg"},
		{"deeply nested source", "/media/set1/a/b/clip.mp4", "/media/set1/a/b/.thumbnails/clip.mp4.jpg"},
		{"relative", "set1/clip.mp4", "set1/.thumbnails/clip.mp4.jpg"},
		{"bare name", "clip.mp4", ".thumbnails/clip.mp4.jpg"},
		{"unclean path is cleaned", "/media/set1/a/../clip.mp4", "/media/set1/.thumbnails/clip.mp4.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThumbnailPathFor(tt.srcPath); got != tt.want {
				t.Errorf("ThumbnailPathFor(%q) = %q, want %q", tt.srcPath, got, tt.want)
			}
		})
	}
}

// TestThumbnailPathFor_UniquePerSource: neither a shared stem nor a shared
// name in two folders of one set may lead to a shared thumbnail path.
func TestThumbnailPathFor_UniquePerSource(t *testing.T) {
	sources := []string{
		"/media/set1/clip.mp4", "/media/set1/clip.png", "/media/set1/clip.mkv",
		"/media/set1/a/clip.mp4", "/media/set1/b/clip.mp4", "/media/set1/a/b/clip.mp4",
		"/media/set2/clip.mp4",
	}
	seen := make(map[string]string, len(sources))
	for _, src := range sources {
		p := ThumbnailPathFor(src)
		if other, dup := seen[p]; dup {
			t.Errorf("%q and %q share thumbnail path %q", other, src, p)
		}
		seen[p] = src
	}
}

func TestIsGenerated(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"current name", "/media/set1/.thumbnails/clip.mp4.jpg", true},
		{"old stem name", "/media/set1/.thumbnails/clip.jpg", true},
		{"relative", ".thumbnails/clip.jpg", true},
		{"own output", ThumbnailPathFor("/media/set1/a/clip.mp4"), true},
		{"image used as its own thumbnail", "/media/set1/photo.jpg", false},
		{"audio cover", "/media/set1/album/cover.jpg", false},
		{"the directory itself", "/media/set1/.thumbnails", false},
		{"subfolder of the directory", "/media/set1/.thumbnails/a/clip.jpg", false},
		{"similar directory name", "/media/set1/.thumbnails2/clip.jpg", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGenerated(tt.path); got != tt.want {
				t.Errorf("IsGenerated(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestDirNameConstant guards the on-disk convention; many tests in
// internal/service compare against the literal ".thumbnails" name.
func TestDirNameConstant(t *testing.T) {
	if DirName != ".thumbnails" {
		t.Errorf("DirName = %q, want %q (changing this breaks on-disk layout)", DirName, ".thumbnails")
	}
}
