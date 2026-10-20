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

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
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
		{"episodes not yet", "alice", `{"episodes": [{"feed_key": "feed:a/"}]}`, http.StatusBadRequest},
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
