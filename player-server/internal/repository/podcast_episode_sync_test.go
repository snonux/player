package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/snonux/player/internal/model"
)

const (
	testFeedKey = "feed:example.com/rss"
	testEp1Key  = "guid:ep1"
	testEp2Key  = "enc:cdn.example.com/2.mp3"
)

// catalogEpisodes creates the shared podcast set with one catalog feed
// (testFeedKey) and two episodes: ep1 with a GUID, ep2 keyed by its
// enclosure. It returns the set id and both episode ids.
func (f *syncFixture) catalogEpisodes() (int64, int64, int64) {
	f.t.Helper()
	ctx := context.Background()
	setID, err := f.s.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: f.now})
	f.must(err)
	feedID, err := f.s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "https://example.com/rss", Title: "Show", CreatedAt: f.now})
	f.must(err)
	ep1, err := f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: "ep1", EpisodeURL: "https://cdn.example.com/1.mp3", CreatedAt: f.now})
	f.must(err)
	ep2, err := f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, EpisodeURL: "https://cdn.example.com/2.mp3", CreatedAt: f.now})
	f.must(err)
	return setID, ep1, ep2
}

// serverCopy downloads (as a database row) an episode onto the server and
// returns the media id of the copy.
func (f *syncFixture) serverCopy(setID, episodeID int64, name string) int64 {
	f.t.Helper()
	ctx := context.Background()
	mediaID, err := f.s.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "Show/" + name, FileName: name,
		AbsPath: "/nowhere/" + name, Type: model.MediaTypeAudio, Duration: 600, CreatedAt: f.now})
	f.must(err)
	f.must(f.s.UpdateEpisodeMedia(ctx, episodeID, mediaID, name))
	return mediaID
}

