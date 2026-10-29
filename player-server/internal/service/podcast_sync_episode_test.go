package service

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

const (
	validPositionEdit   = "position-edit-0001"
	validCompletionEdit = "completion-edit-01"
)

// epEdit builds a valid episode item with both registers.
func epEdit(mut func(*model.PodcastEpisodeEdit)) model.PodcastEpisodeEdit {
	e := model.PodcastEpisodeEdit{FeedKey: "feed:example.com/rss", EpisodeKey: "guid:ep-42",
		EnclosureURL: "https://cdn.example.com/42.mp3",
		Position:     &model.PodcastPositionEdit{EditID: validPositionEdit, Seconds: 812.4, ObservedMS: 1000},
		Completion:   &model.PodcastCompletionEdit{EditID: validCompletionEdit, Completed: true, Source: "auto", ObservedMS: 1000}}
	if mut != nil {
		mut(&e)
	}
	return e
}

func TestPodcastSyncRejectsInvalidEpisodeEdits(t *testing.T) {
	both := []string{validPositionEdit, validCompletionEdit}
	cases := []struct {
		name     string
		mut      func(*model.PodcastEpisodeEdit)
		rejected []string // edit ids rejected
		reason   string
	}{
		{"valid", nil, nil, ""},
		{"position only", func(e *model.PodcastEpisodeEdit) { e.Completion = nil }, nil, ""},
		{"enc key", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "enc:cdn.example.com/42.mp3" }, nil, ""},
		{"legacy key", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "legacy-episode:7" }, nil, ""},
		{"hashed guid", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "guid:sha256:" + strings.Repeat("a", 64) }, nil, ""},
		{"no enclosure", func(e *model.PodcastEpisodeEdit) { e.EnclosureURL = "" }, nil, ""},
		{"bad feed key", func(e *model.PodcastEpisodeEdit) { e.FeedKey = "feed:Example.com/rss" }, both, model.PodcastRejectInvalidKey},
		{"empty guid", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "guid:" }, both, model.PodcastRejectInvalidKey},
		{"untrimmed guid", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "guid: x" }, both, model.PodcastRejectInvalidKey},
		{"overlong guid", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "guid:" + strings.Repeat("g", 1025) }, both, model.PodcastRejectInvalidKey},
		{"bad utf8 guid", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "guid:\xff" }, both, model.PodcastRejectInvalidKey},
		{"uncanonical enc", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "enc:CDN.example.com/42.mp3" }, both, model.PodcastRejectInvalidKey},
		{"feed key as episode", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "feed:example.com/rss" }, both, model.PodcastRejectInvalidKey},
		{"legacy zero", func(e *model.PodcastEpisodeEdit) { e.EpisodeKey = "legacy-episode:0" }, both, model.PodcastRejectInvalidKey},
		{"bad enclosure", func(e *model.PodcastEpisodeEdit) { e.EnclosureURL = "/relative.mp3" }, both, model.PodcastRejectInvalidURL},
		{"negative position", func(e *model.PodcastEpisodeEdit) { e.Position.Seconds = -1 }, []string{validPositionEdit}, model.PodcastRejectInvalidPosition},
		{"huge position", func(e *model.PodcastEpisodeEdit) { e.Position.Seconds = 9e12 + 1 }, []string{validPositionEdit}, model.PodcastRejectInvalidPosition},
		{"infinite position", func(e *model.PodcastEpisodeEdit) { e.Position.Seconds = math.Inf(1) }, []string{validPositionEdit}, model.PodcastRejectInvalidPosition},
		{"position clock zero", func(e *model.PodcastEpisodeEdit) { e.Position.ObservedMS = 0 }, []string{validPositionEdit}, model.PodcastRejectInvalidTime},
		{"position edit id", func(e *model.PodcastEpisodeEdit) { e.Position.EditID = "short" }, []string{"short"}, model.PodcastRejectInvalidEdit},
		{"unknown source", func(e *model.PodcastEpisodeEdit) { e.Completion.Source = "sync" }, []string{validCompletionEdit}, model.PodcastRejectInvalidEdit},
		{"completion clock beyond 9999", func(e *model.PodcastEpisodeEdit) { e.Completion.ObservedMS = maxObservedMS + 1 }, []string{validCompletionEdit}, model.PodcastRejectInvalidTime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, got := syncTestService(t, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
			item := epEdit(tc.mut)
			out, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{item}})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Rejected) != len(tc.rejected) {
				t.Fatalf("rejected = %+v, want %v", out.Rejected, tc.rejected)
			}
			for i, id := range tc.rejected {
				if out.Rejected[i].EditID != id || out.Rejected[i].Reason != tc.reason {
					t.Fatalf("rejection %d = %+v, want %s %s", i, out.Rejected[i], id, tc.reason)
				}
			}
			assertStoredRegisters(t, got.Episodes, item, tc.rejected)
		})
	}
}

