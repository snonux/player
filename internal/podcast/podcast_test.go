package podcast

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
)

// ---------------------------------------------------------------------------
// Helpers for building gofeed structures (avoids verbose literals in tests)
// ---------------------------------------------------------------------------

func makeFeed(title, description, imageURL string) *gofeed.Feed {
	return &gofeed.Feed{
		Title:       title,
		Description: description,
		ITunesExt:   &ext.ITunesFeedExtension{Image: imageURL},
	}
}

func makeItemWithEnclosure(guid, title, desc, encURL, encLength string, published *time.Time) *gofeed.Item {
	it := &gofeed.Item{
		GUID:        guid,
		Title:       title,
		Description: desc,
	}
	if published != nil {
		it.PublishedParsed = published
	}
	if encURL != "" {
		it.Enclosures = []*gofeed.Enclosure{{URL: encURL, Length: encLength}}
	}
	return it
}

func makeItemWithDuration(guid, title, duration string) *gofeed.Item {
	return &gofeed.Item{
		GUID:        guid,
		Title:       title,
		ITunesExt:   &ext.ITunesItemExtension{Duration: duration},
		Enclosures:  []*gofeed.Enclosure{{URL: "http://example.com/" + guid + ".mp3", Length: "0"}},
	}
}

// ---------------------------------------------------------------------------
// TestParseFeedReader — end-to-end positive and negative cases
// ---------------------------------------------------------------------------

func TestParseFeedReader_EmptyFeed(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" version="2.0">
  <channel>
    <title>Empty</title>
  </channel>
</rss>`

	pf, err := ParseFeedReader(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Title != "Empty" {
		t.Errorf("Title = %q, want Empty", pf.Title)
	}
	if len(pf.Episodes) != 0 {
		t.Fatalf("expected 0 episodes, got %d", len(pf.Episodes))
	}
}

func TestParseFeedReader_WithEpisodes(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" version="2.0">
  <channel>
    <title>My Podcast</title>
    <description>A test feed</description>
    <itunes:image href="https://example.com/cover.jpg"/>
    <item>
      <title>Episode One</title>
      <guid>ep1</guid>
      <description>First episode</description>
      <pubDate>Mon, 02 Jan 2026 15:04:05 GMT</pubDate>
      <enclosure url="https://example.com/ep1.mp3" length="12345" type="audio/mpeg"/>
      <itunes:duration>00:05:30</itunes:duration>
    </item>
    <item>
      <title>Episode Two</title>
      <guid>ep2</guid>
      <description>Second episode</description>
      <pubDate>Tue, 03 Jan 2026 10:00:00 GMT</pubDate>
      <enclosure url="https://example.com/ep2.mp3" length="67890" type="audio/mpeg"/>
    </item>
  </channel>
</rss>`

	pf, err := ParseFeedReader(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Title != "My Podcast" {
		t.Errorf("Title = %q, want My Podcast", pf.Title)
	}
	if pf.Description != "A test feed" {
		t.Errorf("Description = %q, want A test feed", pf.Description)
	}
	if pf.ImageURL != "https://example.com/cover.jpg" {
		t.Errorf("ImageURL = %q, want https://example.com/cover.jpg", pf.ImageURL)
	}
	if len(pf.Episodes) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(pf.Episodes))
	}

	ep1 := pf.Episodes[0]
	if ep1.GUID != "ep1" {
		t.Errorf("ep1.GUID = %q, want ep1", ep1.GUID)
	}
	if ep1.Title != "Episode One" {
		t.Errorf("ep1.Title = %q, want Episode One", ep1.Title)
	}
	if ep1.Description != "First episode" {
		t.Errorf("ep1.Description = %q, want First episode", ep1.Description)
	}
	if ep1.EpisodeURL != "https://example.com/ep1.mp3" {
		t.Errorf("ep1.EpisodeURL = %q", ep1.EpisodeURL)
	}
	if ep1.FileSize == nil || *ep1.FileSize != 12345 {
		t.Errorf("ep1.FileSize = %v, want 12345", ep1.FileSize)
	}
	if ep1.DurationSeconds == nil || *ep1.DurationSeconds != 5*60+30 {
		t.Errorf("ep1.DurationSeconds = %v, want 330", ep1.DurationSeconds)
	}
	if ep1.PublishedAt == nil {
		t.Fatal("ep1.PublishedAt is nil")
	}
	if ep1.PublishedAt.Year() != 2026 {
		t.Errorf("ep1.PublishedAt.Year = %d, want 2026", ep1.PublishedAt.Year())
	}

	ep2 := pf.Episodes[1]
	if ep2.GUID != "ep2" {
		t.Errorf("ep2.GUID = %q, want ep2", ep2.GUID)
	}
	if ep2.DurationSeconds != nil {
		t.Errorf("ep2.DurationSeconds expected nil, got %v", *ep2.DurationSeconds)
	}
	if ep2.FileSize == nil || *ep2.FileSize != 67890 {
		t.Errorf("ep2.FileSize = %v, want 67890", ep2.FileSize)
	}
}

