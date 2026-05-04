package mediatype

import (
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

func TestTypeForExt(t *testing.T) {
	cases := []struct {
		name string
		want model.MediaType
	}{
		// video
		{"a.mp4", model.MediaTypeVideo},
		{"a.mkv", model.MediaTypeVideo},
		{"a.avi", model.MediaTypeVideo},
		{"a.mov", model.MediaTypeVideo},
		{"a.wmv", model.MediaTypeVideo},
		{"a.flv", model.MediaTypeVideo},
		{"a.webm", model.MediaTypeVideo},
		// audio
		{"a.mp3", model.MediaTypeAudio},
		{"a.wav", model.MediaTypeAudio},
		{"a.flac", model.MediaTypeAudio},
		{"a.aac", model.MediaTypeAudio},
		{"a.ogg", model.MediaTypeAudio},
		{"a.m4a", model.MediaTypeAudio},
		{"a.wma", model.MediaTypeAudio},
		{"a.m4b", model.MediaTypeAudio},
		{"a.opus", model.MediaTypeAudio},
		// image
		{"a.jpg", model.MediaTypeImage},
		{"a.jpeg", model.MediaTypeImage},
		{"a.png", model.MediaTypeImage},
		{"a.gif", model.MediaTypeImage},
		{"a.webp", model.MediaTypeImage},
		{"a.bmp", model.MediaTypeImage},
		{"a.avif", model.MediaTypeImage},
		{"a.svg", model.MediaTypeImage},
		// unknown → video (consistent default)
		{"unknown.xyz", model.MediaTypeVideo},
		{"no_ext", model.MediaTypeVideo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TypeForExt(c.name); got != c.want {
				t.Errorf("TypeForExt(%q) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestTypeForExt_CaseInsensitive(t *testing.T) {
	if got := TypeForExt("movie.MP4"); got != model.MediaTypeVideo {
		t.Errorf("TypeForExt(\"movie.MP4\") = %v, want video", got)
	}
	if got := TypeForExt("song.FLAC"); got != model.MediaTypeAudio {
		t.Errorf("TypeForExt(\"song.FLAC\") = %v, want audio", got)
	}
	if got := TypeForExt("photo.PNG"); got != model.MediaTypeImage {
		t.Errorf("TypeForExt(\"photo.PNG\") = %v, want image", got)
	}
}

func TestIsSupportedExt(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"movie.mp4", true},
		{"song.MP3", true},
		{"archive.zip", false},
		{"photo.jpg", true},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSupportedExt(c.name); got != c.want {
				t.Errorf("IsSupportedExt(%q) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestIsImageExt(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"image.png", true},
		{"anim.gif", true},
		{"img.webp", true},
		{"legacy.bmp", true},
		{"modern.avif", true},
		{"vector.svg", true},
		{"video.mp4", false},
		{"audio.mp3", false},
		{"archive.tar.gz", false},
		{"no_ext", false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := IsImageExt(c.path); got != c.want {
				t.Errorf("IsImageExt(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestIsCoverImageExt(t *testing.T) {
	if !IsCoverImageExt("cover.jpg") {
		t.Error("expected true for jpg")
	}
	if !IsCoverImageExt("cover.png") {
		t.Error("expected true for png")
	}
	if IsCoverImageExt("cover.webp") {
		t.Error("expected false for webp (not a cover-art ext)")
	}
}

func TestMIMETypeForExt(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"movie.mp4", "video/mp4"},
		{"song.mkv", "video/x-matroska"},
		{"song.avi", "video/vnd.avi"},
		{"clip.mov", "video/quicktime"},
		{"clip.wmv", "video/x-ms-wmv"},
		{"clip.flv", "video/x-flv"},
		{"clip.webm", "audio/webm"},
		{"song.mp3", "audio/mpeg"},
		{"song.flac", "audio/flac"},
		{"song.wav", "audio/wav"},
		{"song.aac", "audio/aac"},
		{"song.m4a", "audio/mp4"},
		{"song.ogg", "audio/ogg"},
		{"song.opus", "audio/ogg"},
		{"song.m4b", "audio/x-m4b"},
		{"song.wma", "audio/x-ms-wma"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.png", "image/png"},
		{"photo.gif", "image/gif"},
		{"photo.webp", "image/webp"},
		{"photo.bmp", "image/bmp"},
		{"photo.avif", "image/avif"},
		{"photo.svg", "image/svg+xml"},
		{"unknown.bin", "application/octet-stream"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MIMETypeForExt(c.name); got != c.want {
				t.Errorf("MIMETypeForExt(%q) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}
