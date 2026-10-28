package service

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func refreshSubscription(t *testing.T, f *checkerFixture, path string) *model.PodcastFeed {
	t.Helper()
	feed := f.feed(t, path, &f.admin)
	if err := f.store.SetPodcastSubscription(context.Background(), f.user, feed, true, f.svc.clock.Now()); err != nil {
		t.Fatal(err)
	}
	return feed
}
func allowRefreshFixture(f *checkerFixture) {
	// Controlled publisher exception is test-only; production uses publicUnicast.
	f.svc.restrictedClient = newRestrictedClient(time.Second, func(netip.Addr) bool { return true }, maxRestrictedFeedBytes)
}
func TestSubscriptionRefreshIngestOwnershipAndFreshness(t *testing.T) {
	f := newCheckerFixture(t)
	allowRefreshFixture(f)
	feed := refreshSubscription(t, f, "/rss")
	ctx := context.Background()
	if _, err := f.svc.RefreshSubscribedFeed(ctx, f.admin, feed.FeedKey); !errors.Is(err, ErrPodcastRefreshMissing) {
		t.Fatal(err)
	}
	if _, err := f.svc.RefreshSubscribedFeed(ctx, f.user, "https://arbitrary.example/rss"); !errors.Is(err, ErrPodcastRefreshInvalid) {
		t.Fatal(err)
	}
	out, err := f.svc.RefreshSubscribedFeed(ctx, f.user, feed.FeedKey)
	if err != nil || out.Status != "refreshed" {
		t.Fatalf("%+v %v", out, err)
	}
	eps, err := f.store.ListSubscribedEpisodes(ctx, f.user, repository.PodcastEpisodeQuery{FeedKey: feed.FeedKey, Limit: 10})
	if err != nil || len(eps) != 1 {
		t.Fatalf("episodes=%+v err=%v", eps, err)
	}
	permission, err := f.store.GetPermission(ctx, f.setID, f.user)
	if err != nil || permission != nil {
		t.Fatalf("unexpected set grant: %+v %v", permission, err)
	}
	if f.audio.Load() != 0 {
		t.Fatal("downloaded enclosure")
	}
	out, err = f.svc.RefreshSubscribedFeed(ctx, f.user, feed.FeedKey)
	if err != nil || out.Status != "fresh" || f.feeds.Load() != 1 {
		t.Fatalf("fresh %+v %v", out, err)
	}
	if err := f.store.SetPodcastSubscription(ctx, f.user, feed, false, f.svc.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RefreshSubscribedFeed(ctx, f.user, feed.FeedKey); !errors.Is(err, ErrPodcastRefreshMissing) {
		t.Fatal(err)
	}
}
func TestSubscriptionRefreshBlocksPrivateAdminCatalog(t *testing.T) {
	f := newCheckerFixture(t)
	feed := refreshSubscription(t, f, "/private")
	if _, err := f.svc.RefreshSubscribedFeed(context.Background(), f.user, feed.FeedKey); !errors.Is(err, ErrPodcastRefreshFailed) {
		t.Fatal(err)
	}
	if f.feeds.Load() != 0 {
		t.Fatal("private publisher reached")
	}
	if _, err := f.svc.RefreshSubscribedFeed(context.Background(), f.user, feed.FeedKey); !errors.Is(err, ErrPodcastRefreshBusy) {
		t.Fatalf("backoff: %v", err)
	}
}
func TestSubscriptionRefreshMalformedAndAlias(t *testing.T) {
	f := newCheckerFixture(t)
	allowRefreshFixture(f)
	ctx := context.Background()
	bad := refreshSubscription(t, f, "/bad")
	f.routes["/bad"] = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not RSS")) }
	if _, err := f.svc.RefreshSubscribedFeed(ctx, f.user, bad.FeedKey); !errors.Is(err, ErrPodcastRefreshFailed) {
		t.Fatal(err)
	}
	if f.reload(t, bad.ID).NextCheckAt == nil {
		t.Fatal("missing backoff")
	}
	good := refreshSubscription(t, f, "/good")
	if _, err := f.store.MoveFeed(ctx, good.ID, f.srv.URL+"/alias"); err != nil {
		t.Fatal(err)
	}
	out, err := f.svc.RefreshSubscribedFeed(ctx, f.user, "feed:"+hostForURL(f.srv.URL)+"/alias")
	if err != nil || out.FeedKey != good.FeedKey {
		t.Fatalf("alias %+v %v", out, err)
	}
}
func TestSubscriptionRefreshBoundsAndCancellation(t *testing.T) {
	f := newCheckerFixture(t)
	allowRefreshFixture(f)
	started := make(chan struct{}, 3)
	handler := func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-r.Context().Done() }
	feeds := []*model.PodcastFeed{}
	for _, path := range []string{"/one", "/two", "/three"} {
		f.routes[path] = handler
		feeds = append(feeds, refreshSubscription(t, f, path))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	for _, feed := range feeds[:2] {
		go func(key string) { _, err := f.svc.RefreshSubscribedFeed(ctx, f.user, key); done <- err }(feed.FeedKey)
		<-started
	}
	for _, feed := range []*model.PodcastFeed{feeds[0], feeds[2]} {
		if _, err := f.svc.RefreshSubscribedFeed(context.Background(), f.user, feed.FeedKey); !errors.Is(err, ErrPodcastRefreshBusy) {
			t.Fatal(err)
		}
	}
	cancel()
	for range 2 {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("refresh did not cancel")
		}
	}
	f.svc.refreshMu.Lock()
	active := len(f.svc.refreshing)
	f.svc.refreshMu.Unlock()
	if active != 0 {
		t.Fatalf("slots=%d", active)
	}
}

