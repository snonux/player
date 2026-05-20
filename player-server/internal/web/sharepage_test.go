package web

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSharePageRenderer_Render(t *testing.T) {
	t.Run("injects marshaled metadata", func(t *testing.T) {
		fs := fstest.MapFS{
			"share.html": {Data: []byte(`<script><!--SHARE_MEDIA--></script>`)},
		}
		r := NewSharePageRenderer(http.FS(fs))
		page, err := r.Render(map[string]string{"stream_url": "/s/abc/stream"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(page.HTML, `"stream_url":"/s/abc/stream"`) {
			t.Fatalf("expected injected JSON, got %q", page.HTML)
		}
		if page.Name != "share.html" {
			t.Fatalf("expected name share.html, got %q", page.Name)
		}
	})

	t.Run("returns marshal error", func(t *testing.T) {
		fs := fstest.MapFS{
			"share.html": {Data: []byte(`<script><!--SHARE_MEDIA--></script>`)},
		}
		r := NewSharePageRenderer(http.FS(fs))
		_, err := r.Render(map[string]any{"bad": make(chan int)})
		if err == nil {
			t.Fatal("expected marshal error")
		}
		if !strings.Contains(err.Error(), "marshal share metadata") {
			t.Fatalf("expected wrapped marshal error, got %v", err)
		}
	})

	t.Run("nil renderer", func(t *testing.T) {
		var r *SharePageRenderer
		if _, err := r.Render(nil); err == nil {
			t.Fatal("expected error from nil renderer")
		}
	})

	t.Run("missing template", func(t *testing.T) {
		r := NewSharePageRenderer(http.FS(fstest.MapFS{}))
		_, err := r.Render(nil)
		if err == nil {
			t.Fatal("expected open error")
		}
	})

	t.Run("stat error propagates", func(t *testing.T) {
		r := NewSharePageRenderer(statErrorFS{})
		_, err := r.Render(nil)
		if err == nil {
			t.Fatal("expected stat error")
		}
	})
}

func TestInjectShareMedia(t *testing.T) {
	html, err := injectShareMedia(`a <!--X--> b`, "<!--X-->", "v")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if html != `a "v" b` {
		t.Fatalf("unexpected result: %q", html)
	}
}

func TestSanitizeFileName(t *testing.T) {
	t.Run("short clean name unchanged", func(t *testing.T) {
		got := SanitizeFileName("movie.mp4")
		if got != "movie.mp4" {
			t.Fatalf("expected %q, got %q", "movie.mp4", got)
		}
	})

	t.Run("html special chars are escaped", func(t *testing.T) {
		got := SanitizeFileName(`<script>alert(1)</script>.mp4`)
		if strings.Contains(got, "<") || strings.Contains(got, ">") {
			t.Fatalf("expected HTML-escaped output, got %q", got)
		}
		if !strings.Contains(got, "&lt;") {
			t.Fatalf("expected &lt; in output, got %q", got)
		}
	})

	t.Run("long filename is truncated to MaxFileNameLength runes", func(t *testing.T) {
		// Build a filename longer than MaxFileNameLength characters.
		long := strings.Repeat("a", MaxFileNameLength+100)
		got := SanitizeFileName(long)
		if len([]rune(got)) > MaxFileNameLength {
			t.Fatalf("expected at most %d runes, got %d", MaxFileNameLength, len([]rune(got)))
		}
	})

	t.Run("multibyte runes counted correctly", func(t *testing.T) {
		// Each '日' is 3 UTF-8 bytes but 1 rune; we want exactly MaxFileNameLength runes.
		long := strings.Repeat("日", MaxFileNameLength+10)
		got := SanitizeFileName(long)
		if len([]rune(got)) > MaxFileNameLength {
			t.Fatalf("expected at most %d runes, got %d", MaxFileNameLength, len([]rune(got)))
		}
	})

	t.Run("ampersand is escaped", func(t *testing.T) {
		got := SanitizeFileName("a&b.mp4")
		if got != "a&amp;b.mp4" {
			t.Fatalf("expected %q, got %q", "a&amp;b.mp4", got)
		}
	})
}

// statErrorFS returns a file whose Stat() fails — used to cover the
// rare error path in Render where the template is openable but cannot
// be stat'd.
type statErrorFS struct{}

func (statErrorFS) Open(string) (http.File, error) { return statErrorFile{}, nil }

type statErrorFile struct{}

func (statErrorFile) Close() error                       { return nil }
func (statErrorFile) Read([]byte) (int, error)           { return 0, io.EOF }
func (statErrorFile) Seek(int64, int) (int64, error)     { return 0, nil }
func (statErrorFile) Readdir(int) ([]os.FileInfo, error) { return nil, nil }
func (statErrorFile) Stat() (os.FileInfo, error)         { return nil, errors.New("stat failed") }
