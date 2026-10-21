package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestDeferredPodcastCatalogQuotaSurvivesDeletion(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	var edits []model.PodcastSubscriptionEdit
	for i := 0; i < MaxCatalogFeedsPerUserHour+2; i++ {
		edits = append(edits, edit(fmt.Sprintf("backlog-%d", i), fmt.Sprintf("feed:backlog.example.com/%d", i), true, 1))
	}
	f.sync(f.alice, 0, 0, edits...)
	deferred, canceled := edits[len(edits)-2], edits[len(edits)-1]
	if f.countFeeds(deferred.FeedKey) != 0 {
		t.Fatal("quota did not defer catalog creation")
	}
	// Deleting a created row cannot buy another slot in the same hour.
	created := edits[0]
	f.sync(f.alice, 0, 0, edit("remove", created.FeedKey, false, 2), edit("cancel", canceled.FeedKey, false, 2))
	feed, err := f.s.GetFeedByKey(ctx, created.FeedKey)
	f.must(err)
	f.must(f.s.DeleteFeed(ctx, feed.ID))
	f.must(f.s.CreateDeferredPodcastFeeds(ctx, f.now.Add(59*time.Minute), 15))
	if f.countFeeds(deferred.FeedKey) != 0 {
		t.Fatal("catalog deletion reset the hourly quota")
	}
	// Another account has its own budget; its row must not change Alice's
	// registers or authorize access to any server media.
	f.sync(f.bob, 0, 0, edit("bob", "feed:bob.example.com/rss", true, 1))
	if f.countFeeds("feed:bob.example.com/rss") != 1 {
		t.Fatal("quota leaked between accounts")
	}
	f.must(f.s.CreateDeferredPodcastFeeds(ctx, f.now.Add(time.Hour), 15))
	feed, err = f.s.GetFeedByKey(ctx, deferred.FeedKey)
	f.must(err)
	if feed == nil || feed.CreatedBy == nil || *feed.CreatedBy != f.alice || feed.AutoDownload || feed.CheckIntervalMinutes != 15 {
		t.Fatalf("deferred catalog row: %+v", feed)
	}
	if f.countFeeds(canceled.FeedKey) != 0 || f.countFeeds(created.FeedKey) != 0 {
		t.Fatal("background creation resurrected a tombstone")
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM set_permissions`); n != 0 {
		t.Fatalf("background catalog creation granted %d permissions", n)
	}
	f.must(f.s.CreateDeferredPodcastFeeds(ctx, f.now.Add(time.Hour), 15))
	if f.countFeeds(deferred.FeedKey) != 1 {
		t.Fatal("backlog processing is not idempotent")
	}
}

func TestDeleteFeedChecksSubscriptionAndAllCatalogRows(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	old, ids := f.catalogFeed(setID, oldFeedURL, "ep")
	target, _ := f.catalogFeed(setID, newFeedURL, "other")
	f.sync(f.alice, 0, 0, edit("subscribe", oldFeedKey, true, 10))
	_, err := f.s.MoveFeed(ctx, old.ID, newFeedURL)
	f.must(err)
	f.must(f.s.DeleteFeed(ctx, target.ID))
	if got, _ := f.s.GetFeedByID(ctx, old.ID); got == nil {
		t.Fatal("delete lost a subscribed catalog's merged row")
	}
	f.sync(f.alice, 0, 0, edit("unsubscribe", oldFeedKey, false, 20))
	f.must(f.s.DeleteFeed(ctx, target.ID))
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_feeds WHERE feed_key = ?`, newFeedKey); n != 0 {
		t.Fatal("delete left duplicate catalog rows")
	}
	if got, _ := f.s.GetEpisodeByID(ctx, ids["ep"]); got != nil {
		t.Fatal("delete left merged-row episodes")
	}
	if sub := f.sub(f.alice, newFeedKey); sub == nil || sub.Subscribed {
		t.Fatalf("delete changed subscription state: %+v", sub)
	}
	// Subscribing through the retained alias recreates the canonical row.
	f.sync(f.alice, 0, 0, edit("again", oldFeedKey, true, 30))
	if f.countFeeds(newFeedKey) != 1 {
		t.Fatal("retained alias prevented catalog recreation")
	}
}
