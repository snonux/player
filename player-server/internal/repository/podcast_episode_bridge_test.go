package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// bridgeFixture is a sync fixture with alice's server copy of ep1.
type bridgeFixture struct {
	*syncFixture
	mediaID int64
	ep1     int64
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	f := newSyncFixture(t)
	setID, ep1, _ := f.catalogEpisodes()
	return &bridgeFixture{syncFixture: f, mediaID: f.serverCopy(setID, ep1, "1.mp3"), ep1: ep1}
}

// at is a UTC time ms milliseconds after the epoch.
func at(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// ep1 returns alice's registers of ep1.
func (f *bridgeFixture) aliceEp1() episodeRegisters {
	f.t.Helper()
	r, _ := f.state(f.alice, testFeedKey, testEp1Key)
	return r
}

func TestBridgePositionSavesAndFinishUseEpisodeRegisters(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()
	f.episodes(f.alice, compItem("done", testFeedKey, testEp1Key, true, "manual", 5000))
	// A position save older than the completion but newer than the
	// position register applies and leaves the completion alone.
	f.must(f.s.UpsertProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: f.mediaID, PositionSeconds: 100, AccumulatedSeconds: 70, UpdatedAt: at(3000)}))
	if r := f.aliceEp1(); r.position != 100 || r.positionMS != 3000 || !r.completed {
		t.Fatalf("position save: %+v", r)
	}
	m := f.mirror(f.alice, f.mediaID)
	if m.PositionSeconds != 100 || !m.Finished || m.AccumulatedSeconds != 70 || m.UpdatedAt.UnixMilli() != 5000 {
		t.Fatalf("mirror: %+v", m)
	}
	pos, err := f.s.PositionEditTime(ctx, f.alice, f.mediaID)
	f.must(err)
	latest, err := f.s.ProgressEditTime(ctx, f.alice, f.mediaID)
	f.must(err)
	if pos.UnixMilli() != 3000 || latest.UnixMilli() != 5000 {
		t.Fatalf("edit times: position %v, latest %v", pos, latest)
	}
	// A finish is a manual completion that keeps the position and starts a
	// new playback cycle; a stale finish changes nothing.
	f.episodes(f.alice, compItem("undo", testFeedKey, testEp1Key, false, "manual", 6000))
	f.must(f.s.FinishProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: f.mediaID, PositionSeconds: 600, Finished: true, UpdatedAt: at(7000)}))
	if r := f.aliceEp1(); !r.completed || r.completionSource != "manual" || r.position != 100 {
		t.Fatalf("finish: %+v", r)
	}
	if m := f.mirror(f.alice, f.mediaID); !m.Finished || m.AccumulatedSeconds != 0 || m.PositionSeconds != 100 {
		t.Fatalf("mirror after finish: %+v", m)
	}
	f.must(f.s.FinishProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: f.mediaID, Finished: true, UpdatedAt: at(6500)}))
	if r := f.aliceEp1(); r.completionMS != 7000 {
		t.Fatalf("stale finish applied: %+v", r)
	}
}

