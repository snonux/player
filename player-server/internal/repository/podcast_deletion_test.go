package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/snonux/player/internal/model"
)

func delItem(id, feed, episode string, deleted bool, ms int64) model.PodcastEpisodeEdit {
	return model.PodcastEpisodeEdit{FeedKey: feed, EpisodeKey: episode, Deletion: &model.PodcastDeletionEdit{EditID: fmt.Sprintf("del-%012s", id), Deleted: deleted, ObservedMS: ms}}
}

func TestEpisodeDeletionIndependentAndDurable(t *testing.T) {
	f, ids, setID := viewFixture(t)
	ctx := context.Background()
	feed := "feed:a.example.com/rss"
	media := f.serverCopy(setID, ids["a2"], "a2.mp3")
	f.episodes(f.alice, posItem("saved", feed, "guid:a2", 37, 10), compItem("done", feed, "guid:a2", true, "manual", 20))
	before := f.mirror(f.alice, media)
	out := f.episodes(f.alice, delItem("delete", feed, "guid:a2", true, 30))
	if len(out.Acked) != 1 || len(out.Episodes) != 1 || out.Episodes[0].Deletion == nil || !out.Episodes[0].Deletion.Deleted {
		t.Fatalf("deletion response: %+v", out)
	}
	after := f.mirror(f.alice, media)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("deletion altered playback history: %+v -> %+v", before, after)
	}
	f.episodes(f.alice, posItem("late", feed, "guid:a2", 45, 40), compItem("replay", feed, "guid:a2", false, "manual", 50), delItem("stale", feed, "guid:a2", false, 29), delItem("tie", feed, "guid:a2", false, 30))
	r, _ := f.state(f.alice, feed, "guid:a2")
	if !r.deleted || r.position != 45 || r.completed {
		t.Fatalf("independent registers: %+v", r)
	}
	views, err := f.s.ListSubscribedEpisodes(ctx, f.alice, PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10})
	f.must(err)
	if titles(views) != "[a3 a1]" {
		t.Fatalf("deleted episode visible: %s", titles(views))
	}
	// Another account retains its view of the shared catalog.
	f.sync(f.bob, 0, 0, edit("bob", feed, true, 1))
	views, err = f.s.ListSubscribedEpisodes(ctx, f.bob, PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10})
	f.must(err)
	if titles(views) != "[a3 a2 a1]" {
		t.Fatalf("deletion leaked to another account: %s", titles(views))
	}
	// Refreshed catalog metadata never clears the personal tombstone.
	f.must(execSQL(f, `UPDATE podcast_episodes SET title='refreshed' WHERE id=?`, ids["a2"]))
	views, err = f.s.ListSubscribedEpisodes(ctx, f.alice, PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10})
	f.must(err)
	if titles(views) != "[a3 a1]" {
		t.Fatal("refresh resurrected deleted episode")
	}
}

func TestEpisodeDeletionOnlyPullAndClockTie(t *testing.T) {
	f := newSyncFixture(t)
	setID, ep, _ := f.catalogEpisodes()
	media := f.serverCopy(setID, ep, "one.mp3")
	f.episodes(f.alice, delItem("false", testFeedKey, testEp1Key, false, 20))
	f.episodes(f.alice, delItem("true", testFeedKey, testEp1Key, true, 20))
	if r, _ := f.state(f.alice, testFeedKey, testEp1Key); !r.deleted {
		t.Fatal("deletion lost equal-clock tie")
	}
	out, err := f.s.SyncPodcast(context.Background(), f.alice, model.PodcastSyncBatch{Now: f.now})
	f.must(err)
	if len(out.Episodes) != 1 || out.Episodes[0].Deletion == nil || !out.Episodes[0].Deletion.Deleted || out.Episodes[0].Position != nil || out.Episodes[0].Completion != nil {
		t.Fatalf("deletion-only full pull: %+v", out.Episodes)
	}
	var count int
	f.must(f.s.db.QueryRow(`SELECT COUNT(*) FROM playback_progress WHERE user_id=? AND media_id=?`, f.alice, media).Scan(&count))
	if count != 0 {
		t.Fatal("deletion-only edit fabricated playback history")
	}
	f.episodes(f.alice, delItem("newer", testFeedKey, testEp1Key, false, 21))
	if r, _ := f.state(f.alice, testFeedKey, testEp1Key); r.deleted {
		t.Fatal("newer restore ignored")
	}
}

func TestPodcastDeletionMigrationPreservesOldRegisters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(context.Background(), &model.User{Username: "migration", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO podcast_episode_state (user_id,feed_key,episode_key,position_seconds,position_ms,completed,completion_source,completion_ms,change_seq) VALUES (?, ?, ?, 42, 10, 1, 'manual', 20, 1)`, user, testFeedKey, testEp1Key)
	if err != nil {
		t.Fatal(err)
	}
	// The preceding schema lacked the two additive deletion columns.
	_, err = s.db.Exec(`ALTER TABLE podcast_episode_state DROP COLUMN deleted`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`ALTER TABLE podcast_episode_state DROP COLUMN deletion_ms`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, _, err := loadRegisters(context.Background(), s.db, episodeStateKey{user, testFeedKey, testEp1Key})
	if err != nil || r.position != 42 || !r.completed || r.deleted || r.deletionMS != 0 {
		t.Fatalf("old registers changed: %+v %v", r, err)
	}
	for _, name := range []string{"deleted", "deletion_ms"} {
		ok, err := hasColumn(s.db, "podcast_episode_state", name)
		if err != nil || !ok {
			t.Fatalf("migration missing %s: %v", name, err)
		}
	}
}

func TestEpisodeDeletionFollowsCanonicalMerge(t *testing.T) {
	f := newSyncFixture(t)
	old := "feed:old.example.com/rss"
	f.episodes(f.alice, delItem("old", old, testEp1Key, true, 20), posItem("oldpos", old, testEp1Key, 15, 10))
	f.episodes(f.alice, delItem("target", testFeedKey, testEp1Key, false, 20), compItem("done", testFeedKey, testEp1Key, true, "manual", 30))
	ctx := context.Background()
	tx, err := f.s.db.BeginTx(ctx, nil)
	f.must(err)
	defer tx.Rollback()
	states, err := loadStatesOfFeed(ctx, tx, old)
	f.must(err)
	if len(states) != 1 {
		t.Fatal("source state missing")
	}
	f.must(moveEpisodeState(ctx, tx, states[0], episodeStateKey{f.alice, testFeedKey, testEp1Key}))
	f.must(tx.Commit())
	r, _ := f.state(f.alice, testFeedKey, testEp1Key)
	if !r.deleted || !r.completed || r.position != 15 || r.deletionMS != 20 {
		t.Fatalf("merge lost independent deletion: %+v", r)
	}
	if r, _ := f.state(f.alice, old, testEp1Key); r != (episodeRegisters{}) {
		t.Fatal("losing state survived")
	}
}