// assertStoredRegisters checks that exactly the registers that were not
// rejected reach the store.
func assertStoredRegisters(t *testing.T, stored []model.PodcastEpisodeEdit, sent model.PodcastEpisodeEdit, rejected []string) {
	t.Helper()
	want := len(episodeItemIDs(sent)) - len(rejected)
	if want == 0 {
		if len(stored) != 0 {
			t.Fatalf("an item without valid edits reached the store: %+v", stored)
		}
		return
	}
	if len(stored) != 1 || len(episodeItemIDs(stored[0])) != want {
		t.Fatalf("store got %+v, want %d register(s)", stored, want)
	}
}

func TestPodcastSyncClampsEpisodeClocksPerRegister(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	svc, got := syncTestService(t, now)
	item := epEdit(func(e *model.PodcastEpisodeEdit) {
		e.Position.ObservedMS = now.Add(5 * time.Minute).UnixMilli()
		e.Completion.ObservedMS = now.Add(time.Hour).UnixMilli()
	})
	out, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{item}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Clamped) != 1 || out.Clamped[0] != validCompletionEdit {
		t.Fatalf("clamped = %v", out.Clamped)
	}
	stored := got.Episodes[0]
	if stored.Position.ObservedMS != item.Position.ObservedMS || stored.Completion.ObservedMS != now.UnixMilli() {
		t.Fatalf("clocks passed on: %+v %+v", stored.Position, stored.Completion)
	}
	// The caller's item is not modified (the store gets copies).
	if item.Completion.ObservedMS == now.UnixMilli() {
		t.Fatal("validation changed the request's item")
	}
}

func TestPodcastSyncPassesScopeToStore(t *testing.T) {
	svc, got := syncTestService(t, time.Now())
	scope := &model.PodcastSyncScope{FeedKey: "feed:example.com/rss", EpisodeKey: "enc:cdn.example.com/1.mp3"}
	if _, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{Scope: scope}); err != nil {
		t.Fatal(err)
	}
	if got.Scope == nil || *got.Scope != *scope {
		t.Fatalf("scope = %+v", got.Scope)
	}
}

// TestPodcastSyncClampedEpisodeRetryOnRealStore pins the accepted cost of
// clamping (contract §4 rule 4) for an episode register, as the
// subscription test does for subscriptions: a far-future edit is stored at
// the receive time; another device's later edit wins; a retry of the
// first edit after a lost acknowledgement is clamped again, to the later
// receive time, and overwrites it.
func TestPodcastSyncClampedEpisodeRetryOnRealStore(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	user, err := store.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	now1 := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	clk := &clock.MockClock{T: now1}
	svc := NewPodcastSyncService(store, clk, 60, "")
	sync := func(item model.PodcastEpisodeEdit) *model.PodcastSyncResponse {
		t.Helper()
		out, err := svc.Sync(ctx, user, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{item}})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	position := func(out *model.PodcastSyncResponse) model.PodcastPositionRegister {
		t.Helper()
		if len(out.Episodes) != 1 || out.Episodes[0].Position == nil {
			t.Fatalf("episodes: %+v", out.Episodes)
		}
		return *out.Episodes[0].Position
	}
	future := epEdit(func(e *model.PodcastEpisodeEdit) {
		e.Completion, e.Position.Seconds, e.Position.ObservedMS = nil, 100, now1.Add(time.Hour).UnixMilli()
	})
	out := sync(future)
	if len(out.Acked) != 1 || len(out.Clamped) != 1 || position(out).ObservedMS != now1.UnixMilli() {
		t.Fatalf("first attempt: %+v", out)
	}
	between := epEdit(func(e *model.PodcastEpisodeEdit) {
		e.Completion = nil
		e.Position = &model.PodcastPositionEdit{EditID: "between-between-1", Seconds: 200, ObservedMS: now1.Add(30 * time.Second).UnixMilli()}
	})
	if got := position(sync(between)); got.Seconds != 200 {
		t.Fatalf("edit after the clamped clock lost: %+v", got)
	}
	clk.T = now1.Add(time.Minute)
	out = sync(future)
	if got := position(out); len(out.Acked) != 1 || out.Acked[0] != validPositionEdit || len(out.Clamped) != 1 ||
		got.Seconds != 100 || got.ObservedMS != clk.T.UnixMilli() {
		t.Fatalf("retry: %+v", out)
	}
}