func TestSubscriptionRefreshMergedEmptyWinner(t *testing.T) {
	f := newCheckerFixture(t)
	allowRefreshFixture(f)
	ctx := context.Background()
	source := refreshSubscription(t, f, "/source")
	winner := f.feed(t, "/winner", &f.admin)
	f.routes["/source"] = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, f.srv.URL+"/winner", http.StatusMovedPermanently)
	}
	out, err := f.svc.RefreshSubscribedFeed(ctx, f.user, source.FeedKey)
	if err != nil || out.FeedKey != winner.FeedKey {
		t.Fatalf("merge %+v %v", out, err)
	}
	eps, err := f.store.ListSubscribedEpisodes(ctx, f.user, repository.PodcastEpisodeQuery{FeedKey: winner.FeedKey, Limit: 10})
	if err != nil || len(eps) != 1 {
		t.Fatalf("merged winner episodes=%+v err=%v", eps, err)
	}
	if f.audio.Load() != 0 {
		t.Fatal("downloaded enclosure")
	}
}

// mergeOnLookupStore returns the first source snapshot and then commits the
// move before the refresh can reload it, reproducing a concurrent catalog move.
type mergeOnLookupStore struct {
	PodcastServiceStore
	sourceKey string
	sourceID  int64
	target    string
	moved     atomic.Bool
}

func (s *mergeOnLookupStore) GetSubscribedPodcastFeed(ctx context.Context, user int64, key string) (*model.PodcastFeed, error) {
	feed, err := s.PodcastServiceStore.GetSubscribedPodcastFeed(ctx, user, key)
	if err == nil && key == s.sourceKey && s.moved.CompareAndSwap(false, true) {
		_, err = s.PodcastServiceStore.MoveFeed(ctx, s.sourceID, s.target)
	}
	return feed, err
}
func TestSubscriptionRefreshMergeBeforeReloadPreservesDedup(t *testing.T) {
	f := newCheckerFixture(t)
	allowRefreshFixture(f)
	source := refreshSubscription(t, f, "/source-race")
	winner := refreshSubscription(t, f, "/winner-race")
	started := make(chan struct{}, 2)
	var requests atomic.Int64
	f.routes["/winner-race"] = func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		started <- struct{}{}
		<-r.Context().Done()
	}
	f.svc.store = &mergeOnLookupStore{PodcastServiceStore: f.store, sourceKey: source.FeedKey, sourceID: source.ID, target: winner.FeedURL}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.svc.RefreshSubscribedFeed(ctx, f.user, winner.FeedKey); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("winner did not start")
	}
	_, err := f.svc.RefreshSubscribedFeed(context.Background(), f.user, source.FeedKey)
	if !errors.Is(err, ErrPodcastRefreshBusy) {
		t.Fatalf("merged source refresh: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("duplicate winner requests: %d", requests.Load())
	}
	f.svc.refreshMu.Lock()
	winnerHeld, sourceHeld := f.svc.refreshing[winner.ID], f.svc.refreshing[source.ID]
	f.svc.refreshMu.Unlock()
	if !winnerHeld || sourceHeld {
		t.Fatalf("winner held=%v source held=%v", winnerHeld, sourceHeld)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("winner did not cancel")
	}
}

