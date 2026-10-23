package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

// episodeEdit builds an episode item of ep1 with the given registers; a
// negative position or a nil completion leaves that register out.
func episodeEdit(feedKey, id string, position float64, completed *bool, ms int64) model.PodcastEpisodeEdit {
	item := model.PodcastEpisodeEdit{FeedKey: feedKey, EpisodeKey: "guid:ep1"}
	if position >= 0 {
		item.Position = &model.PodcastPositionEdit{EditID: "pos-edit-" + id + "-0000000", Seconds: position, ObservedMS: ms}
	}
	if completed != nil {
		item.Completion = &model.PodcastCompletionEdit{EditID: "cmp-edit-" + id + "-0000000", Completed: *completed,
			Source: "manual", ObservedMS: ms}
	}
	return item
}

// phoneSync posts a sync request with alice's device token.
func phoneSync(t *testing.T, e *e2e, token string, req model.PodcastSyncRequest) (model.PodcastSyncResponse, string) {
	t.Helper()
	rr := syncDeviceRequest(t, e.server, token, http.MethodPost, "/api/v1/podcasts/sync", req)
	if rr.Code != http.StatusOK {
		t.Fatalf("phone sync = %d: %s", rr.Code, rr.Body)
	}
	var out model.PodcastSyncResponse
	e.must(json.Unmarshal(rr.Body.Bytes(), &out))
	return out, rr.Body.String()
}

// TestPodcastEpisodeSyncAcrossDevices drives the contract's conflict
// examples (§4.1) through the HTTP API with the production wiring: a phone
// (Bearer token) and the web (cookie) of one account, and another account.
func TestPodcastEpisodeSyncAcrossDevices(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	key, _, _ := e.linkedEpisode(t)
	phone := syncDeviceToken(t, e, "alice", "phone")
	done := true
	t1 := time.Now().Add(-time.Hour).UnixMilli()

	// The phone, offline, marked the episode completed at t1; the web saved
	// a position at t2 > t1 meanwhile. Both registers are kept.
	web := e.podcastSync("/api/v1/podcasts/sync", "alice", model.PodcastSyncRequest{
		Episodes: []model.PodcastEpisodeEdit{episodeEdit(key, "web", 300, nil, t1+1000)}})
	if len(web.Acked) != 1 {
		t.Fatalf("web edit: %+v", web)
	}
	offline := episodeEdit(key, "phone", -1, &done, t1)
	out, _ := phoneSync(t, e, phone, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{offline}})
	ep := findEpisode(t, out.Episodes, key)
	if len(out.Acked) != 1 || !ep.Completion.Completed || ep.Position.Seconds != 300 {
		t.Fatalf("offline completion: %+v %+v", out.Acked, ep)
	}
	// The phone lost the answer and retries: acked again, nothing changes.
	retry, _ := phoneSync(t, e, phone, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{offline}})
	if len(retry.Acked) != 1 || retry.Acked[0] != offline.Completion.EditID || len(retry.Rejected) != 0 {
		t.Fatalf("retry: %+v", retry)
	}
	// An older delayed uncompletion is acked but superseded.
	undone := false
	stale, _ := phoneSync(t, e, phone, model.PodcastSyncRequest{Episodes: []model.PodcastEpisodeEdit{episodeEdit(key, "late", -1, &undone, t1-1)}})
	if len(stale.Acked) != 1 || !findEpisode(t, stale.Episodes, key).Completion.Completed {
		t.Fatalf("delayed edit: %+v", stale)
	}
	// The pre-playback scope answers that one episode without a cursor.
	scoped, raw := phoneSync(t, e, phone, model.PodcastSyncRequest{Scope: &model.PodcastSyncScope{FeedKey: key, EpisodeKey: "guid:ep1"}})
	if len(scoped.Episodes) != 1 || strings.Contains(raw, "next_since") || strings.Contains(raw, "has_more") {
		t.Fatalf("scoped answer: %s", raw)
	}
	// Bob sees none of it, also not through a scope on alice's keys.
	bob := e.podcastSync("/api/v1/podcasts/sync", "bob", model.PodcastSyncRequest{Scope: &model.PodcastSyncScope{FeedKey: key, EpisodeKey: "guid:ep1"}})
	if len(bob.Episodes) != 0 {
		t.Fatalf("bob sees alice's state: %+v", bob.Episodes)
	}
}

// findEpisode returns the ep1 row of a sync answer.
func findEpisode(t *testing.T, episodes []model.PodcastEpisodeState, feedKey string) model.PodcastEpisodeState {
	t.Helper()
	for _, ep := range episodes {
		if ep.FeedKey == feedKey && ep.EpisodeKey == "guid:ep1" {
			return ep
		}
	}
	t.Fatalf("ep1 missing from %+v", episodes)
	return model.PodcastEpisodeState{}
}

