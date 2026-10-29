package podcast

import (
	"strings"
	"testing"
)

func TestEpisodeArtworkAndCompleteShowNotes(t *testing.T) {
	xml := `<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:media="http://search.yahoo.com/mrss/"><channel><title>Show</title><itunes:image href="https://example.com/feed.jpg"/>
 <item><guid>one</guid><title>One</title><description>Short summary</description><content:encoded><![CDATA[<p>Complete notes &amp; <a href="https://example.com/notes">links</a></p>]]></content:encoded><itunes:image href="https://example.com/one.jpg"/><pubDate>Wed, 28 Oct 2026 12:00:00 +0000</pubDate><enclosure url="https://example.com/one.mp3"/></item>
 <item><guid>two</guid><title>Two</title><media:thumbnail url="https://example.com/two.jpg"/><enclosure url="https://example.com/two.mp3"/></item>
 <item><guid>three</guid><title>Three</title><itunes:summary>Fallback notes</itunes:summary><enclosure url="https://example.com/three.mp3"/></item>
 </channel></rss>`
	feed, err := ParseFeedReader(strings.NewReader(xml))
	if err != nil {
		t.Fatal(err)
	}
	if feed.ImageURL != "https://example.com/feed.jpg" || len(feed.Episodes) != 3 {
		t.Fatalf("feed: %+v", feed)
	}
	a, b, c := feed.Episodes[0], feed.Episodes[1], feed.Episodes[2]
	if a.ImageURL != "https://example.com/one.jpg" || !strings.Contains(a.Description, "Complete notes") || a.PublishedAt == nil {
		t.Fatalf("episode: %+v", a)
	}
	if b.ImageURL != "https://example.com/two.jpg" || b.PublishedAt != nil {
		t.Fatalf("missing publisher date must stay unknown: %+v", b)
	}
	if c.ImageURL != "" || c.Description != "Fallback notes" {
		t.Fatalf("feed art fallback stays separate from item art: %+v", c)
	}
}
