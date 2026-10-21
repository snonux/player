package podcast

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadCoverImageReplacesCover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("new cover"))
	}))
	defer srv.Close()
	dir := writeOldCover(t)

	if err := DownloadCoverImage(srv.Client(), srv.URL, dir); err != nil {
		t.Fatalf("DownloadCoverImage: %v", err)
	}
	assertCover(t, dir, "new cover")
}

// A body that ends early (here: shorter than its Content-Length) must keep
// the previous cover intact and leave no temporary file behind.
func TestDownloadCoverImageKeepsOldCoverOnFailedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
	}))
	defer srv.Close()
	dir := writeOldCover(t)

	if err := DownloadCoverImage(srv.Client(), srv.URL, dir); err == nil {
		t.Fatal("DownloadCoverImage succeeded on a truncated body")
	}
	assertCover(t, dir, "old cover")
}

func TestDownloadCoverImageKeepsOldCoverOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	dir := writeOldCover(t)

	if err := DownloadCoverImage(srv.Client(), srv.URL, dir); err == nil {
		t.Fatal("DownloadCoverImage succeeded on HTTP 404")
	}
	assertCover(t, dir, "old cover")
}

func TestDownloadCoverImageWithoutURLIsNoop(t *testing.T) {
	dir := writeOldCover(t)
	if err := DownloadCoverImage(http.DefaultClient, "", dir); err != nil {
		t.Fatalf("DownloadCoverImage: %v", err)
	}
	assertCover(t, dir, "old cover")
}

func writeOldCover(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("old cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// assertCover checks cover.jpg's content and that it is the only file left.
func assertCover(t *testing.T, dir, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("cover.jpg = %q, want %q", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("folder has %d entries, want only cover.jpg", len(entries))
	}
}
