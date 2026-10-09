package ffsafe

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInputArgs(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string // expected "file:" argument
	}{
		{"absolute", "/media/set/a.avi", "file:/media/set/a.avi"},
		{"cleaned", "/media/set/../other/a.avi", "file:/media/other/a.avi"},
		// Names that look like options, protocols or sequence patterns
		// are passed literally behind the file: prefix.
		{"hostile name", "/media/-weird:name %03d.avi", "file:/media/-weird:name %03d.avi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InputArgs(tt.path)
			if err != nil {
				t.Fatalf("InputArgs: %v", err)
			}
			want := []string{"-protocol_whitelist", "file", "-format_whitelist", FormatWhitelist, "-i", tt.want}
			if !slices.Equal(got, want) {
				t.Errorf("InputArgs = %q, want %q", got, want)
			}
		})
	}
}

func TestInputArgs_RelativePathBecomesAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := InputArgs("media/-a.avi")
	if err != nil {
		t.Fatalf("InputArgs: %v", err)
	}
	if want := "file:" + filepath.Join(wd, "media/-a.avi"); got[len(got)-1] != want {
		t.Errorf("input = %q, want %q", got[len(got)-1], want)
	}
}

// imageHeads are minimal file starts of every supported image format, by
// the demuxer they must select.
func imageHeads() map[string][]byte {
	return map[string][]byte{
		"jpeg_pipe": []byte("\xff\xd8\xff\xe0\x00\x10JFIF"),
		"png_pipe":  []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"),
		"gif":       []byte("GIF89a\x01\x00\x01\x00"),
		"webp_pipe": []byte("RIFF\x24\x00\x00\x00WEBPVP8 "),
		"bmp_pipe":  []byte("BM\x36\x00\x00\x00\x00\x00"),
		"mov":       []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00"),
		"svg_pipe":  []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!-- c -->\n<svg xmlns=\"http://www.w3.org/2000/svg\"/>"),
	}
}

func writeFile(t *testing.T, path string, content []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The demuxer is forced from the content, whatever the name says, and the
// whitelist holds exactly that demuxer.
func TestImageInputArgs(t *testing.T) {
	heads := imageHeads()
	if len(heads) != len(ImageDemuxers()) {
		t.Fatalf("test covers %d formats, ImageDemuxers has %d", len(heads), len(ImageDemuxers()))
	}
	for demuxer, head := range heads {
		t.Run(demuxer, func(t *testing.T) {
			// A hostile name, with the wrong extension for most formats.
			path := writeFile(t, filepath.Join(t.TempDir(), "-we:ird %03d.jpg"), head)
			got, err := ImageInputArgs(path)
			if err != nil {
				t.Fatalf("ImageInputArgs: %v", err)
			}
			want := []string{"-f", demuxer, "-protocol_whitelist", "file", "-format_whitelist", demuxer, "-i", "file:" + path}
			if !slices.Equal(got, want) {
				t.Errorf("ImageInputArgs = %q, want %q", got, want)
			}
		})
	}
}

// Content that is no image is refused before ffmpeg could look at it.
func TestImageInputArgs_RefusesOtherContent(t *testing.T) {
	contents := map[string]string{
		"ffconcat":   "ffconcat version 1.0\nfile 'private/s.mp4'\n",
		"hls":        "#EXTM3U\n#EXTINF:3.0,\nprivate/s.mp4\n#EXT-X-ENDLIST\n",
		"empty":      "",
		"text":       "hello",
		"html":       "<html><body>no vector graphic</body></html>",
		"avi":        "RIFF\x00\x00\x00\x00AVI LIST",
		"short riff": "RIFF",
	}
	for name, content := range contents {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(t.TempDir(), "evil.jpg"), []byte(content))
			if got, err := ImageInputArgs(path); !errors.Is(err, ErrNotAnImage) {
				t.Errorf("ImageInputArgs = %q, %v; want ErrNotAnImage", got, err)
			}
		})
	}
}

func TestImageInputArgs_MissingFile(t *testing.T) {
	_, err := ImageInputArgs(filepath.Join(t.TempDir(), "gone.png"))
	if err == nil || errors.Is(err, ErrNotAnImage) {
		t.Errorf("err = %v, want an open error", err)
	}
}

// SourceArgs picks the image path for every image extension of mediatype
// and the audio/video whitelist for everything else.
func TestSourceArgs(t *testing.T) {
	dir := t.TempDir()
	png := imageHeads()["png_pipe"]
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".avif", ".svg", ".PNG"} {
		path := writeFile(t, filepath.Join(dir, "img"+ext), png)
		got, err := SourceArgs(path)
		if err != nil {
			t.Fatalf("SourceArgs(%s): %v", ext, err)
		}
		if !slices.Equal(got[:2], []string{"-f", "png_pipe"}) {
			t.Errorf("SourceArgs(%s) = %q, want the forced image demuxer", ext, got)
		}
	}
	for _, ext := range []string{".mp4", ".avi", ".mp3", ".opus", ".unknown", ""} {
		// The file need not exist: audio/video is not sniffed.
		path := filepath.Join(dir, "media"+ext)
		got, err := SourceArgs(path)
		if err != nil {
			t.Fatalf("SourceArgs(%s): %v", ext, err)
		}
		want := []string{"-protocol_whitelist", "file", "-format_whitelist", FormatWhitelist, "-i", "file:" + path}
		if !slices.Equal(got, want) {
			t.Errorf("SourceArgs(%s) = %q, want %q", ext, got, want)
		}
	}
}

func TestOutputArg(t *testing.T) {
	got, err := OutputArg("/media/set/.thumbnails/-a:b%03d.jpg")
	if err != nil || got != "file:/media/set/.thumbnails/-a:b%03d.jpg" {
		t.Errorf("OutputArg = %q, %v", got, err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := OutputArg("-out.jpg"); got != "file:"+filepath.Join(wd, "-out.jpg") {
		t.Errorf("OutputArg(relative) = %q", got)
	}
}

// Neither list may ever contain a demuxer that reads other files.
func TestWhitelists_HaveNoReferencingDemuxers(t *testing.T) {
	names := append(strings.Split(FormatWhitelist, ","), ImageDemuxers()...)
	for _, name := range names {
		switch name {
		case "concat", "ffconcat", "concatf", "hls", "applehttp", "dash", "tee", "image2", "image2pipe",
			"lavfi", "tty", "sdp", "rtsp", "imf", "webm_dash_manifest":
			t.Errorf("whitelist contains %q, which can reference other files", name)
		}
	}
}

// TestEveryFFmpegCallSiteUsesFFsafe reads the server's sources: a file that
// starts processes must build its arguments with this package. It catches a
// new ffmpeg/ffprobe call site written without the hardening; the argument
// list tests of each package (TestProbeArgs, TestRemuxArgs, TestThumbArgs,
// TestFFmpegArgs) catch an existing one dropping it.
func TestEveryFFmpegCallSiteUsesFFsafe(t *testing.T) {
	sites := 0
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// ffsafetest creates fixtures from lavfi sources, not user media.
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(path, "ffsafetest") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), "exec.Command") {
			return nil
		}
		sites++
		if !strings.Contains(string(src), "ffsafe.") {
			t.Errorf("%s starts a process without using ffsafe for its arguments", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// probe.go, remux.go, thumb.go, transcode/ffmpeg.go, transcode/ffprobe.go
	if sites < 5 {
		t.Errorf("found %d files starting processes, want at least 5: is the walk root wrong?", sites)
	}
}
