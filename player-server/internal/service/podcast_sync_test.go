package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// syncTestService returns a sync service on a mock store that records what
// the store receives.
func syncTestService(t *testing.T, now time.Time) (*podcastSyncService, *model.PodcastSubscriptionSync) {
	t.Helper()
	store := repository.NewMockStore()
	got := &model.PodcastSubscriptionSync{}
	store.PodcastSyncRepo.SyncPodcastSubscriptionsFunc = func(_ context.Context, _ int64, in model.PodcastSubscriptionSync) (*model.PodcastSyncResponse, error) {
		*got = in
		out := &model.PodcastSyncResponse{Clamped: []string{}, Rejected: []model.PodcastSyncRejection{}}
		for _, e := range in.Edits {
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
				if len(out.Rejected) != 0 || len(got.Edits) != 1 {
					t.Fatalf("valid edit rejected: %+v", out.Rejected)
				}
				return
			}
			if len(out.Rejected) != 1 || out.Rejected[0].Reason != tc.reason || len(got.Edits) != 0 {
				t.Fatalf("rejected = %+v, store got %d edits; want %s", out.Rejected, len(got.Edits), tc.reason)
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
	if got.Edits[0].ObservedMS != near.ObservedMS || got.Edits[1].ObservedMS != now.UnixMilli() {
		t.Fatalf("clocks passed on: %d %d", got.Edits[0].ObservedMS, got.Edits[1].ObservedMS)
	}
	if got.CheckIntervalMinutes != 60 || !got.Now.Equal(now) {
		t.Fatalf("store input: %+v", got)
	}
}

func TestPodcastSyncEnvelope(t *testing.T) {
	negative := int64(-1)
	since := int64(7)
	many := make([]model.PodcastSubscriptionEdit, MaxPodcastSyncEdits+1)
	cases := []struct {
		name string
		req  model.PodcastSyncRequest
		want error
	}{
		{"limit too large", model.PodcastSyncRequest{Limit: MaxPodcastSyncLimit + 1}, ErrInvalidPodcastSync},
		{"negative limit", model.PodcastSyncRequest{Limit: -1}, ErrInvalidPodcastSync},
		{"negative since", model.PodcastSyncRequest{Since: &negative}, ErrInvalidPodcastSync},
		{"too many edits", model.PodcastSyncRequest{Subscriptions: many}, ErrInvalidPodcastSync},
		{"episodes", model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{{FeedKey: "feed:a/"}}}, ErrPodcastEpisodeSyncUnsupported},
		{"scope", model.PodcastSyncRequest{Scope: &model.PodcastSyncScope{}}, ErrPodcastEpisodeSyncUnsupported},
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
	store.PodcastSyncRepo.SyncPodcastSubscriptionsFunc = func(context.Context, int64, model.PodcastSubscriptionSync) (*model.PodcastSyncResponse, error) {
		return nil, boom
	}
	svc := NewPodcastSyncService(store, &clock.MockClock{T: time.Now()}, 30)
	if _, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
