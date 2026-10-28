package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPodcastDiscoverySearch(t *testing.T) {
	d := NewPodcastDiscovery()
	calls := 0
	d.client = &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "itunes.apple.com" || r.URL.Query().Get("term") != "space & science" || r.URL.Query().Get("entity") != "podcast" {
			t.Fatalf("bad search URL: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("credentials leaked")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"collectionName":"Space","artistName":"Astronomer","feedUrl":"https://example.org/feed"},{"collectionName":"Duplicate","feedUrl":"https://example.org/feed"},{"collectionName":"Unsafe","feedUrl":"javascript:alert(1)"},{"collectionName":"Credentials","feedUrl":"https://user:pass@example.org/feed"},{"collectionName":"No feed"}]}`))}, nil
	})}
	rows, err := d.Search(context.Background(), " space & science ")
	if err != nil || len(rows) != 1 || rows[0].Title != "Space" || rows[0].Author != "Astronomer" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if _, err = d.Search(context.Background(), "SPACE & SCIENCE"); err != nil || calls != 1 {
		t.Fatalf("cache missed calls=%d err=%v", calls, err)
	}
	if _, err = d.Search(context.Background(), "another query"); err != ErrDiscoveryBusy {
		t.Fatalf("missing rate limit: %v", err)
	}
}
func TestPodcastDiscoveryValidationAndFailure(t *testing.T) {
	for _, query := range []string{"", "a", strings.Repeat("x", 101)} {
		if _, err := NewPodcastDiscovery().Search(context.Background(), query); err != ErrDiscoveryQuery {
			t.Fatalf("query error %v", err)
		}
	}
	for _, body := range []string{"invalid", strings.Repeat("x", (2<<20)+1)} {
		d := NewPodcastDiscovery()
		d.client = &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, err := d.Search(context.Background(), "podcasts"); err != ErrDiscoveryUnavailable {
			t.Fatalf("expected upstream failure, got %v", err)
		}
	}
}

func TestPodcastDiscoveryPopular(t *testing.T) {
	d := NewPodcastDiscovery()
	calls := 0
	d.client = &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"results":[{"collectionName":"Popular","artistName":"Publisher","feedUrl":"https://example.org/popular"}]}`
		if calls == 1 {
			if r.URL.Path != "/de/rss/toppodcasts/limit=20/json" {
				t.Fatalf("chart path %s", r.URL.Path)
			}
			body = `{"feed":{"entry":[{"id":{"attributes":{"im:id":"123"}}},{"id":{"attributes":{"im:id":"not an id"}}}]}}`
		} else if r.URL.Path != "/lookup" || r.URL.Query().Get("id") != "123" || r.URL.Query().Get("country") != "DE" {
			t.Fatalf("lookup %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	rows, err := d.Popular(context.Background(), "DE")
	if err != nil || len(rows) != 1 || rows[0].Title != "Popular" || calls != 2 {
		t.Fatalf("rows=%v calls=%d err=%v", rows, calls, err)
	}
	if _, err = d.Popular(context.Background(), "de"); err != nil || calls != 2 {
		t.Fatal("chart cache missed", err)
	}
	if _, err = d.Popular(context.Background(), "../"); err != ErrDiscoveryQuery {
		t.Fatal("invalid country allowed", err)
	}
}
