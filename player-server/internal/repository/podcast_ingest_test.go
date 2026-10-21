package repository

import (
	"context"
	"fmt"
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

// ingestDoc builds a parsed feed document from (guid, enclosure) pairs.
func ingestDoc(pairs ...[2]string) []model.PodcastEpisode {
	eps := make([]model.PodcastEpisode, len(pairs))
	for i, p := range pairs {
		eps[i] = model.PodcastEpisode{GUID: p[0], EpisodeURL: p[1], Title: "t" + p[0]}
	}
	return eps
}

// episodeKeys returns the feed's episodes as key → enclosure key.
func episodeKeys(t *testing.T, s *SQLite, feedID int64) map[string]string {
	t.Helper()
	rows, err := s.db.Query(`SELECT episode_key, enclosure_key FROM podcast_episodes WHERE feed_id = ?`, feedID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var k, enc string
		if err := rows.Scan(&k, &enc); err != nil {
			t.Fatal(err)
		}
		got[k] = enc
	}
	return got
}

func TestIngestFeedEpisodesKeysAliasesAndIdempotence(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	f.sync(f.alice, 0, 0, edit("a", "feed:example.com/rss", true, 1))
	feed, err := f.s.GetFeedByKey(ctx, "feed:example.com/rss")
	if err != nil || feed == nil {
		t.Fatalf("feed: %v %v", feed, err)
	}
	doc := ingestDoc([2]string{"g1", "https://cdn.example.com/1.mp3"}, [2]string{"g2", "https://cdn.example.com/2.mp3"},
		[2]string{"", "https://cdn.example.com/3.mp3"}, [2]string{"", "relative.mp3"})
	for range 2 { // the second ingest of the same document changes nothing
		if err := f.s.IngestFeedEpisodes(ctx, feed, doc, f.now); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{"guid:g1": "enc:cdn.example.com/1.mp3", "guid:g2": "enc:cdn.example.com/2.mp3",
		"enc:cdn.example.com/3.mp3": "enc:cdn.example.com/3.mp3"}
	if got := episodeKeys(t, f.s, feed.ID); len(got) != 3 || got["guid:g1"] != want["guid:g1"] || got["enc:cdn.example.com/3.mp3"] == "" {
		t.Fatalf("episodes = %v, want %v", got, want)
	}
	seq, _ := maxChangeSeq(ctx, f.s.db)

	// g1's GUID changes (same enclosure): an alias, no new episode. g2's
	// enclosure rotates: refreshed in place, no alias.
	doc2 := ingestDoc([2]string{"g1-new", "https://cdn.example.com/1.mp3"}, [2]string{"g2", "https://cdn.example.com/2.mp3?sig=x"})
	if err := f.s.IngestFeedEpisodes(ctx, feed, doc2, f.now); err != nil {
		t.Fatal(err)
	}
	got := episodeKeys(t, f.s, feed.ID)
	if len(got) != 3 || got["guid:g2"] != "enc:cdn.example.com/2.mp3?sig=x" {
		t.Fatalf("after second document: %v", got)
	}
	var aliases int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_episode_aliases WHERE feed_key = ? AND alias_key = 'guid:g1-new' AND episode_key = 'guid:g1'`, feed.FeedKey).Scan(&aliases); err != nil || aliases != 1 {
		t.Fatalf("GUID change alias: %d %v", aliases, err)
	}
	// Ingesting the second document again finds g1-new through its alias.
	if err := f.s.IngestFeedEpisodes(ctx, feed, doc2, f.now); err != nil {
		t.Fatal(err)
	}
	if got := episodeKeys(t, f.s, feed.ID); len(got) != 3 {
		t.Fatalf("re-ingest through the alias created episodes: %v", got)
	}
	var total int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_episode_aliases`).Scan(&total); err != nil || total != 1 {
		t.Fatalf("rotating enclosure added aliases: %d %v", total, err)
	}
	// The subscriber pulls the alias; a user without rows for the feed does not.
	if out := f.sync(f.alice, seq, 0); len(out.Aliases) != 1 || out.Aliases[0].Kind != "episode" || out.Aliases[0].To != "guid:g1" {
		t.Fatalf("subscriber alias pull: %+v", out.Aliases)
	}
	if out := f.sync(f.bob, 0, 0); len(out.Aliases) != 0 {
		t.Fatalf("alias leaked: %+v", out.Aliases)
	}
}

func TestIngestKeepsMediaLinksAndClearDownloads(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	f.sync(f.alice, 0, 0, edit("a", "feed:example.com/rss", true, 1))
	feed, _ := f.s.GetFeedByKey(ctx, "feed:example.com/rss")
	doc := ingestDoc([2]string{"g1", "https://cdn.example.com/1.mp3"})
	if err := f.s.IngestFeedEpisodes(ctx, feed, doc, f.now); err != nil {
		t.Fatal(err)
	}
	var epID int64
	if err := f.s.db.QueryRow(`SELECT id FROM podcast_episodes WHERE feed_id = ?`, feed.ID).Scan(&epID); err != nil {
		t.Fatal(err)
	}
	mediaID, err := f.s.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "x/1.mp3", FileName: "1.mp3", AbsPath: "/m/1.mp3", Type: model.MediaTypeAudio, CreatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpdateEpisodeMedia(ctx, epID, mediaID, "1.mp3"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.IngestFeedEpisodes(ctx, feed, doc, f.now); err != nil {
		t.Fatal(err)
	}
	ep, err := f.s.GetEpisodeByID(ctx, epID)
	if err != nil || ep.MediaID == nil || *ep.MediaID != mediaID || !ep.IsDownloaded {
		t.Fatalf("re-ingest lost the media link: %+v %v", ep, err)
	}
	// Clearing stale flags must not detach a live copy, including a
	// download that finished concurrently with administrative removal.
	if err := f.s.ClearEpisodeDownloads(ctx, feed.ID); err != nil {
		t.Fatal(err)
	}
	ep, _ = f.s.GetEpisodeByID(ctx, epID)
	if ep.MediaID == nil || *ep.MediaID != mediaID {
		t.Fatal("clear detached a live copy")
	}
	if err := f.s.HardDeleteMedia(ctx, mediaID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ClearEpisodeDownloads(ctx, feed.ID); err != nil {
		t.Fatal(err)
	}
	ep, _ = f.s.GetEpisodeByID(ctx, epID)
	if ep.MediaID != nil || ep.IsDownloaded || ep.EpisodeKey != "guid:g1" {
		t.Fatalf("clear downloads: %+v", ep)
	}
}

func TestCreateFeedKeys(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID, err := f.s.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	good := &model.PodcastFeed{SetID: setID, FeedURL: "HTTPS://Example.COM:443/rss#x", CreatedAt: f.now}
	if _, err := f.s.CreateFeed(ctx, good); err != nil || good.FeedKey != "feed:example.com/rss" {
		t.Fatalf("key = %q, %v", good.FeedKey, err)
	}
	bad := &model.PodcastFeed{SetID: setID, FeedURL: "ftp://example.com/rss", CreatedAt: f.now}
	id, err := f.s.CreateFeed(ctx, bad)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := f.s.GetFeedByID(ctx, id)
	if stored.FeedKey != bad.FeedKey || bad.FeedKey != fmt.Sprintf("legacy-feed:%d", id) {
		t.Fatalf("legacy key = %q / %q", bad.FeedKey, stored.FeedKey)
	}
	// A second catalog row with the same key is refused.
	if _, err := f.s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "http://example.com/rss", CreatedAt: f.now}); err == nil {
		t.Fatal("duplicate catalog key accepted")
	}
	if _, err := f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: id, EpisodeURL: "not a url", CreatedAt: f.now}); err == nil {
		t.Fatal("episode without guid or enclosure accepted")
	}
}

