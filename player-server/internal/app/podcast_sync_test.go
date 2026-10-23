package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// podcastFixture serves RSS documents whose enclosures point back at the
// fixture, and counts every audio request: synchronization must never make
// the server download episode audio.
type podcastFixture struct {
	srv       *httptest.Server
	audioHits atomic.Int64
	feedHits  atomic.Int64
}

func newPodcastFixture(t *testing.T) *podcastFixture {
	t.Helper()
	f := &podcastFixture{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/audio/") {
			f.audioHits.Add(1)
			_, _ = w.Write([]byte("ID3 not really audio"))
			return
		}
		f.feedHits.Add(1)
		name := strings.TrimPrefix(r.URL.Path, "/")
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>%[1]s</title>
<item><title>One</title><guid>%[1]s-1</guid><enclosure url="%[2]s/audio/%[1]s-1.mp3" type="audio/mpeg" length="20"/></item>
<item><title>Two</title><enclosure url="%[2]s/audio/%[1]s-2.mp3" type="audio/mpeg" length="20"/></item>
</channel></rss>`, name, f.srv.URL)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *podcastFixture) url(name string) string { return f.srv.URL + "/" + name }

// feedKey is the identity of a fixture feed URL.
func (f *podcastFixture) feedKey(name string) string {
	return "feed:" + strings.TrimPrefix(f.url(name), "http://")
}

// podcastSync posts a sync request as user and decodes the 200 answer.
func (e *e2e) podcastSync(path, user string, req model.PodcastSyncRequest) model.PodcastSyncResponse {
	e.t.Helper()
	body, err := json.Marshal(req)
	e.must(err)
	rr := e.request(context.Background(), http.MethodPost, path, user, string(body))
	if rr.Code != http.StatusOK {
		e.t.Fatalf("sync %s as %s = %d: %s", path, user, rr.Code, rr.Body)
	}
	var out model.PodcastSyncResponse
	e.must(json.Unmarshal(rr.Body.Bytes(), &out))
	return out
}

func subscribeEdit(id, key, url string, subscribed bool, ms int64) model.PodcastSubscriptionEdit {
	return model.PodcastSubscriptionEdit{EditID: fmt.Sprintf("edit-id-%010s", id), FeedKey: key,
		FetchURL: url, URLs: []string{url}, Subscribed: subscribed, ObservedMS: ms}
}

func TestPodcastSyncHTTPRoutesAndAccountIsolation(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	alice := syncDeviceToken(t, e, "alice", "phone")
	edit := subscribeEdit("a", "feed:example.com/rss", "https://example.com/rss", false, 1000)
	rr := syncDeviceRequest(t, e.server, alice, http.MethodPost, "/api/v1/podcasts/sync",
		model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{edit}})
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), edit.EditID) {
		t.Fatalf("bearer sync = %d: %s", rr.Code, rr.Body)
	}
	// The legacy prefix is the same endpoint; bob sees nothing of alice's.
	got := e.podcastSync("/api/podcasts/sync", "bob", model.PodcastSyncRequest{})
	if len(got.Subscriptions) != 0 || got.Epoch == "" || got.Acked == nil {
		t.Fatalf("bob's pull: %+v", got)
	}
	got = e.podcastSync("/api/podcasts/sync", "alice", model.PodcastSyncRequest{})
	if len(got.Subscriptions) != 1 || got.Subscriptions[0].Subscribed {
		t.Fatalf("alice's pull: %+v", got)
	}
	cases := []struct {
		name, user, body string
		want             int
	}{
		{"anonymous", "", `{}`, http.StatusUnauthorized},
		{"malformed", "alice", `{"subscriptions": 3}`, http.StatusBadRequest},
		{"limit", "alice", `{"limit": 5000}`, http.StatusBadRequest},
		{"episode item without edits", "alice", `{"episodes": [{"feed_key": "feed:a/", "episode_key": "guid:x"}]}`, http.StatusBadRequest},
		{"scope and cursor", "alice", `{"since": 0, "scope": {"feed_key": "feed:a/", "episode_key": "guid:x"}}`, http.StatusBadRequest},
		{"too large", "alice", `{"epoch": "` + strings.Repeat("x", 2<<20) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		rr := e.request(context.Background(), http.MethodPost, "/api/v1/podcasts/sync", tc.user, tc.body)
		if rr.Code != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", tc.name, rr.Code, tc.want, rr.Body)
		}
	}
	// "sync" is never taken for a feed id by the neighbouring routes.
	if rr := e.request(context.Background(), http.MethodDelete, "/api/podcasts/sync", "admin", ""); rr.Code != http.StatusBadRequest {
		t.Errorf("DELETE /api/podcasts/sync = %d, want 400 (invalid feed id)", rr.Code)
	}
}

