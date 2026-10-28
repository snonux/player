package podcast

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

// AT URIs identify repositories by DID, whose colons are not an HTTP port.
// gofeed 1.3.0 rejected the entire feed while resolving this unused extension.
func TestParseFeedSocialInteractATURI(t *testing.T) {
	const atURI = "at://did:plc:5us6v2xizw73xaafpqgf6orb/app.bsky.feed.post/3mymnfyhmic2u"
	const header = `<rss version="2.0" xmlns:p="https://podcastindex.org/namespace/1.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel>
<title>Social podcast</title><description>A &amp; B</description>
<itunes:image href="https://example.org/cover.jpg"/>
<itunes:new-feed-url>https://example.org/feed.xml</itunes:new-feed-url>`
	var body strings.Builder
	body.WriteString(header)
	want := &ParsedFeed{Title: "Social podcast", Description: "A & B", ImageURL: "https://example.org/cover.jpg", NewFeedURL: "https://example.org/feed.xml"}
	published := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	duration, size := 330.0, int64(12345)
	for i := range 98 {
		guid := fmt.Sprintf("https://example.org/episode/%d?x=1&y=2", i)
		enclosure := fmt.Sprintf("https://cdn.example.org/%d.mp3?token=a%%2Fb&part=1", i)
		fmt.Fprintf(&body, `<item><title>Episode %d</title><guid isPermaLink="false">%s</guid>
<description><![CDATA[<p>A & B</p><svg xmlns="http://www.w3.org/2000/svg"><text>art</text></svg>]]></description>
<pubDate>Fri, 02 Jan 2026 15:04:05 GMT</pubDate><itunes:duration>00:05:30</itunes:duration>
<enclosure url="%s" length="12345" type="audio/mpeg"/>
<p:socialInteract protocol="atproto" uri="%s"/>
</item>`, i, strings.ReplaceAll(guid, "&", "&amp;"), strings.ReplaceAll(enclosure, "&", "&amp;"), atURI)
		want.Episodes = append(want.Episodes, Episode{
			GUID: guid, Title: fmt.Sprintf("Episode %d", i),
			Description: `<p>A & B</p><svg xmlns="http://www.w3.org/2000/svg"><text>art</text></svg>`,
			PublishedAt: &published, EpisodeURL: enclosure, DurationSeconds: &duration, FileSize: &size,
		})
	}
	body.WriteString(`</channel></rss>`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, body.String())
	}))
	defer srv.Close()
	for _, mode := range []string{"reader", "URL"} {
		t.Run(mode, func(t *testing.T) {
			var got *ParsedFeed
			var err error
			if mode == "reader" {
				got, err = ParseFeedReader(strings.NewReader(body.String()))
			} else {
				got, err = ParseFeed(srv.Client(), srv.URL)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("parsed metadata changed: got %+v, want %+v", got, want)
			}
		})
	}

	// Upgrading the dependency must retain extension attributes themselves,
	// including unrelated URI attributes that are not HTTP URLs.
	feed, err := gofeed.NewParser().ParseString(strings.ReplaceAll(body.String(), "</item>", `<x:other xmlns:x="urn:example" uri="urn:example:record"/><x:invalid xmlns:x="urn:example" uri="https://example.org:bad/path"/></item>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, attr := range []struct{ prefix, element, uri string }{
		{"p", "socialInteract", atURI},
		{"x", "other", "urn:example:record"},
		{"x", "invalid", "https://example.org:bad/path"},
	} {
		extensions := feed.Items[0].Extensions[attr.prefix][attr.element]
		if len(extensions) != 1 || extensions[0].Attrs["uri"] != attr.uri {
			t.Fatalf("%s URI changed: %+v", attr.element, extensions)
		}
	}
}

func TestParseFeedReaderSocialInteractRejectsBrokenInput(t *testing.T) {
	const prefix = `<rss version="2.0" xmlns:p="https://podcastindex.org/namespace/1.0"><channel><title>Feed</title><item>`
	for _, tail := range []string{
		`<p:socialInteract uri="at://did:plc:example/post/1`,
		`<p:socialInteract uri="at://did:plc:example/post/1"/><title><![CDATA[unterminated`,
	} {
		if _, err := ParseFeedReader(strings.NewReader(prefix + tail)); err == nil {
			t.Fatalf("accepted malformed XML: %s", tail)
		}
	}
	if _, err := ParseFeedReader(&errorAfterNReader{n: len(prefix), data: []byte(prefix)}); err == nil {
		t.Fatal("lost reader error")
	}
}

func TestParseFeedReaderSocialInteractEncoding(t *testing.T) {
	// A non-UTF8 feed still passes through gofeed's charset decoder unchanged.
	body := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>" +
		`<rss version="2.0" xmlns:p="https://podcastindex.org/namespace/1.0"><channel><title>Caf` + "\xe9" + `</title><item>` +
		`<guid isPermaLink="false">identity</guid><p:socialInteract uri="at://did:plc:example/post/1"/>` +
		`<enclosure url="https://example.org/episode.mp3"/></item></channel></rss>`
	feed, err := ParseFeedReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "Café" || len(feed.Episodes) != 1 || feed.Episodes[0].GUID != "identity" {
		t.Fatalf("encoding changed metadata: %+v", feed)
	}
}