func TestMoveFeedAliasesCapLegacyAndMerge(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID, err := f.s.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	create := func(url string) *model.PodcastFeed {
		feed := &model.PodcastFeed{SetID: setID, FeedURL: url, CreatedAt: f.now}
		id, err := f.s.CreateFeed(ctx, feed)
		if err != nil {
			t.Fatal(err)
		}
		feed.ID = id
		return feed
	}
	feed := create("https://a.example.com/rss")
	other := create("https://b.example.com/rss")
	for i := 0; i < 18; i++ {
		moved, err := f.s.MoveFeed(ctx, feed.ID, fmt.Sprintf("https://a%d.example.com/rss", i))
		if err != nil || moved != FeedMoved {
			t.Fatalf("move %d: %v %v", i, moved, err)
		}
	}
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_feed_aliases WHERE feed_key = ?`, feed.FeedKey).Scan(&n); err != nil || n != 16 {
		t.Fatalf("aliases = %d %v, want the cap of 16", n, err)
	}
	if got, _ := f.s.GetFeedByID(ctx, feed.ID); got.FeedURL != "https://a17.example.com/rss" || got.FeedKey != "feed:a.example.com/rss" {
		t.Fatalf("after moves: %+v", got)
	}
	if moved, err := f.s.MoveFeed(ctx, feed.ID, "https://a3.example.com/rss"); moved != FeedMoved || err != nil {
		t.Fatalf("move to an own alias: %v %v", moved, err)
	}
	if moved, err := f.s.MoveFeed(ctx, feed.ID, "ftp://nope/"); moved != FeedNotMoved || err != nil {
		t.Fatalf("move to a URL without key: %v %v", moved, err)
	}
	if moved, err := f.s.MoveFeed(ctx, 9999, "https://c.example.com/rss"); moved != FeedNotMoved || err != nil {
		t.Fatalf("move of a missing feed: %v %v", moved, err)
	}
	// A legacy key is retired by a valid URL and stays resolvable as alias.
	legacy := create("ftp://legacy/rss")
	if moved, err := f.s.MoveFeed(ctx, legacy.ID, "https://legacy.example.com/rss"); moved != FeedMoved || err != nil {
		t.Fatalf("legacy move: %v %v", moved, err)
	}
	for _, key := range []string{"feed:legacy.example.com/rss", legacy.FeedKey} {
		if got, _ := f.s.GetFeedByKey(ctx, key); got == nil || got.ID != legacy.ID || got.FeedKey != "feed:legacy.example.com/rss" {
			t.Fatalf("after retirement %s → %+v", key, got)
		}
	}
	// A move into another catalog feed merges into it; a merged feed does
	// not move again.
	if moved, err := f.s.MoveFeed(ctx, feed.ID, other.FeedURL); moved != FeedMerged || err != nil {
		t.Fatalf("move into another feed: %v %v", moved, err)
	}
	if moved, err := f.s.MoveFeed(ctx, feed.ID, "https://d.example.com/rss"); moved != FeedNotMoved || err != nil {
		t.Fatalf("move of a merged feed: %v %v", moved, err)
	}
}

func TestSyncPullsMigratedEpisodeStateWithAliases(t *testing.T) {
	f := newSyncFixture(t)
	key := "feed:example.com/rss"
	f.sync(f.alice, 0, 0, edit("a", key, true, 1))
	if _, err := f.s.db.Exec(`INSERT INTO podcast_episode_state (user_id, feed_key, episode_key, position_seconds, position_ms,
 completed, completion_source, completion_ms, change_seq) VALUES (?, ?, 'guid:g1', 42, 5, 0, '', 0, 100), (?, ?, 'guid:g2', 0, 0, 1, 'manual', 7, 101)`,
		f.alice, key, f.bob, key); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec(`INSERT INTO podcast_episode_aliases (feed_key, alias_key, episode_key, change_seq) VALUES (?, 'enc:cdn/1.mp3', 'guid:g1', 1)`, key); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec(`UPDATE podcast_sync_seq SET next_seq = 102`); err != nil {
		t.Fatal(err)
	}
	out := f.sync(f.alice, 0, 0)
	if len(out.Episodes) != 1 || out.Episodes[0].Position == nil || out.Episodes[0].Position.Seconds != 42 || out.Episodes[0].Completion != nil {
		t.Fatalf("episode pull: %+v", out.Episodes)
	}
	if len(out.Aliases) != 1 || out.Aliases[0].Kind != "episode" || out.Aliases[0].FeedKey != key {
		t.Fatalf("episode aliases: %+v", out.Aliases)
	}
	if bob := f.sync(f.bob, 0, 0); len(bob.Episodes) != 1 || bob.Episodes[0].Completion == nil || bob.Episodes[0].Position != nil {
		t.Fatalf("bob's state: %+v", bob.Episodes)
	}
}