func (f *syncFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

// posItem is a position-only episode item.
func posItem(id, feed, ep string, seconds float64, ms int64) model.PodcastEpisodeEdit {
	return model.PodcastEpisodeEdit{FeedKey: feed, EpisodeKey: ep,
		Position: &model.PodcastPositionEdit{EditID: fmt.Sprintf("pos-%012s", id), Seconds: seconds, ObservedMS: ms}}
}

// compItem is a completion-only episode item.
func compItem(id, feed, ep string, completed bool, source string, ms int64) model.PodcastEpisodeEdit {
	return model.PodcastEpisodeEdit{FeedKey: feed, EpisodeKey: ep,
		Completion: &model.PodcastCompletionEdit{EditID: fmt.Sprintf("cmp-%012s", id), Completed: completed, Source: source, ObservedMS: ms}}
}

// episodes runs one request carrying episode items, without a cursor.
func (f *syncFixture) episodes(user int64, items ...model.PodcastEpisodeEdit) *model.PodcastSyncResponse {
	f.t.Helper()
	out, err := f.s.SyncPodcast(context.Background(), user, model.PodcastSyncBatch{Episodes: items, Now: f.now})
	f.must(err)
	return out
}

// state returns the stored registers of an episode and its change seq.
func (f *syncFixture) state(user int64, feed, ep string) (episodeRegisters, int64) {
	f.t.Helper()
	r, _, err := loadRegisters(context.Background(), f.s.db, episodeStateKey{user, feed, ep})
	f.must(err)
	var seq int64
	_ = f.s.db.QueryRow(`SELECT change_seq FROM podcast_episode_state WHERE user_id = ? AND feed_key = ? AND episode_key = ?`,
		user, feed, ep).Scan(&seq)
	return r, seq
}

// mirror returns the user's playback_progress row of a media item.
func (f *syncFixture) mirror(user, mediaID int64) *model.PlaybackProgress {
	f.t.Helper()
	p, err := getProgress(context.Background(), f.s.db, user, mediaID)
	f.must(err)
	return p
}

func TestEpisodeSyncRegistersMergeIndependently(t *testing.T) {
	f := newSyncFixture(t)
	key := "guid:solo"
	out := f.episodes(f.alice, posItem("p1", testFeedKey, key, 100, 2000), compItem("c1", testFeedKey, key, true, "manual", 1000))
	if len(out.Acked) != 2 || len(out.Episodes) != 1 || out.Episodes[0].Position.Seconds != 100 || !out.Episodes[0].Completion.Completed {
		t.Fatalf("first edits: %+v", out)
	}
	_, seq := f.state(f.alice, testFeedKey, key)
	// A retry (equal clock) and a delayed older edit are acked, change
	// nothing and take no new change sequence number.
	out = f.episodes(f.alice, posItem("p1", testFeedKey, key, 100, 2000), posItem("old", testFeedKey, key, 5, 1500))
	if r, s := f.state(f.alice, testFeedKey, key); len(out.Acked) != 2 || r.position != 100 || s != seq {
		t.Fatalf("stale edits applied: %+v seq %d→%d", r, seq, s)
	}
	// An equal clock from another device keeps the stored value.
	f.episodes(f.alice, compItem("tie", testFeedKey, key, false, "manual", 1000))
	if r, _ := f.state(f.alice, testFeedKey, key); !r.completed {
		t.Fatal("a tie replaced the stored completion")
	}
	// Uncompletion keeps the end position; a later position keeps the
	// completion register (§4 rule 5).
	f.episodes(f.alice, compItem("un", testFeedKey, key, false, "manual", 3000))
	if r, _ := f.state(f.alice, testFeedKey, key); r.completed || r.position != 100 || r.completionMS != 3000 {
		t.Fatalf("uncompletion: %+v", r)
	}
	f.episodes(f.alice, posItem("p2", testFeedKey, key, 0, 4000))
	if r, _ := f.state(f.alice, testFeedKey, key); r.completed || r.completionMS != 3000 || r.position != 0 {
		t.Fatalf("position edit touched completion: %+v", r)
	}
	// Bob's registers are his own.
	if r, _ := f.state(f.bob, testFeedKey, key); r != (episodeRegisters{}) {
		t.Fatalf("state leaked to bob: %+v", r)
	}
}

func TestEpisodeSyncResolvesClientKeys(t *testing.T) {
	f := newSyncFixture(t)
	f.catalogEpisodes()
	f.must(execSQL(f, `INSERT INTO podcast_feed_aliases (alias_key, feed_key, change_seq) VALUES ('feed:old.example.com/rss', ?, 0)`, testFeedKey))
	// An enclosure key of a GUID episode resolves to it; an aliased feed
	// key to its canonical key.
	byEnc := posItem("enc", "feed:old.example.com/rss", "enc:cdn.example.com/1.mp3", 10, 1000)
	// An unknown GUID sharing ep1's enclosure stays a state-only key: client
	// evidence never links two GUIDs (contract §3.3, §11 case d).
	stateOnly := posItem("guid", testFeedKey, "guid:other", 20, 1000)
	stateOnly.EnclosureURL = "https://cdn.example.com/1.mp3"
	out := f.episodes(f.alice, byEnc, stateOnly, posItem("legacy", testFeedKey, "legacy-episode:999", 1, 1000))
	want := []model.PodcastKeyMapping{
		{Kind: "feed", From: "feed:old.example.com/rss", To: testFeedKey},
		{Kind: "episode", FeedKey: testFeedKey, From: "enc:cdn.example.com/1.mp3", To: testEp1Key},
	}
	if fmt.Sprint(out.Resolved) != fmt.Sprint(want) {
		t.Fatalf("resolved = %+v, want %+v", out.Resolved, want)
	}
	if len(out.Rejected) != 1 || out.Rejected[0].Reason != model.PodcastRejectInvalidKey || out.Rejected[0].EditID != "pos-000000legacy" {
		t.Fatalf("unknown legacy key: %+v", out.Rejected)
	}
	if r, _ := f.state(f.alice, testFeedKey, testEp1Key); r.position != 10 {
		t.Fatalf("resolved edit not stored under the canonical key: %+v", r)
	}
	if r, _ := f.state(f.alice, testFeedKey, "guid:other"); r.position != 20 {
		t.Fatalf("state-only key not kept: %+v", r)
	}
	// A known legacy key (here: the episode's alias) is accepted.
	f.must(execSQL(f, `INSERT INTO podcast_episode_aliases (feed_key, alias_key, episode_key, change_seq) VALUES (?, 'legacy-episode:5', ?, 0)`, testFeedKey, testEp2Key))
	out = f.episodes(f.alice, posItem("legacy5", testFeedKey, "legacy-episode:5", 3, 1000))
	if r, _ := f.state(f.alice, testFeedKey, testEp2Key); len(out.Acked) != 1 || r.position != 3 {
		t.Fatalf("known legacy key: %+v %+v", out, r)
	}
}

func execSQL(f *syncFixture, query string, args ...any) error {
	_, err := f.s.db.Exec(query, args...)
	return err
}

func TestEpisodeSyncQuotaRejectsNewRowsOnly(t *testing.T) {
	f := newSyncFixture(t)
	f.must(execSQL(f, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
INSERT INTO podcast_episode_state (user_id, feed_key, episode_key, position_ms, change_seq)
SELECT ?, 'feed:q.example.com/rss', 'guid:' || i, 1, 0 FROM n`, MaxPodcastEpisodeStatesPerUser, f.alice))
	item := posItem("new", testFeedKey, "guid:new", 1, 5)
	item.Completion = &model.PodcastCompletionEdit{EditID: "cmp-new-new-new-", Completed: true, Source: "auto", ObservedMS: 5}
	out := f.episodes(f.alice, item, posItem("old", "feed:q.example.com/rss", "guid:1", 1, 5))
	if len(out.Rejected) != 2 || out.Rejected[0].Reason != model.PodcastRejectQuota || len(out.Acked) != 1 {
		t.Fatalf("quota: %+v", out)
	}
	if out := f.episodes(f.bob, item); len(out.Acked) != 2 {
		t.Fatalf("bob blocked by alice's quota: %+v", out)
	}
}

func TestEpisodeSyncScopeReturnsOneEpisodeWithoutCursor(t *testing.T) {
	f := newSyncFixture(t)
	f.catalogEpisodes()
	f.episodes(f.alice, posItem("a", testFeedKey, testEp1Key, 30, 1000), posItem("b", testFeedKey, testEp2Key, 40, 1000))
	scope := &model.PodcastSyncScope{FeedKey: testFeedKey, EpisodeKey: "enc:cdn.example.com/1.mp3"}
	out, err := f.s.SyncPodcast(context.Background(), f.alice, model.PodcastSyncBatch{Scope: scope, Now: f.now})
	f.must(err)
	if out.NextSince != nil || out.HasMore != nil || len(out.Subscriptions) != 0 {
		t.Fatalf("scoped answer has cursor fields or other rows: %+v", out)
	}
	if len(out.Episodes) != 1 || out.Episodes[0].EpisodeKey != testEp1Key || out.Episodes[0].Position.Seconds != 30 {
		t.Fatalf("scoped episode: %+v", out.Episodes)
	}
	if len(out.Resolved) != 1 || out.Resolved[0].To != testEp1Key {
		t.Fatalf("scope resolution not reported: %+v", out.Resolved)
	}
	// Another user's scope sees nothing of alice's state.
	out, err = f.s.SyncPodcast(context.Background(), f.bob, model.PodcastSyncBatch{Scope: scope, Now: f.now})
	f.must(err)
	if len(out.Episodes) != 0 {
		t.Fatalf("bob's scope: %+v", out.Episodes)
	}
}

func TestEpisodeSyncPullPagesEpisodeRowsWithAliases(t *testing.T) {
	f := newSyncFixture(t)
	f.catalogEpisodes()
	f.must(execSQL(f, `INSERT INTO podcast_episode_aliases (feed_key, alias_key, episode_key, change_seq) VALUES (?, 'guid:ep1-old', ?, 0)`, testFeedKey, testEp1Key))
	f.episodes(f.alice, posItem("a", testFeedKey, testEp1Key, 1, 10), posItem("b", testFeedKey, testEp2Key, 2, 10),
		compItem("c", testFeedKey, "guid:solo", true, "manual", 10))
	first := f.sync(f.alice, 0, 2)
	if len(first.Episodes) != 2 || !*first.HasMore || len(first.Aliases) != 1 || first.Aliases[0].From != "guid:ep1-old" {
		t.Fatalf("first page: %+v", first)
	}
	second := f.sync(f.alice, *first.NextSince, 2)
	if len(second.Episodes) != 1 || *second.HasMore || second.Episodes[0].EpisodeKey != "guid:solo" ||
		second.Episodes[0].Position != nil || second.Episodes[0].Completion.Source != "manual" {
		t.Fatalf("second page: %+v", second)
	}
	if out := f.sync(f.bob, 0, 0); len(out.Episodes) != 0 || len(out.Aliases) != 0 {
		t.Fatalf("alice's episodes leaked to bob: %+v", out)
	}
}

func TestEpisodeSyncRewritesServerCopyMirror(t *testing.T) {
	f := newSyncFixture(t)
	setID, ep1, _ := f.catalogEpisodes()
	mediaID := f.serverCopy(setID, ep1, "1.mp3")
	f.must(execSQL(f, `INSERT INTO playback_progress (user_id, media_id, position_seconds, accumulated_seconds, finished, updated_at)
VALUES (?, ?, 5, 90, 0, ?)`, f.alice, mediaID, f.now))
	end := posItem("end", testFeedKey, testEp1Key, 598, 2000)
	end.Completion = &model.PodcastCompletionEdit{EditID: "cmp-end-end-end-", Completed: true, Source: "auto", ObservedMS: 2000}
	f.episodes(f.alice, end)
	m := f.mirror(f.alice, mediaID)
	if m.PositionSeconds != 598 || !m.Finished || m.AccumulatedSeconds != 90 || m.UpdatedAt.UnixMilli() != 2000 {
		t.Fatalf("mirror after the end: %+v", m)
	}
	// Replaying a completed episode: a later position projects unfinished,
	// so old clients resume it and it stays In progress.
	f.episodes(f.alice, posItem("replay", testFeedKey, testEp1Key, 30, 3000))
	if m := f.mirror(f.alice, mediaID); m.PositionSeconds != 30 || m.Finished || m.AccumulatedSeconds != 90 {
		t.Fatalf("mirror of a replay: %+v", m)
	}
	if r, _ := f.state(f.alice, testFeedKey, testEp1Key); !r.completed {
		t.Fatal("replay uncompleted the episode")
	}
	if inProgress, err := f.s.ListInProgressMedia(context.Background(), f.alice, MediaFilter{}); err != nil || len(inProgress) != 1 {
		t.Fatalf("replayed episode not In progress: %+v %v", inProgress, err)
	}
	// Bob has no state: no mirror is written for him.
	if m := f.mirror(f.bob, mediaID); m != nil {
		t.Fatalf("mirror for a user without state: %+v", m)
	}
}
