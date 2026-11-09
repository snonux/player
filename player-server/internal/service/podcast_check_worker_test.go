package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

type dueFeedsStore struct {
	*repository.SQLite
	feeds []model.PodcastFeed
}

func (d dueFeedsStore) ListFeedsNeedingCheck(context.Context, time.Time, time.Time) ([]model.PodcastFeed, error) {
	return d.feeds, nil
}

// B12: one CheckFeeds run fetches at most maxConcurrentFeedChecks feeds at
// once, and skips a feed a manual refresh holds.
func TestCheckFeeds_BoundedConcurrency(t *testing.T) {
	f := newCheckerFixture(t)
	var inFlight, peak, fetched atomic.Int32
	slow := func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		fetched.Add(1)
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>t</title></channel></rss>`))
	}
	var feeds []model.PodcastFeed
	for i := 0; i < 20; i++ {
		path := "/slow/" + string(rune('a'+i))
		f.routes[path] = slow
		feeds = append(feeds, *f.feed(t, path, nil))
	}
	f.svc.store = dueFeedsStore{SQLite: f.store, feeds: feeds}
	held := feeds[0].ID
	f.svc.refreshing[held] = true
	if err := f.svc.CheckFeeds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p := peak.Load(); p > maxConcurrentFeedChecks || p < 2 {
		t.Fatalf("peak concurrency %d, want 2..%d", p, maxConcurrentFeedChecks)
	}
	if n := fetched.Load(); n != 19 {
		t.Fatalf("fetched %d feeds, want 19 (one held by a manual refresh)", n)
	}
	if len(f.svc.refreshing) != 1 {
		t.Fatalf("claims left behind: %v", f.svc.refreshing)
	}
}

// B12: Stop cancels a running check (its fetch sees the cancelled
// context) and returns only after the check has finished.
func TestPodcastCheckWorker_StopCancelsAndWaits(t *testing.T) {
	f := newCheckerFixture(t)
	started := make(chan struct{})
	var cancelled atomic.Bool
	f.routes["/block"] = func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		cancelled.Store(true)
	}
	f.svc.store = dueFeedsStore{SQLite: f.store, feeds: []model.PodcastFeed{*f.feed(t, "/block", nil)}}
	var returned atomic.Bool
	w := NewPodcastCheckWorker(context.Background(), checkerProbe{f.svc, &returned}, time.Hour, time.Millisecond, nil)
	w.Start()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first check did not start shortly after Start")
	}
	w.Stop()
	if !returned.Load() {
		t.Fatal("Stop returned before CheckFeeds")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !cancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !cancelled.Load() {
		t.Fatal("in-flight fetch was not cancelled")
	}
}

type checkerProbe struct {
	FeedChecker
	returned *atomic.Bool
}

func (c checkerProbe) CheckFeeds(ctx context.Context) error {
	defer c.returned.Store(true)
	return c.FeedChecker.CheckFeeds(ctx)
}

// B18: the legacy per-set listing shows an episode once after a merge.
func TestListEpisodes_NoDuplicatesAfterMerge(t *testing.T) {
	f := newCheckerFixture(t)
	ctx := context.Background()
	if err := f.store.GrantPermission(ctx, &model.SetPermission{SetID: f.setID, UserID: f.admin, Role: model.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	var feeds []*model.PodcastFeed
	for _, path := range []string{"/old", "/new"} {
		feed := f.feed(t, path, nil)
		if _, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed.ID, GUID: "shared", Title: "shared",
			EpisodeURL: "https://cdn.example.com/shared.mp3", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		feeds = append(feeds, feed)
	}
	if move, err := f.store.MoveFeed(ctx, feeds[0].ID, feeds[1].FeedURL); err != nil || move != repository.FeedMerged {
		t.Fatalf("merge: %v %v", move, err)
	}
	eps, err := f.svc.ListEpisodes(ctx, f.setID, f.admin, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].FeedID != feeds[1].ID {
		t.Fatalf("episodes after merge: %+v", eps)
	}
}
