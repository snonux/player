package repository

import (
	"context"
	"encoding/json"
	"github.com/snonux/player/internal/model"
	"strings"
	"testing"
	"time"
)

func TestPodcastMetadataUpgradeRefreshPreservesEpisodeStateAndMediaTags(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	f.sync(f.alice, 0, 0, edit("sub", "feed:example.com/rss", true, 1))
	feed, err := f.s.GetFeedByKey(ctx, "feed:example.com/rss")
	f.must(err)
	feed.ImageURL = "https://example.com/feed.jpg"
	feed.Title = "Beyond Coding"
	feed.LastETag = "old"
	f.must(f.s.UpdateFeed(ctx, feed))
	published := time.Date(2026, 10, 28, 12, 0, 0, 123456789, time.UTC)
	doc := []model.PodcastEpisode{{GUID: "same-guid", Title: "Before", EpisodeURL: "https://cdn.example.com/a.mp3", PublishedAt: &published}}
	f.must(f.s.IngestFeedEpisodes(ctx, feed, doc, f.now))
	ep, err := f.s.GetEpisodeByGUID(ctx, feed.ID, "same-guid")
	f.must(err)
	mid := f.serverCopy(feed.SetID, ep.ID, "keep.mp3")
	media, err := f.s.GetMediaByID(ctx, mid)
	f.must(err)
	media.Music = &model.MusicMetadata{Title: "Title Placeholder Episode ID: 128221282", Album: "Beyond Coding"}
	f.must(f.s.UpdateMedia(ctx, media))
	f.episodes(f.alice, posItem("position", feed.FeedKey, ep.EpisodeKey, 42, 10), compItem("completed", feed.FeedKey, ep.EpisodeKey, true, "manual", 10))
	// Simulate the old persisted schema; upgrade must retain identities/copies.
	_, err = f.s.db.Exec(`ALTER TABLE podcast_episodes DROP COLUMN image_url`)
	f.must(err)
	f.must(migratePodcastEpisodeArtwork(f.s.db))
	stored, err := f.s.GetFeedByID(ctx, feed.ID)
	f.must(err)
	if stored.LastETag != "" {
		t.Fatal("old validator would block metadata repair")
	}
	stored.LastETag = "new"
	f.must(f.s.UpdateFeed(ctx, stored))
	f.must(migratePodcastEpisodeArtwork(f.s.db))
	stored, _ = f.s.GetFeedByID(ctx, feed.ID)
	if stored.LastETag != "new" {
		t.Fatal("migration must reset validators only once")
	}
	doc[0].Title = "Actual publisher episode"
	doc[0].Description = "<p>Complete show notes</p>"
	doc[0].ImageURL = "https://example.com/episode.jpg"
	f.must(f.s.IngestFeedEpisodes(ctx, feed, doc, f.now))
	after, err := f.s.GetEpisodeByID(ctx, ep.ID)
	f.must(err)
	if after.EpisodeKey != ep.EpisodeKey || after.GUID != ep.GUID || after.MediaID == nil || *after.MediaID != mid || after.FileName != "keep.mp3" || !after.IsDownloaded || after.Title != doc[0].Title {
		t.Fatalf("refresh lost identity/download: %+v", after)
	}
	views, err := f.s.ListSubscribedEpisodes(ctx, f.alice, PodcastEpisodeQuery{Filter: "all", Limit: 10})
	f.must(err)
	if len(views) != 1 || views[0].ImageURL != doc[0].ImageURL || views[0].FeedImageURL != feed.ImageURL || views[0].PositionSeconds != 42 || !views[0].Completed {
		t.Fatalf("view: %+v", views)
	}
	copies, err := f.s.ListEpisodesWithStatus(ctx, f.alice, feed.ID, 10, 0)
	f.must(err)
	if len(copies) != 1 || copies[0].FeedImageURL != feed.ImageURL || copies[0].ImageURL != doc[0].ImageURL {
		t.Fatalf("legacy view: %+v", copies)
	}
	check := func(m *model.Media) {
		t.Helper()
		if m.Podcast == nil || m.Podcast.Title != "Actual publisher episode" || m.Podcast.ImageURL != "https://example.com/episode.jpg" || m.Podcast.PublishedAt == nil || !m.Podcast.PublishedAt.Equal(published) || m.Music.Title != "Title Placeholder Episode ID: 128221282" || m.FileName != "keep.mp3" {
			t.Fatalf("projected metadata/tags: %+v %+v", m.Podcast, m.Music)
		}
	}
	current, err := f.s.GetMediaByID(ctx, mid)
	f.must(err)
	check(current)
	all, err := f.s.ListMedia(ctx, MediaFilter{SetID: &feed.SetID})
	f.must(err)
	if len(all) != 1 {
		t.Fatal(all)
	}
	check(&all[0])
	raw, err := json.Marshal(current)
	f.must(err)
	if !strings.Contains(string(raw), "2026-10-28T12:00:00.123456789Z") {
		t.Fatalf("publication timestamp must be RFC3339 with precision: %s", raw)
	}
	// A partial later document must not erase useful publisher metadata.
	doc[0].Title = ""
	doc[0].Description = ""
	doc[0].ImageURL = ""
	doc[0].PublishedAt = nil
	f.must(f.s.IngestFeedEpisodes(ctx, feed, doc, f.now))
	current, err = f.s.GetMediaByID(ctx, mid)
	f.must(err)
	check(current)
}

func TestPodcastMetadataProjectionTimestampLayoutsAndUnknowns(t *testing.T) {
	for _, raw := range []string{"2026-10-28 12:00:00.123456789 +0000 UTC", "2026-10-28T12:00:00.123456789Z", "2026-10-28 12:00:00.123456789"} {
		text, _ := json.Marshal(map[string]any{"title": "Episode", "published_at": raw})
		m, err := decodePodcastMediaMetadata(string(text))
		if err != nil || m.PublishedAt == nil || m.PublishedAt.Nanosecond() != 123456789 || m.PublishedAt.Location() != time.UTC {
			t.Fatalf("%s: %+v %v", raw, m, err)
		}
	}
	for _, raw := range []string{`{"published_at":null}`, `{"published_at":"invalid"}`} {
		m, err := decodePodcastMediaMetadata(raw)
		if err != nil || m.PublishedAt != nil {
			t.Fatalf("unknown optional date blocks media: %+v %v", m, err)
		}
	}
	if _, err := decodePodcastMediaMetadata(`broken`); err == nil {
		t.Fatal("malformed projection JSON accepted")
	}
}
