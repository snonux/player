package repository

import (
	"context"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"testing"
	"time"
)

func TestPodcastRetentionBoundaryAndHistory(t *testing.T) {
	f := newSyncFixture(t)
	clk := &clock.MockClock{T: f.now}
	f.s = f.s.WithClock(clk)
	set, ep, _ := f.catalogEpisodes()
	media := f.serverCopy(set, ep, "one.mp3")
	f.episodes(f.alice, posItem("p", testFeedKey, testEp1Key, 37, 10), compItem("c", testFeedKey, testEp1Key, true, "manual", 20))
	f.episodes(f.bob, compItem("b", testFeedKey, testEp1Key, true, "manual", 20))
	before := f.mirror(f.alice, media)
	clk.T = clk.T.Add(PodcastRetentionAge - time.Millisecond)
	f.must(f.s.retainPodcastEpisodes(context.Background(), f.alice))
	if r, _ := f.state(f.alice, testFeedKey, testEp1Key); r.deleted {
		t.Fatal("expired early")
	}
	f.episodes(f.alice, compItem("stale", testFeedKey, testEp1Key, true, "manual", 20))
	clk.T = clk.T.Add(time.Millisecond)
	out, err := f.s.SyncPodcast(context.Background(), f.alice, model.PodcastSyncBatch{Now: clk.T})
	f.must(err)
	if len(out.Episodes) != 1 || out.Episodes[0].Deletion == nil || !out.Episodes[0].Deletion.Deleted {
		t.Fatalf("missing tombstone: %+v", out.Episodes)
	}
	r, seq := f.state(f.alice, testFeedKey, testEp1Key)
	if !r.deleted || !r.completed || r.position != 37 {
		t.Fatalf("registers: %+v", r)
	}
	if after := f.mirror(f.alice, media); *after != *before {
		t.Fatalf("history changed: %+v -> %+v", before, after)
	}
	var count int
	f.must(f.s.db.QueryRow("SELECT COUNT(*) FROM media WHERE id=?", media).Scan(&count))
	if count != 1 {
		t.Fatal("removed shared media")
	}
	if r, _ := f.state(f.bob, testFeedKey, testEp1Key); r.deleted {
		t.Fatal("cross-account deletion")
	}
	f.must(f.s.retainPodcastEpisodes(context.Background(), f.alice))
	_, after := f.state(f.alice, testFeedKey, testEp1Key)
	if seq != after {
		t.Fatal("repeat changed sequence")
	}
}

func TestPodcastRetentionGuardsAndRecompletion(t *testing.T) {
	for _, test := range []struct {
		name string
		edit model.PodcastEpisodeEdit
	}{
		{"uncompleted", compItem("u", testFeedKey, testEp1Key, false, "manual", 30)},
		{"replayed", posItem("r", testFeedKey, testEp1Key, 10, 30)},
		{"restored", delItem("restore", testFeedKey, testEp1Key, false, 30)},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newSyncFixture(t)
			clk := &clock.MockClock{T: f.now}
			f.s = f.s.WithClock(clk)
			f.catalogEpisodes()
			f.episodes(f.alice, compItem("c", testFeedKey, testEp1Key, true, "manual", 20))
			f.episodes(f.alice, test.edit)
			clk.T = clk.T.Add(PodcastRetentionAge)
			f.must(f.s.RetainPodcastEpisodes(context.Background()))
			if r, _ := f.state(f.alice, testFeedKey, testEp1Key); r.deleted {
				t.Fatal("guard ignored")
			}
			f.episodes(f.alice, compItem("again", testFeedKey, testEp1Key, true, "manual", 40))
			clk.T = clk.T.Add(PodcastRetentionAge - time.Millisecond)
			f.must(f.s.RetainPodcastEpisodes(context.Background()))
			if r, _ := f.state(f.alice, testFeedKey, testEp1Key); r.deleted {
				t.Fatal("fresh grace lost")
			}
			clk.T = clk.T.Add(time.Millisecond)
			f.must(f.s.RetainPodcastEpisodes(context.Background()))
			if r, _ := f.state(f.alice, testFeedKey, testEp1Key); !r.deleted {
				t.Fatal("recompletion did not expire")
			}
		})
	}
}

func TestPodcastRetentionListAndCancellation(t *testing.T) {
	f, _, _ := viewFixture(t)
	clk := &clock.MockClock{T: f.now}
	f.s = f.s.WithClock(clk)
	feed := "feed:a.example.com/rss"
	f.episodes(f.alice, compItem("c", feed, "guid:a2", true, "auto", 20))
	clk.T = clk.T.Add(PodcastRetentionAge)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.s.RetainPodcastEpisodes(ctx); err == nil {
		t.Fatal("cancelled sweep succeeded")
	}
	if r, _ := f.state(f.alice, feed, "guid:a2"); r.deleted {
		t.Fatal("cancelled sweep committed")
	}
	views, err := f.s.ListSubscribedEpisodes(context.Background(), f.alice, PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10})
	f.must(err)
	if titles(views) != "[a3 a1]" {
		t.Fatalf("unexpected list: %s", titles(views))
	}
}

func TestPodcastRetentionConcurrentRestoreWins(t *testing.T) {
	f := newSyncFixture(t)
	clk := &clock.MockClock{T: f.now}
	f.s = f.s.WithClock(clk)
	f.catalogEpisodes()
	f.episodes(f.alice, compItem("complete", testFeedKey, testEp1Key, true, "manual", 20))
	clk.T = clk.T.Add(PodcastRetentionAge)
	errs := make(chan error, 2)
	go func() { errs <- f.s.RetainPodcastEpisodes(context.Background()) }()
	go func() {
		_, err := f.s.SyncPodcast(context.Background(), f.alice, model.PodcastSyncBatch{Now: clk.T, Episodes: []model.PodcastEpisodeEdit{delItem("restore", testFeedKey, testEp1Key, false, clk.T.UnixMilli()+1)}})
		errs <- err
	}()
	for range 2 {
		f.must(<-errs)
	}
	f.must(f.s.RetainPodcastEpisodes(context.Background()))
	if r, _ := f.state(f.alice, testFeedKey, testEp1Key); r.deleted {
		t.Fatal("retention defeated newer concurrent restore")
	}
}
