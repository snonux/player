package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPodcastHTTPClient_RefusesPrivateAddresses checks that, by default, a
// feed or episode URL pointing at the server's own network is not fetched,
// and that PODCAST_ALLOW_PRIVATE_NETWORKS lifts the restriction.
func TestPodcastHTTPClient_RefusesPrivateAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("internal"))
	}))
	defer server.Close()

	if resp, err := podcastHTTPClient(false).Get(server.URL); err == nil {
		resp.Body.Close()
		t.Fatal("loopback fetch succeeded, want it refused")
	}
	resp, err := podcastHTTPClient(true).Get(server.URL)
	if err != nil {
		t.Fatalf("allowPrivate fetch: %v", err)
	}
	resp.Body.Close()
}
