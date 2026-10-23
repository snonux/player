package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func TestPublicUnicast(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8": true, "2001:4860:4860::8888": true, "93.184.216.34": true,
		"127.0.0.1": false, "::1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "fe80::1": false, "fc00::1": false, "fd12::1": false, "224.0.0.1": false,
		"ff02::1": false, "0.0.0.0": false, "::": false, "100.64.0.1": false, "::ffff:127.0.0.1": false,
		"::ffff:8.8.8.8": true, "64:ff9b::a00:1": false, "198.18.0.1": false, "255.255.255.255": false,
		"240.0.0.1": false, "2002:a00:1::1": false,
	}
	for addr, want := range cases {
		if got := publicUnicast(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicUnicast(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestRestrictedClientRefusesLoopbackAndOddRedirects(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "ftp://example.com/feed", http.StatusFound)
	}))
	defer srv.Close()
	_, err := newRestrictedClient(time.Second, publicUnicast, 0).Get(srv.URL)
	if !errors.Is(err, errBlockedAddress) || hits.Load() != 0 {
		t.Fatalf("loopback fetch: err=%v hits=%d", err, hits.Load())
	}
	allowAll := func(netip.Addr) bool { return true }
	if _, err := newRestrictedClient(time.Second, allowAll, 0).Get(srv.URL); err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("redirect to ftp: %v", err)
	}
}

func TestRestrictedClientChecksEveryRedirectHop(t *testing.T) {
	// The second server stands for an internal address a public feed
	// redirects to (or a DNS answer that changed): only the first dial is
	// allowed, so the hop is refused before any byte reaches it.
	var internalHits atomic.Int64
	internal := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { internalHits.Add(1) }))
	defer internal.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/admin", http.StatusMovedPermanently)
	}))
	defer public.Close()
	var dials atomic.Int64
	firstOnly := func(netip.Addr) bool { return dials.Add(1) == 1 }
	_, err := newRestrictedClient(time.Second, firstOnly, 0).Get(public.URL)
	if !errors.Is(err, errBlockedAddress) || internalHits.Load() != 0 {
		t.Fatalf("redirect hop: err=%v internal hits=%d", err, internalHits.Load())
	}
}

func TestRestrictedClientCapsBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	allowAll := func(netip.Addr) bool { return true }
	read := func(max int64) error {
		resp, err := newRestrictedClient(time.Second, allowAll, max).Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		return err
	}
	if err := read(99); !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("over the cap: %v", err)
	}
	if err := read(100); err != nil {
		t.Fatalf("at the cap: %v", err)
	}
	if err := read(0); err != nil {
		t.Fatalf("uncapped: %v", err)
	}
}

// checkerFixture is a podcast service on a real store with an admin, a
// listener and a fixture HTTP server for feed fetches.
type checkerFixture struct {
	svc          *podcastService
	store        *repository.SQLite
	srv          *httptest.Server
	admin, user  int64
	setID        int64
	audio, feeds atomic.Int64
	routes       map[string]func(http.ResponseWriter, *http.Request)
}

func newCheckerFixture(t *testing.T) *checkerFixture {
	t.Helper()
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f := &checkerFixture{store: store, routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	f.admin, _ = store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "h", IsAdmin: true, CreatedAt: now})
	f.user, _ = store.CreateUser(ctx, &model.User{Username: "user", PasswordHash: "h", CreatedAt: now})
	f.setID, _ = store.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: now})
	f.svc = NewPodcastServiceWithLogger(store, &clock.MockClock{T: now}, t.TempDir(), NewAccessHelper(store), nil, nil,
		&http.Client{Timeout: 5 * time.Second}, 60, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.svc.fetchPolicy = FeedFetchPolicy{MaxAttempts: 1}
	return f
}

func (f *checkerFixture) serve(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/audio/") {
		f.audio.Add(1)
		return
	}
	if route := f.routes[r.URL.Path]; route != nil {
		route(w, r)
		return
	}
	f.feeds.Add(1)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel>
