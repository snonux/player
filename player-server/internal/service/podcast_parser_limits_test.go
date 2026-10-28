package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/podcast"
)

func TestPodcastSocialInteractParserRetainsResponseLimit(t *testing.T) {
	// Exercise the actual parser with the restricted client's body cap. The
	// optional social extension must not hide an error later in the document.
	body := `<rss version="2.0" xmlns:p="https://podcastindex.org/namespace/1.0"><channel><title>Feed</title><item>` +
		`<p:socialInteract uri="at://did:plc:example/post/1"/><description>` + strings.Repeat("x", 1024) +
		`</description><guid isPermaLink="false">episode</guid><enclosure url="https://example.org/episode.mp3"/></item></channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	defer srv.Close()
	for _, cap := range []int64{512, int64(len(body))} {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			client := newRestrictedClient(time.Second, func(netip.Addr) bool { return true }, cap)
			defer client.CloseIdleConnections()
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			feed, err := podcast.ParseFeedReader(resp.Body)
			if cap < int64(len(body)) {
				if !errors.Is(err, errResponseTooLarge) {
					t.Fatalf("over cap: feed=%+v err=%v", feed, err)
				}
			} else if err != nil || len(feed.Episodes) != 1 || feed.Episodes[0].GUID != "episode" {
				t.Fatalf("at cap: feed=%+v err=%v", feed, err)
			}
		})
	}
}
