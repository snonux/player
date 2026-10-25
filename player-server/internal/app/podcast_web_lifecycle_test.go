package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

// Production HTTP/service/SQLite checks, without launching a server process.
func TestPodcastWebLifecycleDetailAndTrash(t *testing.T) {
	var root string
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) { root = cfg.MediaRoot })
	ctx, now := context.Background(), time.Now()
	mediaID := e.media["movie.mp4"]
	m, err := e.store.GetMediaByID(ctx, mediaID)
	e.must(err)
	feed := &model.PodcastFeed{SetID: m.SetID, FeedURL: "https://web.example.com/rss", Title: "Web", CreatedAt: now}
	feedID, err := e.store.CreateFeed(ctx, feed)
	e.must(err)
	e.must(e.store.SetPodcastSubscription(ctx, e.users["alice"], feed, true, now))
	epID, err := e.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: "ep1", EpisodeURL: "https://cdn.example.com/web.mp4", CreatedAt: now})
	e.must(err)
	ms := now.Add(-time.Hour).UnixMilli()
	phone := syncDeviceToken(t, e, "alice", "independent phone")
	phoneSync(t, e, phone, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{episodeEdit(feed.FeedKey, "before-copy", 123, nil, ms)}})
	// State existed before the server copy. Linking seeds the projection;
	// authenticated detail exposes the raw stable register, not only a mirror.
	e.must(e.store.UpdateEpisodeMedia(ctx, epID, mediaID, m.FileName))
	for _, prefix := range []string{"/api", "/api/v1"} {
		rr := e.get(fmt.Sprintf("%s/media/%d", prefix, mediaID), "alice")
		var detail service.MediaDetail
		e.must(json.Unmarshal(rr.Body.Bytes(), &detail))
		if rr.Code != http.StatusOK || detail.PodcastEpisode == nil || detail.PodcastEpisode.FeedKey != feed.FeedKey || detail.PodcastEpisode.Position.Seconds != 123 {
			t.Fatalf("detail = %d %+v", rr.Code, detail)
		}
		if rr := e.get(fmt.Sprintf("%s/media/%d", prefix, mediaID), "bob"); rr.Code != http.StatusForbidden {
			t.Fatalf("inaccessible detail = %d", rr.Code)
		}
	}
	done := true
	end := episodeEdit(feed.FeedKey, "natural-end", 600, &done, ms+1000)
	end.Completion.Source = model.PodcastCompletionAuto
	e.podcastSync("/api/podcasts/sync", "alice", model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{end}})
	// A manual uncompletion keeps the terminal position.
	done = false
	e.podcastSync("/api/podcasts/sync", "alice", model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{episodeEdit(feed.FeedKey, "manual", -1, &done, ms+2000)}})
	path := fmt.Sprintf("/api/media/%d", mediaID)
	if rr := e.request(ctx, http.MethodDelete, path, "alice", ""); rr.Code != http.StatusForbidden {
		t.Fatalf("viewer delete = %d", rr.Code)
	}
	if rr := e.request(ctx, http.MethodDelete, path, "admin", ""); rr.Code != http.StatusOK {
		t.Fatalf("admin delete = %d: %s", rr.Code, rr.Body)
	}
	if _, err := os.Stat(m.AbsPath); err != nil {
		t.Fatalf("trash removed file immediately: %v", err)
	}
	views, err := e.deps.PodcastSvc.ListSubscribedEpisodes(ctx, e.users["alice"], repository.PodcastEpisodeQuery{Filter: "all", Limit: 10})
	e.must(err)
	if len(views) != 1 || views[0].ServerCopy != nil || views[0].Completed || views[0].PositionSeconds != 600 {
		t.Fatalf("trashed episode view = %+v", views)
	}
	// Simulate a real unlink failure (ENOTEMPTY), which works even as root.
	e.must(os.Remove(m.AbsPath))
	e.must(os.Mkdir(m.AbsPath, 0o755))
	child := filepath.Join(m.AbsPath, "block-unlink")
	e.must(os.WriteFile(child, []byte("fixture"), 0o600))
	w := service.NewGCWorker(e.store, &clock.MockClock{T: now.Add(8 * 24 * time.Hour)}, root, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.must(w.RunOnce())
	episode, err := e.store.GetEpisodeByID(ctx, epID)
	e.must(err)
	if episode.MediaID == nil {
		t.Fatal("failed cleanup dropped the retry link")
	}
	e.must(os.Remove(child))
	e.must(w.RunOnce())
	episode, err = e.store.GetEpisodeByID(ctx, epID)
	e.must(err)
	if episode == nil || episode.MediaID != nil {
		t.Fatalf("cleanup retry lost episode or retained copy: %+v", episode)
	}
	state := e.aliceEpisode(t, feed.FeedKey)
	if state.Position.Seconds != 600 || state.Position.ObservedMS != ms+1000 || state.Completion.Completed || state.Completion.ObservedMS != ms+2000 {
		t.Fatalf("cleanup changed history: %+v", state)
	}
	feeds, err := e.store.PodcastSubscriptionStates(ctx, e.users["alice"])
	e.must(err)
	if !feeds[feed.FeedKey] {
		t.Fatal("cleanup unsubscribed the user")
	}
}
