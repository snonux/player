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
		{"video mp4", "/media/set1/clip.mp4", "clip.jpg"},
		{"image jpg", "/media/set1/photo.JPG", "photo.jpg"},
		{"audio with cover", "/media/set1/song.mp3", "song.jpg"},
		{"no extension", "/media/set1/README", "README.jpg"},
		{"multi-dot filename", "/media/set1/my.movie.final.mkv", "my.movie.final.jpg"},
		{"dotfile", "/media/set1/.bashrc", ".jpg"},
		{"just basename", "clip.mp4", "clip.jpg"},
		{"path with trailing slash", "/media/set1/clip.mp4/", "clip.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThumbnailNameFor(tt.srcPath); got != tt.want {
				t.Errorf("ThumbnailNameFor(%q) = %q, want %q", tt.srcPath, got, tt.want)
			}
		})
	}
}

func TestThumbnailPathFor(t *testing.T) {
	tests := []struct {
		name    string
		srcPath string
		parent  string
		want    string
	}{
		{"video", "/media/set1/clip.mp4", "/media/set1", "/media/set1/.thumbnails/clip.jpg"},
		{"image trailing slash parent", "/media/set1/photo.jpg", "/media/set1/", "/media/set1/.thumbnails/photo.jpg"},
		{"src elsewhere", "/uploads/raw/photo.png", "/media/set1", "/media/set1/.thumbnails/photo.jpg"},
		{"relative", "clip.mp4", "set1", "set1/.thumbnails/clip.jpg"},
		{"no extension", "/media/set1/RAW", "/media/set1", "/media/set1/.thumbnails/RAW.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThumbnailPathFor(tt.srcPath, tt.parent); got != tt.want {
				t.Errorf("ThumbnailPathFor(%q, %q) = %q, want %q", tt.srcPath, tt.parent, got, tt.want)
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