func TestPodcastLegacySubscribeWritesRegisterAndSyncNeverDownloadsAudio(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	fx := newPodcastFixture(t)
	ctx := context.Background()

	rr := e.request(ctx, http.MethodPost, "/api/podcasts", "admin", fmt.Sprintf(`{"feed_url": %q}`, fx.url("legacy")))
	if rr.Code != http.StatusOK {
		t.Fatalf("legacy subscribe = %d: %s", rr.Code, rr.Body)
	}
	var feeds []model.PodcastFeed
	e.must(json.Unmarshal(e.get("/api/v1/podcasts", "admin").Body.Bytes(), &feeds))
	if len(feeds) != 1 || feeds[0].FeedKey != fx.feedKey("legacy") || !feeds[0].Subscribed {
		t.Fatalf("legacy listing: %+v", feeds)
	}
	pulled := e.podcastSync("/api/v1/podcasts/sync", "admin", model.PodcastSyncRequest{})
	if len(pulled.Subscriptions) != 1 || pulled.Subscriptions[0].FeedKey != fx.feedKey("legacy") {
		t.Fatalf("legacy subscribe not synchronized: %+v", pulled.Subscriptions)
	}

	// A sync subscription to an unknown feed creates a catalog row that
	// the checker fetches: metadata and episodes, never audio.
	key := fx.feedKey("synced")
	e.podcastSync("/api/v1/podcasts/sync", "admin", model.PodcastSyncRequest{
		Subscriptions: []model.PodcastSubscriptionEdit{subscribeEdit("s", key, fx.url("synced"), true, time.Now().UnixMilli())}})
	before := fx.feedHits.Load()
	e.must(e.deps.PodcastSvc.CheckFeeds(ctx))
	if fx.feedHits.Load() == before {
		t.Fatal("the checker did not fetch the synced feed")
	}
	synced, err := e.store.GetFeedByKey(ctx, key)
	if err != nil || synced == nil || synced.Title != "synced" {
		t.Fatalf("synced catalog feed: %+v %v", synced, err)
	}
	eps, err := e.store.ListEpisodesByFeed(ctx, synced.ID, 10, 0)
	if err != nil || len(eps) != 2 {
		t.Fatalf("synced episodes: %d %v", len(eps), err)
	}
	if hits := fx.audioHits.Load(); hits != 0 {
		t.Fatalf("server downloaded episode audio %d time(s)", hits)
	}
	media, err := e.store.ListMedia(ctx, repository.MediaFilter{SetID: &synced.SetID, IncludeDeleted: true})
	if err != nil || len(media) != 0 {
		t.Fatalf("media rows in the podcast set: %d %v", len(media), err)
	}
}

func TestPodcastRemoveFromServerKeepsSubscribedFeed(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	fx := newPodcastFixture(t)
	ctx := context.Background()
	if rr := e.request(ctx, http.MethodPost, "/api/podcasts", "admin", fmt.Sprintf(`{"feed_url": %q}`, fx.url("show"))); rr.Code != http.StatusOK {
		t.Fatalf("subscribe = %d: %s", rr.Code, rr.Body)
	}
	feed, err := e.store.GetFeedByKey(ctx, fx.feedKey("show"))
	e.must(err)
	del := fmt.Sprintf("/api/v1/podcasts/%d", feed.ID)
	if rr := e.request(ctx, http.MethodDelete, del, "admin", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("remove = %d: %s", rr.Code, rr.Body)
	}
	if kept, _ := e.store.GetFeedByID(ctx, feed.ID); kept == nil {
		t.Fatal("feed with an active subscriber was deleted")
	}
	pulled := e.podcastSync("/api/v1/podcasts/sync", "admin", model.PodcastSyncRequest{})
	if len(pulled.Subscriptions) != 1 || !pulled.Subscriptions[0].Subscribed {
		t.Fatalf("remove from server touched the subscription: %+v", pulled.Subscriptions)
	}
	// After the admin unsubscribes, removing deletes the catalog row.
	e.podcastSync("/api/v1/podcasts/sync", "admin", model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{
		subscribeEdit("u", fx.feedKey("show"), fx.url("show"), false, time.Now().Add(time.Second).UnixMilli())}})
	if rr := e.request(ctx, http.MethodDelete, del, "admin", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("second remove = %d: %s", rr.Code, rr.Body)
	}
	if gone, _ := e.store.GetFeedByID(ctx, feed.ID); gone != nil {
		t.Fatal("feed without subscribers kept")
	}
	pulled = e.podcastSync("/api/v1/podcasts/sync", "admin", model.PodcastSyncRequest{})
	if len(pulled.Subscriptions) != 1 || pulled.Subscriptions[0].Subscribed {
		t.Fatalf("tombstone lost with the catalog row: %+v", pulled.Subscriptions)
	}
}