func TestParseFeedReader_InvalidXML(t *testing.T) {
	_, err := ParseFeedReader(strings.NewReader("not xml"))
	if err == nil {
		t.Fatal("expected error for invalid XML")
	}
}

// ---------------------------------------------------------------------------
// TestParseFeed — real HTTP server integration
// ---------------------------------------------------------------------------

func TestParseFeed_Success(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Server Feed</title>
    <item>
      <title>Item 1</title>
      <guid>g1</guid>
      <enclosure url="http://example.com/1.mp3" length="1024" type="audio/mpeg"/>
    </item>
  </channel>
</rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(xml))
	}))
	defer srv.Close()

	pf, err := ParseFeed(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Title != "Server Feed" {
		t.Errorf("Title = %q, want Server Feed", pf.Title)
	}
	if len(pf.Episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(pf.Episodes))
	}
}

func TestParseFeed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ParseFeed(srv.URL)
	if err == nil {
		t.Fatal("expected error when server returns 500")
	}
}

// ---------------------------------------------------------------------------
// TestParsedFeedFromGoFeed — unit tests for the conversion logic
// ---------------------------------------------------------------------------

func TestParsedFeedFromGoFeed_Empty(t *testing.T) {
	f := makeFeed("T", "D", "")
	pf := parsedFeedFromGoFeed(f)
	if pf.Title != "T" || pf.Description != "D" {
		t.Error("basic field mismatch")
	}
	if len(pf.Episodes) != 0 {
		t.Error("expected no episodes")
	}
}

func TestParsedFeedFromGoFeed_MultipleEpisodes(t *testing.T) {
	pub := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	f := makeFeed("Title", "Desc", "https://img")
	f.Image = &gofeed.Image{URL: "https://img-rss"} // overridden by iTunes
	f.Items = []*gofeed.Item{
		makeItemWithEnclosure("a", "A", "descA", "http://a.mp3", "1000", &pub),
		makeItemWithDuration("b", "B", "1:02:03"),
	}

	pf := parsedFeedFromGoFeed(f)
	if pf.ImageURL != "https://img" { // iTunes takes priority
		t.Errorf("ImageURL = %q, want iTunes image", pf.ImageURL)
	}
	if len(pf.Episodes) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(pf.Episodes))
	}

	ep0 := pf.Episodes[0]
	if ep0.GUID != "a" || ep0.Title != "A" || ep0.Description != "descA" {
		t.Error("episode 0 field mismatch")
	}
	if ep0.PublishedAt == nil || !ep0.PublishedAt.Equal(pub) {
		t.Errorf("episode 0 PublishedAt mismatch")
	}
	if ep0.FileSize == nil || *ep0.FileSize != 1000 {
		t.Errorf("episode 0 FileSize = %v", ep0.FileSize)
	}
	if ep0.EpisodeURL != "http://a.mp3" {
		t.Errorf("episode 0 EpisodeURL = %q", ep0.EpisodeURL)
	}

	ep1 := pf.Episodes[1]
	if ep1.GUID != "b" || ep1.Title != "B" {
		t.Error("episode 1 field mismatch")
	}
	if ep1.DurationSeconds == nil || *ep1.DurationSeconds != 1*3600+2*60+3 {
		t.Errorf("episode 1 DurationSeconds = %v", ep1.DurationSeconds)
	}
}

