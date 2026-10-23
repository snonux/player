package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// syncTestService returns a sync service on a mock store that records what
// the store receives.
func syncTestService(t *testing.T, now time.Time) (*podcastSyncService, *model.PodcastSyncBatch) {
	t.Helper()
	store := repository.NewMockStore()
	got := &model.PodcastSyncBatch{}
	store.PodcastSyncRepo.SyncPodcastFunc = func(_ context.Context, _ int64, in model.PodcastSyncBatch) (*model.PodcastSyncResponse, error) {
		*got = in
		out := &model.PodcastSyncResponse{Clamped: []string{}, Rejected: []model.PodcastSyncRejection{}}
		for _, e := range in.Subscriptions {
			out.Acked = append(out.Acked, e.EditID)
		}
		return out, nil
	}
	return NewPodcastSyncService(store, &clock.MockClock{T: now}, 0), got
}

const validEdit = "abcdefghijklmnop"

func subEdit(mut func(*model.PodcastSubscriptionEdit)) model.PodcastSubscriptionEdit {
	e := model.PodcastSubscriptionEdit{EditID: validEdit, FeedKey: "feed:example.com/rss",
		FetchURL: "https://example.com/rss", URLs: []string{"http://example.com/rss"}, Subscribed: true, ObservedMS: 1000}
	if mut != nil {
		mut(&e)
	}
	return e
}

func TestPodcastSyncRejectsInvalidEdits(t *testing.T) {
	cases := []struct {
		name   string
		mut    func(*model.PodcastSubscriptionEdit)
		reason string
	}{
		{"valid", nil, ""},
		{"short edit id", func(e *model.PodcastSubscriptionEdit) { e.EditID = "short" }, model.PodcastRejectInvalidEdit},
		{"edit id charset", func(e *model.PodcastSubscriptionEdit) { e.EditID = "abcdefghijklmnop!" }, model.PodcastRejectInvalidEdit},
		{"long edit id", func(e *model.PodcastSubscriptionEdit) { e.EditID = strings.Repeat("a", 65) }, model.PodcastRejectInvalidEdit},
		{"no prefix", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "example.com/rss" }, model.PodcastRejectInvalidKey},
		{"not canonical", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "feed:Example.com/rss" }, model.PodcastRejectInvalidKey},
		{"empty location", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "feed:" }, model.PodcastRejectInvalidKey},
		{"episode key", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "guid:x" }, model.PodcastRejectInvalidKey},
		{"legacy zero", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "legacy-feed:01" }, model.PodcastRejectInvalidKey},
		{"too long", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "feed:a.example.com/" + strings.Repeat("x", 2100) }, model.PodcastRejectInvalidKey},
		{"legacy key", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "legacy-feed:12" }, ""},
		{"https port key", func(e *model.PodcastSubscriptionEdit) { e.FeedKey = "feed:example.com:80/rss" }, ""},
		{"bad url", func(e *model.PodcastSubscriptionEdit) { e.URLs = []string{"ftp://example.com/rss"} }, model.PodcastRejectInvalidURL},
		{"bad fetch url", func(e *model.PodcastSubscriptionEdit) { e.FetchURL = "https://user:pw@example.com/" }, model.PodcastRejectInvalidURL},
		{"too many urls", func(e *model.PodcastSubscriptionEdit) {
			e.URLs = strings.Split(strings.Repeat("https://example.com/rss ", 11), " ")[:11]
		}, model.PodcastRejectInvalidURL},
		{"long title", func(e *model.PodcastSubscriptionEdit) { e.Title = strings.Repeat("é", 501) }, model.PodcastRejectInvalidEdit},
		{"bad utf8 title", func(e *model.PodcastSubscriptionEdit) { e.Title = "\xff" }, model.PodcastRejectInvalidEdit},
		{"zero clock", func(e *model.PodcastSubscriptionEdit) { e.ObservedMS = 0 }, model.PodcastRejectInvalidTime},
		{"clock beyond 9999", func(e *model.PodcastSubscriptionEdit) { e.ObservedMS = 253402300800000 }, model.PodcastRejectInvalidTime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, got := syncTestService(t, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
			out, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{subEdit(tc.mut)}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason == "" {
				if len(out.Rejected) != 0 || len(got.Subscriptions) != 1 {
					t.Fatalf("valid edit rejected: %+v", out.Rejected)
				}
				return
			}
			if len(out.Rejected) != 1 || out.Rejected[0].Reason != tc.reason || len(got.Subscriptions) != 0 {
				t.Fatalf("rejected = %+v, store got %d edits; want %s", out.Rejected, len(got.Subscriptions), tc.reason)
			}
		})
	}
}

func TestPodcastSyncClampsFutureClocks(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	svc, got := syncTestService(t, now)
	near := subEdit(func(e *model.PodcastSubscriptionEdit) { e.ObservedMS = now.Add(5 * time.Minute).UnixMilli() })
	far := subEdit(func(e *model.PodcastSubscriptionEdit) {
		e.EditID, e.ObservedMS = "farfarfarfarfarfar", now.Add(5*time.Minute+time.Millisecond).UnixMilli()
	})
	out, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{near, far}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Clamped) != 1 || out.Clamped[0] != far.EditID {
		t.Fatalf("clamped = %v", out.Clamped)
	}
	if got.Subscriptions[0].ObservedMS != near.ObservedMS || got.Subscriptions[1].ObservedMS != now.UnixMilli() {
		t.Fatalf("clocks passed on: %d %d", got.Subscriptions[0].ObservedMS, got.Subscriptions[1].ObservedMS)
	}
	if got.CheckIntervalMinutes != 60 || !got.Now.Equal(now) {
		t.Fatalf("store input: %+v", got)
	}
}

