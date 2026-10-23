package repository

import (
	"context"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
)

// TestEpisodeEditsFollowTheSubscriptionURLs: a device keys a feed as
// feed:a (its subscription lists the URLs of a and b); the server knows the
// feed only as b. Episode edits sent under feed:a must land under the key
// the subscription resolved to, in the same request and in later requests
// without the subscription edit, instead of staying state-only under feed:a
// where the client, having rewritten its keys, would never see them again.
func TestEpisodeEditsFollowTheSubscriptionURLs(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID, err := f.s.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: f.now})
	f.must(err)
	feedID, err := f.s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "https://b.example.com/rss", CreatedAt: f.now})
	f.must(err)
	_, err = f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: "ep1", EpisodeURL: "https://cdn.example.com/1.mp3", CreatedAt: f.now})
	f.must(err)
	keyA, keyB := "feed:a.example.com/rss", "feed:b.example.com/rss"
	sub := edit("sub", keyA, true, 1000)
	sub.URLs = []string{"https://a.example.com/rss", "https://b.example.com/rss"}
	out, err := f.s.SyncPodcast(ctx, f.alice, model.PodcastSyncBatch{Now: f.now,
		Subscriptions: []model.PodcastSubscriptionEdit{sub}, Episodes: []model.PodcastEpisodeEdit{posItem("p1", keyA, "guid:ep1", 11, 1000)}})
	f.must(err)
	if r, _ := f.state(f.alice, keyB, "guid:ep1"); r.position != 11 || len(out.Acked) != 2 {
		t.Fatalf("same request: %+v %+v", r, out)
	}
	// Later, without the subscription edit: resolved through the stored
	// register's urls.
	out = f.episodes(f.alice, posItem("p2", keyA, "guid:ep1", 22, 2000), posItem("p3", keyA, "guid:new", 33, 2000))
	if r, _ := f.state(f.alice, keyB, "guid:ep1"); r.position != 22 {
		t.Fatalf("later request: %+v", r)
	}
	if r, _ := f.state(f.alice, keyB, "guid:new"); r.position != 33 {
		t.Fatalf("state-only episode not kept under the resolved feed: %+v", r)
	}
	var stranded int
	f.must(f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_episode_state WHERE feed_key = ?`, keyA).Scan(&stranded))
	if stranded != 0 {
		t.Fatalf("%d episode rows stranded under %s", stranded, keyA)
	}
	if !hasMapping(out.Resolved, model.PodcastKeyMapping{Kind: "feed", From: keyA, To: keyB}) {
		t.Fatalf("feed resolution not reported: %+v", out.Resolved)
	}
	// Another user's subscription urls are no evidence for bob.
	f.episodes(f.bob, posItem("b", keyA, "guid:ep1", 5, 1000))
	if r, _ := f.state(f.bob, keyA, "guid:ep1"); r.position != 5 {
		t.Fatalf("bob's edit must stay under his own key: %+v", r)
	}
}

func hasMapping(ms []model.PodcastKeyMapping, want model.PodcastKeyMapping) bool {
	for _, m := range ms {
		if m == want {
			return true
		}
	}
	return false
}

// TestScopeReusesTheRequestsResolution: an item's enclosure evidence can
// move its GUID key to another episode (§3.3 enclosure preference); a scope
// on the same keys in that request answers the episode the edit went to.
func TestScopeReusesTheRequestsResolution(t *testing.T) {
	f := newSyncFixture(t)
	f.catalogEpisodes()
	item := posItem("p", testFeedKey, testEp1Key, 7, 1000)
	item.EnclosureURL = "https://cdn.example.com/2.mp3"
	out, err := f.s.SyncPodcast(context.Background(), f.alice, model.PodcastSyncBatch{Now: f.now,
		Episodes: []model.PodcastEpisodeEdit{item}, Scope: &model.PodcastSyncScope{FeedKey: testFeedKey, EpisodeKey: testEp1Key}})
	f.must(err)
	if len(out.Episodes) != 1 || out.Episodes[0].EpisodeKey != testEp2Key || out.Episodes[0].Position.Seconds != 7 {
		t.Fatalf("scope answer: %+v", out.Episodes)
	}
}

// TestMirrorStartsOverNearTheEnd: an episode uncompleted (or replayed) at
// its end projects position 0 to old clients, so they do not resume at the
// end and complete it again at once (§4.2 start position rule); the
// register keeps the end position.
func TestMirrorStartsOverNearTheEnd(t *testing.T) {
	f := newBridgeFixture(t) // the copy is 600 s long
	ctx := context.Background()
	end := posItem("end", testFeedKey, testEp1Key, 597, 1000)
	end.Completion = &model.PodcastCompletionEdit{EditID: "cmp-end-end-end-", Completed: true, Source: "auto", ObservedMS: 1000}
	f.episodes(f.alice, end)
	if m := f.mirror(f.alice, f.mediaID); !m.Finished || m.PositionSeconds != 597 {
		t.Fatalf("completed at the end: %+v", m)
	}
	f.episodes(f.alice, compItem("un", testFeedKey, testEp1Key, false, "manual", 2000))
	if m := f.mirror(f.alice, f.mediaID); m.Finished || m.PositionSeconds != 0 {
		t.Fatalf("uncompleted at the end: %+v", m)
	}
	out, err := f.s.SyncPlayback(ctx, f.alice, f.mediaID, model.PlaybackSync{})
	f.must(err)
	if out.Progress.PositionSeconds != 0 || out.Progress.Finished || out.Progress.ObservedMS != 2000 {
		t.Fatalf("old client answer: %+v", out.Progress)
	}
	if r := f.aliceEp1(); r.position != 597 {
		t.Fatalf("register lost the end position: %+v", r)
	}
	// Further from the end the position is projected as is.
	f.episodes(f.alice, posItem("mid", testFeedKey, testEp1Key, 594, 3000))
	if m := f.mirror(f.alice, f.mediaID); m.PositionSeconds != 594 {
		t.Fatalf("position before the margin: %+v", m)
	}
	// Without a known duration nothing is rewritten.
	f.must(execSQL(f.syncFixture, `UPDATE media SET duration = NULL WHERE id = ?`, f.mediaID))
	f.episodes(f.alice, posItem("end2", testFeedKey, testEp1Key, 599, 4000))
	if m := f.mirror(f.alice, f.mediaID); m.PositionSeconds != 599 {
		t.Fatalf("unknown duration: %+v", m)
	}
}

// TestBridgeClampUsesTheStoreClock: legacy clocks are clamped against the
// clock the store was wired with (the services' clock), not the wall clock.
func TestBridgeClampUsesTheStoreClock(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	store := f.s.WithClock(&clock.MockClock{T: now})
	_, err := store.SyncPlayback(ctx, f.alice, f.mediaID, model.PlaybackSync{Progress: &model.SyncedProgress{PositionSeconds: 50, ObservedMS: now.Add(time.Hour).UnixMilli()}})
	f.must(err)
	f.must(store.FinishProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: f.mediaID, Finished: true, UpdatedAt: now.Add(time.Hour)}))
	if r := f.aliceEp1(); r.positionMS != now.UnixMilli() || r.completionMS != now.UnixMilli() {
		t.Fatalf("clocks %d/%d, want %d", r.positionMS, r.completionMS, now.UnixMilli())
	}
	// Within the 5-minute skew the device's clock is kept.
	near := now.Add(4 * time.Minute)
	f.must(store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: f.mediaID, PositionSeconds: 60, UpdatedAt: near}))
	if r := f.aliceEp1(); r.positionMS != near.UnixMilli() {
		t.Fatalf("near-future clock clamped: %d", r.positionMS)
	}
}