<title>%s</title><item><guid>one</guid><enclosure url="%s/audio/1.mp3" type="audio/mpeg"/></item></channel></rss>`, r.URL.Path, f.srv.URL)
}

// feed creates a catalog feed fetched from path, created by createdBy.
func (f *checkerFixture) feed(t *testing.T, path string, createdBy *int64) *model.PodcastFeed {
	t.Helper()
	feed := &model.PodcastFeed{SetID: f.setID, FeedURL: f.srv.URL + path, CreatedBy: createdBy, CheckIntervalMinutes: 60, CreatedAt: time.Now()}
	id, err := f.store.CreateFeed(context.Background(), feed)
	if err != nil {
		t.Fatal(err)
	}
	feed.ID = id
	return feed
}

func (f *checkerFixture) reload(t *testing.T, id int64) *model.PodcastFeed {
	t.Helper()
	feed, err := f.store.GetFeedByID(context.Background(), id)
	if err != nil || feed == nil {
		t.Fatalf("reload feed %d: %v", id, err)
	}
	return feed
}

func TestFeedClientChoice(t *testing.T) {
	f := newCheckerFixture(t)
	ctx := context.Background()
	missing := int64(999)
	cases := []struct {
		name      string
		createdBy *int64
		want      *http.Client
	}{
		{"admin legacy row", nil, f.svc.httpClient},
		{"admin sync row", &f.admin, f.svc.httpClient},
		{"user sync row", &f.user, f.svc.restrictedClient},
		{"deleted creator", &missing, f.svc.restrictedClient},
	}
	for _, tc := range cases {
		feed := &model.PodcastFeed{CreatedBy: tc.createdBy}
		if got := f.svc.feedClient(ctx, feed); got != tc.want {
			t.Errorf("%s: wrong feed client", tc.name)
		}
		wantDownload := tc.want
		if tc.want == f.svc.restrictedClient {
			wantDownload = f.svc.restrictedDownloadClient
		}
		if got := f.svc.enclosureClient(ctx, feed); got != wantDownload {
			t.Errorf("%s: wrong enclosure client", tc.name)
		}
	}
}

func TestCheckFeedOfNonAdminRowNeverReachesLoopback(t *testing.T) {
	f := newCheckerFixture(t)
	feed := f.feed(t, "/private", &f.user)
	err := f.svc.checkFeed(context.Background(), *feed)
	if !errors.Is(err, errBlockedAddress) || f.feeds.Load() != 0 {
		t.Fatalf("restricted fetch: err=%v hits=%d", err, f.feeds.Load())
	}
	if got := f.reload(t, feed.ID); got.ConsecutiveFailures != 1 || got.NextCheckAt == nil {
		t.Fatalf("blocked fetch not backed off: %+v", got)
	}
	// The admin's rows keep today's behavior, and the fetch stores
	// episodes without downloading audio.
	adminFeed := f.feed(t, "/public", &f.admin)
	if err := f.svc.checkFeed(context.Background(), *adminFeed); err != nil {
		t.Fatal(err)
	}
	eps, _ := f.store.ListEpisodesByFeed(context.Background(), adminFeed.ID, 10, 0)
	if len(eps) != 1 || eps[0].EpisodeKey != "guid:one" || f.audio.Load() != 0 {
		t.Fatalf("episodes %+v, audio hits %d", eps, f.audio.Load())
	}
}

func TestCheckFeedFollowsPermanentMovesOnly(t *testing.T) {
	f := newCheckerFixture(t)
	ctx := context.Background()
	redirect := func(code int, to string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, f.srv.URL+to, code) }
	}
	f.routes["/old"] = redirect(http.StatusMovedPermanently, "/mid")
	f.routes["/mid"] = redirect(http.StatusPermanentRedirect, "/new")
	f.routes["/temp"] = redirect(http.StatusFound, "/elsewhere")
	f.routes["/temp2"] = redirect(http.StatusTemporaryRedirect, "/elsewhere")
	f.routes["/perm-then-temp"] = redirect(http.StatusMovedPermanently, "/temp2")
	f.routes["/stated"] = func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel>
<title>stated</title><itunes:new-feed-url>%s/declared</itunes:new-feed-url></channel></rss>`, f.srv.URL)
	}
	host := strings.TrimPrefix(f.srv.URL, "http://")
	cases := []struct{ path, wantURL, wantAlias string }{
		{"/old", "/new", "/new"},
		{"/temp", "/temp", ""},
		{"/perm-then-temp", "/temp2", "/temp2"},
		{"/stated", "/declared", "/declared"},
	}
	for _, tc := range cases {
		feed := f.feed(t, tc.path, nil)
		if err := f.svc.checkFeed(ctx, *feed); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if got := f.reload(t, feed.ID); got.FeedURL != f.srv.URL+tc.wantURL || got.FeedKey != "feed:"+host+tc.path {
			t.Errorf("%s: fetch URL %q key %q", tc.path, got.FeedURL, got.FeedKey)
		}
		byAlias, err := f.store.GetFeedByKey(ctx, "feed:"+host+tc.wantAlias)
		if tc.wantAlias != "" && (err != nil || byAlias == nil || byAlias.ID != feed.ID) {
			t.Errorf("%s: alias %s does not resolve to the feed: %+v %v", tc.path, tc.wantAlias, byAlias, err)
		}
	}
}