func TestPodcastSyncEnvelope(t *testing.T) {
	negative := int64(-1)
	since := int64(7)
	many := make([]model.PodcastSubscriptionEdit, MaxPodcastSyncEdits+1)
	// Each episode item with both registers counts as two edits.
	manyEpisodes := make([]model.PodcastEpisodeEdit, MaxPodcastSyncEdits/2+1)
	for i := range manyEpisodes {
		manyEpisodes[i] = epEdit(nil)
	}
	cases := []struct {
		name string
		req  model.PodcastSyncRequest
		want error
	}{
		{"limit too large", model.PodcastSyncRequest{Limit: MaxPodcastSyncLimit + 1}, ErrInvalidPodcastSync},
		{"negative limit", model.PodcastSyncRequest{Limit: -1}, ErrInvalidPodcastSync},
		{"negative since", model.PodcastSyncRequest{Since: &negative}, ErrInvalidPodcastSync},
		{"too many edits", model.PodcastSyncRequest{Subscriptions: many}, ErrInvalidPodcastSync},
		{"episode item without edits", model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{{FeedKey: "feed:a/"}}}, ErrInvalidPodcastSync},
		{"empty scope", model.PodcastSyncRequest{Scope: &model.PodcastSyncScope{}}, ErrInvalidPodcastSync},
		{"scope with since", model.PodcastSyncRequest{Since: &since, Scope: &model.PodcastSyncScope{FeedKey: "feed:a/", EpisodeKey: "guid:x"}}, ErrInvalidPodcastSync},
		{"scope with bad episode key", model.PodcastSyncRequest{Scope: &model.PodcastSyncScope{FeedKey: "feed:a/", EpisodeKey: "x"}}, ErrInvalidPodcastSync},
		{"too many episode edits", model.PodcastSyncRequest{Episodes: manyEpisodes}, ErrInvalidPodcastSync},
		{"pull only", model.PodcastSyncRequest{Since: &since, Limit: MaxPodcastSyncLimit, Epoch: "e"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, got := syncTestService(t, time.Now())
			_, err := svc.Sync(context.Background(), 1, tc.req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (got.Since != since || got.Epoch != "e" || got.Limit != MaxPodcastSyncLimit) {
				t.Fatalf("pull parameters: %+v", got)
			}
		})
	}
}

func TestPodcastSyncStoreError(t *testing.T) {
	store := repository.NewMockStore()
	boom := errors.New("boom")
	store.PodcastSyncRepo.SyncPodcastFunc = func(context.Context, int64, model.PodcastSyncBatch) (*model.PodcastSyncResponse, error) {
		return nil, boom
	}
	svc := NewPodcastSyncService(store, &clock.MockClock{T: time.Now()}, 30)
	if _, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

// TestPodcastSyncClampRetryOnRealStore pins the accepted cost of clock
// clamping (contract §4 rule 4): an edit from a device whose clock runs far
// ahead is stored at the server's receive time. If its acknowledgement is
// lost and the device retries later, the retry is clamped again, to the
// later receive time, and so overwrites an edit another device made in
// between. The alternative, trusting the future clock, would block every
// later edit of that register until the wall clock caught up.
func TestPodcastSyncClampRetryOnRealStore(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	user, err := store.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	now1 := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	clk := &clock.MockClock{T: now1}
	svc := NewPodcastSyncService(store, clk, 60)
	sync := func(e model.PodcastSubscriptionEdit) *model.PodcastSyncResponse {
		t.Helper()
		out, err := svc.Sync(ctx, user, model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{e}})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	register := func(out *model.PodcastSyncResponse) model.PodcastSubscription {
		t.Helper()
		if len(out.Subscriptions) != 1 {
			t.Fatalf("subscriptions: %+v", out.Subscriptions)
		}
		return out.Subscriptions[0]
	}

	future := subEdit(func(e *model.PodcastSubscriptionEdit) {
		e.Subscribed, e.ObservedMS = true, now1.Add(time.Hour).UnixMilli()
	})
	out := sync(future)
	if len(out.Acked) != 1 || len(out.Clamped) != 1 || register(out).SubscriptionMS != now1.UnixMilli() {
		t.Fatalf("first attempt: %+v", out)
	}

	// Another device unsubscribes between the two attempts and wins.
	between := subEdit(func(e *model.PodcastSubscriptionEdit) {
		e.EditID, e.Subscribed, e.ObservedMS = "betweenbetweenbe", false, now1.Add(30*time.Second).UnixMilli()
	})
	if got := register(sync(between)); got.Subscribed {
		t.Fatalf("edit after the clamped clock lost: %+v", got)
	}

	// The lost acknowledgement makes the first device retry the same edit
	// a minute later: clamped to now2, it overwrites the unsubscribe.
	now2 := now1.Add(time.Minute)
	clk.T = now2
	out = sync(future)
	got := register(out)
	if len(out.Acked) != 1 || out.Acked[0] != future.EditID || len(out.Clamped) != 1 ||
		!got.Subscribed || got.SubscriptionMS != now2.UnixMilli() {
		t.Fatalf("retry: %+v", out)
	}
}