func TestBridgeResetAndPlaybackSync(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()
	sync := func(p model.SyncedProgress) *model.SyncedProgress {
		t.Helper()
		out, err := f.s.SyncPlayback(ctx, f.alice, f.mediaID, model.PlaybackSync{Progress: &p,
			Bookmarks: []model.SyncedBookmark{{Key: "b1", PositionSeconds: 9, ObservedMS: p.ObservedMS}}})
		f.must(err)
		if len(out.Bookmarks) != 1 {
			t.Fatalf("bookmarks: %+v", out.Bookmarks)
		}
		return out.Progress
	}
	// A natural end is an automatic completion with the end position.
	if got := sync(model.SyncedProgress{PositionSeconds: 598, Finished: true, ObservedMS: 2000}); !got.Finished || got.PositionSeconds != 598 || got.ObservedMS != 2000 {
		t.Fatalf("end: %+v", got)
	}
	if r := f.aliceEp1(); !r.completed || r.completionSource != "auto" {
		t.Fatalf("end registers: %+v", r)
	}
	// An old client replaying it saves a position: completed stays, the
	// answer projects unfinished so the client resumes at the position.
	if got := sync(model.SyncedProgress{PositionSeconds: 30, ObservedMS: 3000}); got.Finished || got.PositionSeconds != 30 || got.ObservedMS != 3000 {
		t.Fatalf("replay: %+v", got)
	}
	if r := f.aliceEp1(); !r.completed {
		t.Fatal("a position save uncompleted the episode")
	}
	// not_started: position 0 and a manual uncompletion, no reset barrier.
	f.must(f.s.ResetProgressAt(ctx, f.alice, f.mediaID, at(4000)))
	if r := f.aliceEp1(); r.completed || r.position != 0 || r.completionMS != 4000 || r.positionMS != 4000 {
		t.Fatalf("reset: %+v", r)
	}
	if m := f.mirror(f.alice, f.mediaID); m.Finished || m.PositionSeconds != 0 || m.AccumulatedSeconds != 0 {
		t.Fatalf("mirror after reset: %+v", m)
	}
	var resets int
	f.must(f.s.db.QueryRow(`SELECT COUNT(*) FROM playback_resets`).Scan(&resets))
	if resets != 0 {
		t.Fatal("reset of an episode copy wrote a media reset barrier")
	}
	f.must(f.s.ResetProgressAt(ctx, f.alice, f.mediaID, at(3500)))
	if got := sync(model.SyncedProgress{PositionSeconds: 1, ObservedMS: 3900}); got.PositionSeconds != 0 || got.ObservedMS != 4000 {
		t.Fatalf("stale edits after the reset: %+v", got)
	}
}

// TestBridgeClampsLegacyClocks covers contract §4 rule 4 for the legacy
// routes: a clock far in the future (accepted up to year 9999 by the media
// routes) is stored at the server's time, so it cannot block later edits.
func TestBridgeClampsLegacyClocks(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()
	future := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	before := time.Now().UnixMilli()
	_, err := f.s.SyncPlayback(ctx, f.alice, f.mediaID, model.PlaybackSync{Progress: &model.SyncedProgress{PositionSeconds: 50, ObservedMS: future}})
	f.must(err)
	f.must(f.s.FinishProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: f.mediaID, Finished: true, UpdatedAt: at(future)}))
	after := time.Now().UnixMilli()
	r := f.aliceEp1()
	for name, ms := range map[string]int64{"position": r.positionMS, "completion": r.completionMS} {
		if ms < before || ms > after {
			t.Fatalf("%s clock %d not clamped to [%d, %d]", name, ms, before, after)
		}
	}
	// A device with a correct clock can edit again right away.
	f.episodes(f.alice, posItem("next", testFeedKey, testEp1Key, 60, after+1000))
	if r := f.aliceEp1(); r.position != 60 {
		t.Fatalf("a clamped clock blocked a later edit: %+v", r)
	}
}