func TestParsedFeedFromGoFeed_NoEnclosures(t *testing.T) {
	f := makeFeed("F", "D", "")
	f.Items = []*gofeed.Item{
		{
			GUID:        "no-enc",
			Title:       "No Enclosure",
			Description: "none",
		},
	}
	pf := parsedFeedFromGoFeed(f)
	if len(pf.Episodes) != 1 {
		t.Fatalf("expected 1 episode")
	}
	if pf.Episodes[0].EpisodeURL != "" {
		t.Error("expected empty EpisodeURL")
	}
	if pf.Episodes[0].FileSize != nil {
		t.Error("expected nil FileSize")
	}
}

func TestParsedFeedFromGoFeed_BadFileSize(t *testing.T) {
	f := makeFeed("F", "D", "")
	f.Items = []*gofeed.Item{
		{
			GUID:        "bad-size",
			Title:       "Bad Size",
			Enclosures:  []*gofeed.Enclosure{{URL: "http://x", Length: "not-a-number"}},
		},
	}
	pf := parsedFeedFromGoFeed(f)
	if len(pf.Episodes) != 1 {
		t.Fatalf("expected 1 episode")
	}
	if pf.Episodes[0].FileSize != nil {
		t.Error("expected nil FileSize on bad length string")
	}
}

func TestParsedFeedFromGoFeed_ZeroDuration(t *testing.T) {
	f := makeFeed("F", "D", "")
	f.Items = []*gofeed.Item{
		makeItemWithDuration("z", "Zero", "0:00:00"),
	}
	pf := parsedFeedFromGoFeed(f)
	if len(pf.Episodes) != 1 {
		t.Fatalf("expected 1 episode")
	}
	// Duration == 0 is explicitly skipped
	if pf.Episodes[0].DurationSeconds != nil {
		t.Error("expected nil DurationSeconds when parseDuration returns 0")
	}
}

func TestParsedFeedFromGoFeed_NilPubDate(t *testing.T) {
	f := makeFeed("F", "D", "")
	f.Items = []*gofeed.Item{
		makeItemWithEnclosure("np", "No Pub", "", "http://x", "0", nil),
	}
	pf := parsedFeedFromGoFeed(f)
	if len(pf.Episodes) != 1 {
		t.Fatalf("expected 1 episode")
	}
	if pf.Episodes[0].PublishedAt != nil {
		t.Error("expected nil PublishedAt")
	}
}

func TestParsedFeedFromGoFeed_EmptyFileSize(t *testing.T) {
	f := makeFeed("F", "D", "")
	f.Items = []*gofeed.Item{
		{
			GUID:       "empty-len",
			Title:      "Empty Length",
			Enclosures: []*gofeed.Enclosure{{URL: "http://x", Length: ""}},
		},
	}
	pf := parsedFeedFromGoFeed(f)
	if len(pf.Episodes) != 1 {
		t.Fatalf("expected 1 episode")
	}
	if pf.Episodes[0].FileSize != nil {
		t.Error("expected nil FileSize on empty length string")
	}
}

// ---------------------------------------------------------------------------
// TestExtractImageURL
// ---------------------------------------------------------------------------

func TestExtractImageURL_ITunes(t *testing.T) {
	f := makeFeed("", "", "https://itunes.img")
	got := extractImageURL(f)
	if got != "https://itunes.img" {
		t.Errorf("got %q, want iTunes image", got)
	}
}

func TestExtractImageURL_RSSImageFallback(t *testing.T) {
	f := makeFeed("", "", "")
	f.Image = &gofeed.Image{URL: "https://rss.img"}
	got := extractImageURL(f)
	if got != "https://rss.img" {
		t.Errorf("got %q, want RSS image", got)
	}
}