// linkedEpisode creates a catalog feed with one episode whose server copy
// is a media row alice may read, and returns the feed key, episode id and
// media id.
func (e *e2e) linkedEpisode(t *testing.T) (string, int64, int64) {
	t.Helper()
	ctx, now := context.Background(), time.Now()
	setID, err := e.store.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: now})
	e.must(err)
	e.must(e.store.GrantPermission(ctx, &model.SetPermission{SetID: setID, UserID: e.users["alice"], Role: model.RoleViewer, CreatedAt: now}))
	feed := &model.PodcastFeed{SetID: setID, FeedURL: "https://example.com/rss", Title: "Show", CheckIntervalMinutes: 60, CreatedAt: now}
	feedID, err := e.store.CreateFeed(ctx, feed)
	e.must(err)
	epID, err := e.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: "ep1", EpisodeURL: "https://cdn.example.com/1.mp3", CreatedAt: now})
	e.must(err)
	mediaID, err := e.store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "Show/1.mp3", FileName: "1.mp3", AbsPath: "/nowhere/1.mp3", Type: model.MediaTypeAudio, Duration: 600, CreatedAt: now})
	e.must(err)
	e.must(e.store.UpdateEpisodeMedia(ctx, epID, mediaID, "1.mp3"))
	return feed.FeedKey, epID, mediaID
}

// aliceEpisode pulls alice's registers of the linked episode.
func (e *e2e) aliceEpisode(t *testing.T, feedKey string) model.PodcastEpisodeState {
	t.Helper()
	out := e.podcastSync("/api/v1/podcasts/sync", "alice", model.PodcastSyncRequest{})
	for _, ep := range out.Episodes {
		if ep.FeedKey == feedKey && ep.EpisodeKey == "guid:ep1" {
			return ep
		}
	}
	t.Fatalf("no episode state pulled: %+v", out.Episodes)
	return model.PodcastEpisodeState{}
}

// TestLegacyProgressRoutesWriteEpisodeState covers the bridge of contract
// §7.1: every legacy write of an episode copy's progress or completion is an
// edit of the caller's episode registers, which the sync API returns.
func TestLegacyProgressRoutesWriteEpisodeState(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	key, epID, mediaID := e.linkedEpisode(t)
	post := func(path, body string) {
		t.Helper()
		if rr := e.request(ctx, http.MethodPost, path, "alice", body); rr.Code/100 != 2 {
			t.Fatalf("%s = %d: %s", path, rr.Code, rr.Body)
		}
	}
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	at := func(s int) string { return base.Add(time.Duration(s) * time.Second).Format(time.RFC3339Nano) }

	post("/api/v1/progress/batch", fmt.Sprintf(`{"updates":[{"media_id":%d,"position_seconds":120,"observed_at":%q}]}`, mediaID, at(1)))
	got := e.aliceEpisode(t, key)
	if got.Position == nil || got.Position.Seconds != 120 || got.Position.ObservedMS != base.Add(time.Second).UnixMilli() || got.Completion != nil {
		t.Fatalf("position save: %+v", got)
	}
	post("/api/v1/progress/status", fmt.Sprintf(`{"media_id":%d,"status":"finished","observed_at":%q}`, mediaID, at(2)))
	got = e.aliceEpisode(t, key)
	if got.Completion == nil || !got.Completion.Completed || got.Completion.Source != "manual" || got.Position.Seconds != 120 {
		t.Fatalf("manual finish must keep the position: %+v %+v", got.Completion, got.Position)
	}
	post("/api/v1/progress/status", fmt.Sprintf(`{"media_id":%d,"status":"not_started","observed_at":%q}`, mediaID, at(3)))
	got = e.aliceEpisode(t, key)
	if got.Position.Seconds != 0 || got.Completion.Completed || got.Completion.ObservedMS != base.Add(3*time.Second).UnixMilli() {
		t.Fatalf("reset: %+v %+v", got.Position, got.Completion)
	}
	post(fmt.Sprintf("/api/v1/media/%d/playback-sync", mediaID),
		fmt.Sprintf(`{"progress":{"position_seconds":598,"finished":true,"observed_ms":%d},"bookmarks":[]}`, base.Add(4*time.Second).UnixMilli()))
	got = e.aliceEpisode(t, key)
	if !got.Completion.Completed || got.Completion.Source != "auto" || got.Position.Seconds != 598 {
		t.Fatalf("natural end: %+v %+v", got.Position, got.Completion)
	}
	// The legacy toggle flips the completion register (completed by the
	// natural end above) by a manual edit at server time.
	post(fmt.Sprintf("/api/v1/podcasts/episodes/%d/complete", epID), "")
	got = e.aliceEpisode(t, key)
	if got.Completion.Completed || got.Completion.Source != "manual" || got.Completion.ObservedMS < base.Add(time.Hour-time.Minute).UnixMilli() ||
		got.Position.Seconds != 598 {
		t.Fatalf("toggle: %+v %+v", got.Completion, got.Position)
	}
	post(fmt.Sprintf("/api/v1/podcasts/episodes/%d/complete", epID), "")
	if got = e.aliceEpisode(t, key); !got.Completion.Completed {
		t.Fatalf("second toggle: %+v", got.Completion)
	}
	// Progress of ordinary media creates no episode state.
	post("/api/v1/progress", fmt.Sprintf(`{"media_id":%d,"position_seconds":5}`, e.media["movie.mp4"]))
	if out := e.podcastSync("/api/v1/podcasts/sync", "alice", model.PodcastSyncRequest{}); len(out.Episodes) != 1 {
		t.Fatalf("non-podcast progress produced episode state: %+v", out.Episodes)
	}
}