// TestPodcastSyncDoesNotReportRejectedEditsAsClamped: an edit the store
// rejects (here an unknown legacy key) was not applied, so it is not
// listed in clamped either.
func TestPodcastSyncDoesNotReportRejectedEditsAsClamped(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	store := repository.NewMockStore()
	store.PodcastSyncRepo.SyncPodcastFunc = func(_ context.Context, _ int64, in model.PodcastSyncBatch) (*model.PodcastSyncResponse, error) {
		return &model.PodcastSyncResponse{Clamped: []string{}, Rejected: []model.PodcastSyncRejection{
			{EditID: in.Episodes[0].Position.EditID, Reason: model.PodcastRejectInvalidKey}}}, nil
	}
	svc := NewPodcastSyncService(store, &clock.MockClock{T: now}, 0, "")
	item := epEdit(func(e *model.PodcastEpisodeEdit) {
		e.Position.ObservedMS = now.Add(time.Hour).UnixMilli()
		e.Completion.ObservedMS = now.Add(time.Hour).UnixMilli()
	})
	out, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{item}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Clamped) != 1 || out.Clamped[0] != validCompletionEdit || len(out.Rejected) != 1 {
		t.Fatalf("clamped %v, rejected %+v", out.Clamped, out.Rejected)
	}
}

func TestPodcastSyncDeletionValidationAndLimits(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		ms         int64
		id, reason string
	}{
		{"valid", 1000, "deletion-edit-0001", ""},
		{"future", now.Add(time.Hour).UnixMilli(), "deletion-edit-0001", ""},
		{"zero", 0, "deletion-edit-0001", model.PodcastRejectInvalidTime},
		{"invalid id", 1000, "short", model.PodcastRejectInvalidEdit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, got := syncTestService(t, now)
			item := epEdit(nil)
			item.Position = nil
			item.Completion = nil
			item.Deletion = &model.PodcastDeletionEdit{EditID: tc.id, Deleted: true, ObservedMS: tc.ms}
			out, err := svc.Sync(context.Background(), 1, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{item}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason != "" {
				if len(out.Rejected) != 1 || out.Rejected[0].Reason != tc.reason || len(got.Episodes) != 0 {
					t.Fatalf("bad deletion accepted: %+v %+v", out, got)
				}
				return
			}
			if len(got.Episodes) != 1 || got.Episodes[0].Deletion == nil {
				t.Fatalf("deletion lost: %+v", got)
			}
			want := tc.ms
			if want > now.Add(model.PodcastClockSkew).UnixMilli() {
				want = now.UnixMilli()
				if len(out.Clamped) != 1 {
					t.Fatal("future deletion not reported clamped")
				}
			}
			if got.Episodes[0].Deletion.ObservedMS != want {
				t.Fatal("wrong deletion clock")
			}
		})
	}
	// All three independent edits count towards the existing envelope limit.
	item := epEdit(nil)
	item.Deletion = &model.PodcastDeletionEdit{EditID: "deletion-edit-0001", Deleted: true, ObservedMS: 1000}
	items := make([]model.PodcastEpisodeEdit, 167)
	for i := range items {
		items[i] = item
	}
	if err := checkSyncEnvelope(model.PodcastSyncRequest{Episodes: items}); err != ErrInvalidPodcastSync {
		t.Fatal("third register bypassed edit limit")
	}
}