func TestExtractImageURL_Empty(t *testing.T) {
	f := makeFeed("", "", "")
	got := extractImageURL(f)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractImageURL_RSSImageEmptyButITunes(t *testing.T) {
	f := makeFeed("", "", "")
	f.ITunesExt = &ext.ITunesFeedExtension{Image: ""}
	f.Image = &gofeed.Image{URL: "https://fallback.img"}
	got := extractImageURL(f)
	if got != "https://fallback.img" {
		t.Errorf("got %q, want RSS fallback", got)
	}
}

// ---------------------------------------------------------------------------
// TestParseDuration
// ---------------------------------------------------------------------------

func TestParseDuration(t *testing.T) {
	cases := []struct {
		input string
		want  float64
	}{
		{"1:02:03", 3600 + 120 + 3},
		{"12:34", 12*60 + 34},
		{"45", 45},
		{"0:00:00", 0},
		{"0:01", 1},
		{"", 0},
		{"bad::input", 0},
		{"1:2:3:4", 0},
		{"abc", 0},
		{"01:30", 90},
	}

	for _, c := range cases {
		got := parseDuration(c.input)
		if got != c.want {
			t.Errorf("parseDuration(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDownloadCoverImage
// ---------------------------------------------------------------------------

func TestDownloadCoverImage_Success(t *testing.T) {
	imgData := []byte("fake-jpeg-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(imgData)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	cl := srv.Client()

	err := DownloadCoverImage(cl, srv.URL, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	coverPath := filepath.Join(tmpDir, "cover.jpg")
	data, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatalf("cover file not written: %v", err)
	}
	if !bytes.Equal(data, imgData) {
		t.Error("cover file content mismatch")
	}
}

func TestDownloadCoverImage_EmptyURL(t *testing.T) {
	dir := t.TempDir()
	err := DownloadCoverImage(http.DefaultClient, "", dir)
	if err != nil {
		t.Fatalf("expected nil error for empty URL, got %v", err)
	}

	// Ensure no file is created
	coverPath := filepath.Join(dir, "cover.jpg")
	if _, err := os.Stat(coverPath); !os.IsNotExist(err) {
		t.Error("expected no cover.jpg for empty URL")
	}
}

func TestDownloadCoverImage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	err := DownloadCoverImage(srv.Client(), srv.URL, t.TempDir())
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error message does not contain 404: %v", err)
	}
}

func TestDownloadCoverImage_NetworkError(t *testing.T) {
	// Use a URL that will definitely fail to connect
	err := DownloadCoverImage(http.DefaultClient, "http://[::1]:0/", t.TempDir())
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestDownloadCoverImage_WriteError(t *testing.T) {
	imgData := []byte("img")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(imgData)
	}))
	defer srv.Close()

	// Use a read-only directory so file creation fails
	tmpDir := filepath.Join(t.TempDir(), "readonly")
	os.MkdirAll(tmpDir, 0o555)
	defer os.Chmod(tmpDir, 0o755) // allow cleanup

	err := DownloadCoverImage(srv.Client(), srv.URL, tmpDir)
	if err == nil {
		t.Fatal("expected error when cannot create file")
	}
}

func TestDownloadCoverImage_ReadBodyError(t *testing.T) {
	// Simulate a body that succeeds headers but fails during read
	failingBody := &errorAfterNReader{n: 2, data: []byte("ab")}
	cl := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(failingBody),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	tmpDir := t.TempDir()
	err := DownloadCoverImage(cl, "http://anything", tmpDir)
	if err == nil {
		t.Fatal("expected error when body read fails")
	}
	if !strings.Contains(err.Error(), "write cover file") {
		t.Errorf("expected wrap with 'write cover file', got: %v", err)
	}
}

// errorAfterNReader returns data for first n bytes, then errors.
type errorAfterNReader struct {
	n    int
	read int
	data []byte
}

func (e *errorAfterNReader) Read(p []byte) (int, error) {
	if e.read >= e.n {
		return 0, fmt.Errorf("simulated read error")
	}
	max := e.n - e.read
	if len(p) < max {
		max = len(p)
	}
	if max > len(e.data) {
		max = len(e.data)
	}
	copy(p, e.data[:max])
	e.read += max
	return max, nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