// failFirstIngestStore keeps all persistence real while failing just the
// document's first ingestion, before the SQLite transaction can commit.
type failFirstIngestStore struct {
	PodcastServiceStore
	failed        atomic.Bool
	cancel        context.CancelFunc
	sawValidators bool
}

func (s *failFirstIngestStore) IngestFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, episodes []model.PodcastEpisode, now time.Time) error {
	if s.failed.CompareAndSwap(false, true) {
		persisted, err := s.PodcastServiceStore.GetFeedByID(ctx, feed.ID)
		if err != nil {
			return err
		}
		s.sawValidators = persisted.LastETag != "" || persisted.LastCheckedAt != nil
		if s.cancel != nil {
			s.cancel()
			return ctx.Err()
		}
		return errors.New("injected one-time ingestion failure")
	}
	return s.PodcastServiceStore.IngestFeedEpisodes(ctx, feed, episodes, now)
}
func TestSubscriptionRefreshIngestFailureRetainsValidators(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "failure"
		if cancelled {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			f := newCheckerFixture(t)
			allowRefreshFixture(f)
			feed := refreshSubscription(t, f, "/conditional")
			var full, notModified atomic.Int64
			f.routes["/conditional"] = func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-None-Match") == `"document-v1"` || r.Header.Get("If-Modified-Since") != "" {
					notModified.Add(1)
					w.WriteHeader(http.StatusNotModified)
					return
				}
				full.Add(1)
				w.Header().Set("ETag", `"document-v1"`)
				_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Conditional feed</title><item><guid>conditional-one</guid><title>Episode</title><enclosure url="https://example.com/audio.mp3" type="audio/mpeg"/></item></channel></rss>`))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wrapper := &failFirstIngestStore{PodcastServiceStore: f.store}
			if cancelled {
				wrapper.cancel = cancel
			}
			f.svc.store = wrapper
			if _, err := f.svc.RefreshSubscribedFeed(ctx, f.user, feed.FeedKey); !errors.Is(err, ErrPodcastRefreshFailed) {
				t.Fatalf("first refresh %v", err)
			}
			stored := f.reload(t, feed.ID)
			if wrapper.sawValidators || stored.LastETag != "" || stored.LastCheckedAt != nil {
				t.Fatalf("accepted uncommitted validators: before=%v feed=%+v", wrapper.sawValidators, stored)
			}
			// A normal failure has persisted backoff; cancellation may prevent that
			// write too. Both paths must fetch the full document after retry is due.
			f.svc.clock.(*clock.MockClock).T = f.svc.clock.Now().Add(baseFeedRetryBackoff + time.Second)
			out, err := f.svc.RefreshSubscribedFeed(context.Background(), f.user, feed.FeedKey)
			if err != nil || out.Status != "refreshed" {
				t.Fatalf("retry %+v %v", out, err)
			}
			eps, err := f.store.ListSubscribedEpisodes(context.Background(), f.user, repository.PodcastEpisodeQuery{FeedKey: feed.FeedKey, Limit: 10})
			if err != nil || len(eps) != 1 || full.Load() != 2 || notModified.Load() != 0 {
				t.Fatalf("episodes=%+v err=%v full=%d 304=%d", eps, err, full.Load(), notModified.Load())
			}
			stored = f.reload(t, feed.ID)
			if stored.LastETag != `"document-v1"` || stored.LastCheckedAt == nil {
				t.Fatalf("missing committed validators: %+v", stored)
			}
			f.svc.clock.(*clock.MockClock).T = f.svc.clock.Now().Add(2 * time.Minute)
			if _, err := f.svc.RefreshSubscribedFeed(context.Background(), f.user, feed.FeedKey); err != nil {
				t.Fatal(err)
			}
			if notModified.Load() != 1 {
				t.Fatal("successful ingest did not enable conditional requests")
			}
		})
	}
}