func TestToggleEpisodeCompletionIsOneManualEdit(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()
	// A stored clock ahead of the server: the toggle still wins.
	f.episodes(f.alice, posItem("pos", testFeedKey, testEp1Key, 120, 1000), compItem("c", testFeedKey, testEp1Key, false, "auto", f.now.UnixMilli()+60000))
	completed, err := f.s.ToggleEpisodeCompletion(ctx, f.alice, f.ep1, f.now)
	f.must(err)
	r := f.aliceEp1()
	if !completed || !r.completed || r.completionSource != "manual" || r.completionMS != f.now.UnixMilli()+60001 || r.position != 120 {
		t.Fatalf("toggle: %v %+v", completed, r)
	}
	if m := f.mirror(f.alice, f.mediaID); !m.Finished || m.PositionSeconds != 120 {
		t.Fatalf("toggle did not rewrite the mirror: %+v", m)
	}
	if completed, err = f.s.ToggleEpisodeCompletion(ctx, f.alice, f.ep1, f.now); err != nil || completed {
		t.Fatalf("second toggle: %v %v", completed, err)
	}
	var legacy int
	f.must(f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_status`).Scan(&legacy))
	if legacy != 0 {
		t.Fatal("the toggle wrote podcast_status")
	}
	if _, err := f.s.ToggleEpisodeCompletion(ctx, f.alice, 9999, f.now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing episode: %v", err)
	}
}

func TestServerCopyLifecycleKeepsOneHistory(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID, _, ep2 := f.catalogEpisodes()
	// State recorded before any server copy exists (e.g. from Android).
	f.episodes(f.alice, posItem("p", testFeedKey, testEp2Key, 42, 1000))
	mediaID := f.serverCopy(setID, ep2, "2.mp3")
	if m := f.mirror(f.alice, mediaID); m == nil || m.PositionSeconds != 42 || m.Finished {
		t.Fatalf("download did not seed the mirror: %+v", m)
	}
	if m := f.mirror(f.bob, mediaID); m != nil {
		t.Fatalf("mirror for a user without state: %+v", m)
	}
	// Deleting the copy removes only the mirror; the state stays.
	f.must(f.s.HardDeleteMedia(ctx, mediaID))
	if r, _ := f.state(f.alice, testFeedKey, testEp2Key); r.position != 42 {
		t.Fatalf("state lost with the server copy: %+v", r)
	}
	if m := f.mirror(f.alice, mediaID); m != nil {
		t.Fatalf("mirror outlived its media: %+v", m)
	}
}

// TestOrdinaryMediaKeepsMediaScopedProgress pins that media which is no
// episode copy keeps the original rules: one progress register, a finish
// moving the position, a reset barrier, and no episode state.
func TestOrdinaryMediaKeepsMediaScopedProgress(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID, err := f.s.CreateSet(ctx, &model.Set{Name: "music", RootPath: "music", CreatedAt: f.now})
	f.must(err)
	mediaID, err := f.s.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "a.mp3", FileName: "a.mp3", Type: model.MediaTypeAudio, Duration: 300, CreatedAt: f.now})
	f.must(err)
	_, err = f.s.SyncPlayback(ctx, f.alice, mediaID, model.PlaybackSync{Progress: &model.SyncedProgress{PositionSeconds: 40, ObservedMS: 1000}})
	f.must(err)
	f.must(f.s.FinishProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: mediaID, PositionSeconds: 300, Finished: true, UpdatedAt: at(2000)}))
	f.must(f.s.FinishProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: mediaID, PositionSeconds: 1, Finished: true, UpdatedAt: at(1500)}))
	if m := f.mirror(f.alice, mediaID); !m.Finished || m.PositionSeconds != 300 {
		t.Fatalf("finish: %+v", m)
	}
	f.must(f.s.ResetProgressAt(ctx, f.alice, mediaID, at(3000)))
	out, err := f.s.SyncPlayback(ctx, f.alice, mediaID, model.PlaybackSync{Progress: &model.SyncedProgress{PositionSeconds: 9, ObservedMS: 2500}})
	f.must(err)
	if out.Progress == nil || out.Progress.ObservedMS != 3000 || out.Progress.PositionSeconds != 0 || f.mirror(f.alice, mediaID) != nil {
		t.Fatalf("edit older than the reset: %+v", out.Progress)
	}
	if latest, err := f.s.PositionEditTime(ctx, f.alice, mediaID); err != nil || latest.UnixMilli() != 3000 {
		t.Fatalf("position edit time: %v %v", latest, err)
	}
	var states int
	f.must(f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_episode_state`).Scan(&states))
	if states != 0 {
		t.Fatalf("ordinary media wrote %d episode state rows", states)
	}
}
