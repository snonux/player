package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/player/internal/model"
)

type lyricStoreFixture struct{}

func (lyricStoreFixture) HasProviderLyrics(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (lyricStoreFixture) LoadProviderLyrics(context.Context, int64, int64, string) ([]byte, error) {
	return nil, nil
}
func (lyricStoreFixture) SaveProviderLyrics(context.Context, int64, int64, string, []byte) error {
	return nil
}
func (lyricStoreFixture) DeleteProviderLyrics(context.Context, int64, int64) error { return nil }

func TestProviderLyricsHTTPBoundsAndRedirect(t *testing.T) {
	media := &model.Media{Music: &model.MusicMetadata{Title: "Song", Artist: "Artist"}}
	for _, tt := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"rich", `{"synced":true,"lines":[{"text":"Hello world","start":0,"words":[{"text":"Hello ","start":0},{"text":"world","start":1}]}]}`, 200, false},
		{"empty", "", 204, false},
		{"error", "", 503, true},
		{"redirect", "", 302, true},
		{"too large", strings.Repeat("x", 1024*1024+1), 200, true},
		{"invalid JSON", "{", 200, true},
		{"unordered", `{"synced":true,"lines":[{"text":"later","start":2},{"text":"earlier","start":1}]}`, 200, true},
		{"negative", `{"synced":true,"lines":[{"text":"bad","start":-1}]}`, 200, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			count := 0
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if r.URL.Path != "/lookup" {
					t.Error("followed provider redirect")
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("forwarded credentials")
				}
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer fixture.Close()
			p := &ProviderLyrics{endpoint: fixture.URL + "/lookup", client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
			result, err := p.fetch(context.Background(), media)
			if (err != nil) != tt.fail {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if count != 1 {
				t.Fatal("unexpected request count", count)
			}
		})
	}
	for _, endpoint := range []string{"", "file:///etc/passwd", "http://user:password@localhost/", "https://provider.test/#fragment"} {
		if NewProviderLyrics(endpoint, lyricStoreFixture{}) != nil {
			t.Fatal("enabled invalid endpoint", endpoint)
		}
	}
}

func TestProviderLyricsIdentityMemoAndCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.mp3")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &ProviderLyrics{endpoint: "http://fixture/lookup", memo: map[string]string{}}
	media := &model.Media{AbsPath: path, Music: &model.MusicMetadata{Title: "Song", Artist: "Artist"}}
	identity := func() string {
		t.Helper()
		value, err := p.contentIdentity(context.Background(), root, media)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := identity()
	if first != identity() {
		t.Fatal("unstable memo")
	}
	media.Music.Title = "Retagged"
	retagged := identity()
	if first == retagged {
		t.Fatal("tags reused identity")
	}
	p.endpoint = "http://different/lookup"
	if identity() == retagged {
		t.Fatal("provider config reused identity")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.contentIdentity(ctx, root, &model.Media{AbsPath: path}); err == nil {
		t.Fatal("ignored cancellation")
	}
	if err := os.Truncate(path, 512*1024*1024+1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.contentIdentity(context.Background(), root, media); err == nil {
		t.Fatal("unbounded source hashing")
	}
}