func TestCheckFeedMoveIntoAnotherCatalogFeedMerges(t *testing.T) {
	f := newCheckerFixture(t)
	target := f.feed(t, "/target", nil)
	f.routes["/source"] = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.FeedURL, http.StatusMovedPermanently)
	}
	source := f.feed(t, "/source", nil)
	if err := f.svc.checkFeed(context.Background(), *source); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(t, source.ID); got.MergedInto == nil || *got.MergedInto != target.ID || got.FeedKey != target.FeedKey {
		t.Fatalf("source not merged into redirect target: %+v", got)
	}
	if got := f.reload(t, target.ID); got.FeedKey != target.FeedKey {
		t.Fatalf("target rewritten: %+v", got)
	}
}

func TestLegacySubscribeReusesRowOfURLWithoutKey(t *testing.T) {
	f := newCheckerFixture(t)
	ctx := context.Background()
	url := strings.Replace(f.srv.URL, "http://", "http://user:pw@", 1) + "/private-feed"
	first, err := f.svc.SubscribeFeed(ctx, url, "", f.admin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.SubscribeFeed(ctx, url, "", f.admin)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !strings.HasPrefix(first.FeedKey, "legacy-feed:") {
		t.Fatalf("rows %d/%d key %q", first.ID, second.ID, first.FeedKey)
	}
	if active, _ := f.store.HasActivePodcastSubscription(ctx, first.FeedKey); !active {
		t.Fatal("legacy subscribe did not record the admin's subscription")
	}
}

func TestCheckFeedAppliesNewFeedURLAfterIntermediateMerge(t *testing.T) {
	f := newCheckerFixture(t)
	ctx := context.Background()
	target := f.feed(t, "/target", nil)
	final := f.feed(t, "/final", nil)
	f.routes["/source"] = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.FeedURL, http.StatusMovedPermanently)
	}
	f.routes["/target"] = func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>target</title><itunes:new-feed-url>%s</itunes:new-feed-url></channel></rss>`, final.FeedURL)
	}
	source := f.feed(t, "/source", nil)
	if err := f.svc.checkFeed(ctx, *source); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{source.ID, target.ID} {
		if got := f.reload(t, id); got.MergedInto == nil || *got.MergedInto != final.ID || got.FeedKey != final.FeedKey {
			t.Fatalf("explicit final target not applied: %+v", got)
		}
	}
	if got := f.reload(t, final.ID); got.MergedInto != nil {
		t.Fatal("final target was taken over")
	}
}