// TestPodcastLegacyReadersSeeEpisodeState checks the old-client side of
// the switch: the capability, the legacy episode listing, the toggle and
// the projection of a replayed completed episode for old Android clients
// and the web In progress list.
func TestPodcastLegacyReadersSeeEpisodeState(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	key, epID, mediaID := e.linkedEpisode(t)
	var cfg map[string]int
	e.must(json.Unmarshal(e.get("/api/v1/config", "alice").Body.Bytes(), &cfg))
	if cfg["podcast_sync"] != 1 {
		t.Fatalf("config: %+v", cfg)
	}
	done := true
	e.podcastSync("/api/v1/podcasts/sync", "alice", model.PodcastSyncRequest{
		Episodes: []model.PodcastEpisodeEdit{episodeEdit(key, "end", 598, &done, time.Now().Add(-time.Hour).UnixMilli())}})
	listed := e.listEpisodes(t, key)
	if !listed.IsCompleted || listed.PositionSeconds != 598 || listed.EpisodeKey != "guid:ep1" || listed.FeedKey != key {
		t.Fatalf("legacy listing: %+v", listed)
	}
	// The web replays the completed episode: position saves after the
	// completion project unfinished, so it is In progress and an old
	// Android client resumes it at the saved position instead of 0.
	// One session (a device token) plays it: play counting is per session.
	player := syncDeviceToken(t, e, "alice", "player")
	for pos := 12; pos <= 72; pos += 12 {
		rr := syncDeviceRequest(t, e.server, player, http.MethodPost, "/api/v1/progress", map[string]any{"media_id": mediaID, "position_seconds": pos})
		if rr.Code != http.StatusOK {
			t.Fatalf("progress = %d: %s", rr.Code, rr.Body)
		}
	}
	var inProgress []model.Media
	e.must(json.Unmarshal(e.get("/api/v1/in-progress", "alice").Body.Bytes(), &inProgress))
	if len(inProgress) != 1 || inProgress[0].ID != mediaID || *inProgress[0].PositionSeconds != 72 {
		t.Fatalf("in progress: %+v", inProgress)
	}
	// The listening counted as a play of the copy, as for any media, and a
	// later sync API edit rewrites the mirror without losing the time.
	media, err := e.store.GetMediaByID(ctx, mediaID)
	e.must(err)
	before, err := e.store.GetProgress(ctx, e.users["alice"], mediaID)
	e.must(err)
	if media.PlayCount != 1 || before.AccumulatedSeconds < 60 {
		t.Fatalf("play count %d, accumulated %v", media.PlayCount, before.AccumulatedSeconds)
	}
	e.podcastSync("/api/v1/podcasts/sync", "alice", model.PodcastSyncRequest{
		Episodes: []model.PodcastEpisodeEdit{episodeEdit(key, "seek", 72, nil, time.Now().UnixMilli())}})
	if after, _ := e.store.GetProgress(ctx, e.users["alice"], mediaID); after.AccumulatedSeconds != before.AccumulatedSeconds {
		t.Fatalf("mirror rewrite changed accumulated time: %v -> %v", before.AccumulatedSeconds, after.AccumulatedSeconds)
	}
	old := syncDevice(t, e.server, syncDeviceToken(t, e, "alice", "old-phone"), fmt.Sprintf("/api/v1/media/%d/playback-sync", mediaID), model.PlaybackSync{})
	if old.Progress == nil || old.Progress.Finished || old.Progress.PositionSeconds != 72 {
		t.Fatalf("old client projection: %+v", old.Progress)
	}
	if listed = e.listEpisodes(t, key); !listed.IsCompleted || listed.PositionSeconds != 72 {
		t.Fatalf("replay changed completion in the listing: %+v", listed)
	}
	// The toggle flips the register the listing reads.
	if rr := e.request(ctx, http.MethodPost, fmt.Sprintf("/api/v1/podcasts/episodes/%d/complete", epID), "alice", ""); rr.Code/100 != 2 {
		t.Fatalf("toggle = %d: %s", rr.Code, rr.Body)
	}
	if listed = e.listEpisodes(t, key); listed.IsCompleted || listed.PositionSeconds != 72 {
		t.Fatalf("listing after the toggle: %+v", listed)
	}
	// Bob may not read the podcast set: no listing, no toggle.
	if rr := e.request(ctx, http.MethodPost, fmt.Sprintf("/api/v1/podcasts/episodes/%d/complete", epID), "bob", ""); rr.Code != http.StatusForbidden {
		t.Fatalf("bob's toggle = %d", rr.Code)
	}
}

// listEpisodes returns ep1 from alice's legacy episode listing.
func (e *e2e) listEpisodes(t *testing.T, feedKey string) model.PodcastEpisodeWithStatus {
	t.Helper()
	feed, err := e.store.GetFeedByKey(context.Background(), feedKey)
	e.must(err)
	rr := e.get(fmt.Sprintf("/api/v1/podcasts/%d/episodes", feed.SetID), "alice")
	var episodes []model.PodcastEpisodeWithStatus
	e.must(json.Unmarshal(rr.Body.Bytes(), &episodes))
	if len(episodes) != 1 {
		t.Fatalf("listing = %d: %s", rr.Code, rr.Body)
	}
	return episodes[0]
}
