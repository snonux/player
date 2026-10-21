package service

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
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
