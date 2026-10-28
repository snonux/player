package app

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestPodcastLibraryFirstSubscribeHTTP(t *testing.T) {
	for _, path := range []string{"/api/podcasts", "/api/v1/podcasts"} {
		t.Run(path, func(t *testing.T) {
			e := newE2E(t, &fakeRunner{})
			fx := newPodcastFixture(t)
			ctx := context.Background()
			libs, err := e.store.ListMusicLibraries(ctx)
			e.must(err)
			ordinary, err := e.store.CreateSet(ctx, &model.Set{LibraryID: libs[0].ID, Name: "Podcasts", RootPath: "Podcasts", CreatedAt: time.Now()})
			e.must(err)
			// The first subscription comes from an ordinary account's device.
			e.podcastSync(path+"/sync", "alice", model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{subscribeEdit("library", fx.feedKey("show"), fx.url("show"), true, time.Now().UnixMilli())}})
			// Admin subscribe must reuse that catalog and materialize its directory.
			for range 2 {
				rr := e.request(ctx, http.MethodPost, path, "admin", fmt.Sprintf(`{"feed_url":%q}`, fx.url("show")))
				if rr.Code != http.StatusOK {
					t.Fatalf("subscribe status %d: %s", rr.Code, rr.Body)
				}
			}
			feeds, err := e.store.ListFeeds(ctx)
			e.must(err)
			if len(feeds) != 1 {
				t.Fatalf("feeds: %+v", feeds)
			}
			set, err := e.store.GetSetByID(ctx, feeds[0].SetID)
			e.must(err)
			if set.LibraryID != libs[0].ID || !set.IsPodcast || set.ID == ordinary {
				t.Fatalf("managed set: %+v", set)
			}
			preserved, err := e.store.GetSetByID(ctx, ordinary)
			e.must(err)
			if preserved.IsPodcast || preserved.RootPath != "Podcasts" {
				t.Fatalf("ordinary set changed: %+v", preserved)
			}
			if fx.audioHits.Load() != 0 {
				t.Fatal("subscribe downloaded audio")
			}
		})
	}
}
