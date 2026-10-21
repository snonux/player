package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// syncFixture is a store with two users and helpers for subscription sync.
type syncFixture struct {
	t          *testing.T
	s          *SQLite
	alice, bob int64
	now        time.Time
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	s := newTestStore(t)
	t.Cleanup(func() { _ = s.Close() })
	f := &syncFixture{t: t, s: s, now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	for _, name := range []string{"alice", "bob"} {
		id, err := s.CreateUser(context.Background(), &model.User{Username: name, PasswordHash: "h", CreatedAt: f.now})
		if err != nil {
			t.Fatal(err)
		}
		if name == "alice" {
			f.alice = id
		} else {
			f.bob = id
		}
	}
	return f
}

// edit builds a subscription edit with a 16+ byte edit id.
func edit(id, key string, subscribed bool, ms int64) model.PodcastSubscriptionEdit {
	return model.PodcastSubscriptionEdit{EditID: fmt.Sprintf("edit-%012s", id), FeedKey: key,
		Subscribed: subscribed, ObservedMS: ms, URLs: []string{}}
}

// sync runs one request for user with the store's current epoch.
func (f *syncFixture) sync(user int64, since int64, limit int, edits ...model.PodcastSubscriptionEdit) *model.PodcastSyncResponse {
	f.t.Helper()
	epoch, err := podcastSyncEpoch(context.Background(), f.s.db)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := f.s.SyncPodcastSubscriptions(context.Background(), user, model.PodcastSubscriptionSync{
		Epoch: epoch, Since: since, Limit: limit, Edits: edits, Now: f.now, CheckIntervalMinutes: 60})
	if err != nil {
		f.t.Fatalf("sync: %v", err)
	}
	return out
}

// sub returns the user's stored register for key.
func (f *syncFixture) sub(user int64, key string) *subscriptionRow {
	f.t.Helper()
	row, err := getSubscription(context.Background(), f.s.db, user, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return row
}

func (f *syncFixture) countFeeds(key string) int {
	f.t.Helper()
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_feeds WHERE feed_key = ?`, key).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestSyncSubscribeCreatesRegisterAndCatalogFeed(t *testing.T) {
	f := newSyncFixture(t)
	e := edit("a1", "feed:example.com/rss", true, 1000)
	e.FetchURL, e.Title, e.URLs = "https://example.com/rss", "Example", []string{"http://example.com/rss"}
	out := f.sync(f.alice, 0, 0, e)
	if len(out.Acked) != 1 || out.Acked[0] != e.EditID || len(out.Rejected) != 0 {
		t.Fatalf("ack: %+v", out)
	}
	if len(out.Subscriptions) != 1 || !out.Subscriptions[0].Subscribed || out.Subscriptions[0].SubscriptionMS != 1000 {
		t.Fatalf("returned register: %+v", out.Subscriptions)
	}
	feed, err := f.s.GetFeedByKey(context.Background(), "feed:example.com/rss")
	if err != nil || feed == nil {
		t.Fatalf("catalog feed: %v %v", feed, err)
	}
	if feed.FeedURL != "https://example.com/rss" || feed.CreatedBy == nil || *feed.CreatedBy != f.alice ||
		feed.AutoDownload || feed.LastCheckedAt != nil || feed.Title != "Example" {
		t.Fatalf("catalog feed fields: %+v", feed)
	}
	set, err := f.s.GetSetByID(context.Background(), feed.SetID)
	if err != nil || set == nil || !set.IsPodcast || set.RootPath != "podcast" {
		t.Fatalf("podcast set: %+v %v", set, err)
	}
	perms, err := f.s.ListPermissionsByUser(context.Background(), f.alice)
	if err != nil || len(perms) != 0 {
		t.Fatalf("subscribing granted permissions: %+v %v", perms, err)
	}
}

func TestSyncRegisterMergeRules(t *testing.T) {
	f := newSyncFixture(t)
	key := "feed:example.com/rss"
	f.sync(f.alice, 0, 0, edit("a1", key, true, 2000))
	seq := f.sub(f.alice, key).changeSeq

	// A replayed edit (equal clock) and an older delayed edit are acked
	// but change nothing, not even the change sequence.
	out := f.sync(f.alice, 0, 0, edit("a1", key, true, 2000), edit("old", key, false, 1500))
	if len(out.Acked) != 2 {
		t.Fatalf("acked: %+v", out.Acked)
	}
	if row := f.sub(f.alice, key); !row.Subscribed || row.SubscriptionMS != 2000 || row.changeSeq != seq {
		t.Fatalf("stale edits changed the register: %+v", row)
	}
	if f.countFeeds(key) != 1 {
		t.Fatal("replay created a second catalog row")
	}

	// A newer unsubscribe is a tombstone; a stale subscribe cannot
	// resurrect it, a newer one can.
	f.sync(f.alice, 0, 0, edit("del", key, false, 3000))
	f.sync(f.alice, 0, 0, edit("stale", key, true, 2500))
	if row := f.sub(f.alice, key); row.Subscribed || row.SubscriptionMS != 3000 {
		t.Fatalf("tombstone resurrected: %+v", row)
	}
	out = f.sync(f.alice, 0, 0, edit("again", key, true, 3001))
	if row := f.sub(f.alice, key); !row.Subscribed || row.SubscriptionMS != 3001 {
		t.Fatalf("newer subscribe lost: %+v", row)
	}
	if len(out.Subscriptions) != 1 || out.Subscriptions[0].SubscriptionMS != 3001 {
		t.Fatalf("edited register not returned: %+v", out.Subscriptions)
	}
}

func TestSyncRejectsUnknownLegacyKeyAndAcceptsKnownOne(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID, err := f.s.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	id, err := f.s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "https://user:pw@example.com/rss", CreatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf("legacy-feed:%d", id)
	out := f.sync(f.alice, 0, 0, edit("unknown", "legacy-feed:999", true, 1000), edit("known", legacy, true, 1000))
	if len(out.Rejected) != 1 || out.Rejected[0].Reason != model.PodcastRejectInvalidKey || out.Rejected[0].EditID != edit("unknown", "", true, 0).EditID {
		t.Fatalf("rejections: %+v", out.Rejected)
	}
	if len(out.Acked) != 1 || f.sub(f.alice, legacy) == nil {
		t.Fatalf("known legacy key not applied: %+v", out)
	}
	if f.sub(f.alice, "legacy-feed:999") != nil {
		t.Fatal("unknown legacy key stored")
	}
}

func TestSyncResolvesAliasesAndURLs(t *testing.T) {
	f := newSyncFixture(t)
	canonical := "feed:example.com/rss"
	f.sync(f.bob, 0, 0, edit("b1", canonical, true, 1000))
	if _, err := f.s.db.Exec(`INSERT INTO podcast_feed_aliases (alias_key, feed_key, change_seq) VALUES ('feed:old.example.com/rss', ?, 0)`, canonical); err != nil {
		t.Fatal(err)
	}
	byAlias := edit("alias", "feed:old.example.com/rss", true, 1000)
	byURL := edit("url", "feed:unknown.example.com/rss", true, 1000)
	byURL.URLs = []string{"https://EXAMPLE.com/rss"}
	out := f.sync(f.alice, 0, 0, byAlias, byURL)
	want := []model.PodcastKeyMapping{
		{Kind: "feed", From: "feed:old.example.com/rss", To: canonical},
		{Kind: "feed", From: "feed:unknown.example.com/rss", To: canonical},
	}
	if fmt.Sprint(out.Resolved) != fmt.Sprint(want) {
		t.Fatalf("resolved = %+v, want %+v", out.Resolved, want)
	}
	if f.sub(f.alice, canonical) == nil || f.sub(f.alice, "feed:old.example.com/rss") != nil {
		t.Fatal("register not kept under the canonical key")
	}
	if f.countFeeds(canonical) != 1 || f.countFeeds("feed:unknown.example.com/rss") != 0 {
		t.Fatal("resolution created catalog rows")
	}
	// An unknown key stays a state-only key of that user.
	f.sync(f.alice, 0, 0, edit("solo", "feed:solo.example.com/rss", false, 1000))
	if f.sub(f.alice, "feed:solo.example.com/rss") == nil || f.countFeeds("feed:solo.example.com/rss") != 0 {
		t.Fatal("unsubscribed unknown key must be stored without a catalog row")
	}
}

func TestSyncQuotaRejectsNewRowsOnly(t *testing.T) {
	f := newSyncFixture(t)
	if _, err := f.s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
INSERT INTO podcast_subscriptions (user_id, feed_key, subscribed, subscription_ms, change_seq)
SELECT ?, 'feed:q' || i || '.example.com/rss', 0, 1, 0 FROM n`, MaxPodcastSubscriptionsPerUser, f.alice); err != nil {
		t.Fatal(err)
	}
	out := f.sync(f.alice, 0, 0, edit("new", "feed:new.example.com/rss", true, 5), edit("old", "feed:q1.example.com/rss", true, 5))
	if len(out.Rejected) != 1 || out.Rejected[0].Reason != model.PodcastRejectQuota || len(out.Acked) != 1 {
		t.Fatalf("quota: %+v", out)
	}
	// Another user's quota is separate.
	if out := f.sync(f.bob, 0, 0, edit("bob", "feed:new.example.com/rss", true, 5)); len(out.Acked) != 1 {
		t.Fatalf("bob blocked by alice's quota: %+v", out)
	}
}

func TestSyncPullPagesAccountIsolationAndAliases(t *testing.T) {
	f := newSyncFixture(t)
	keys := []string{"feed:a.example.com/rss", "feed:b.example.com/rss", "feed:c.example.com/rss"}
	// One request writes three rows; pages of two cut inside it.
	var edits []model.PodcastSubscriptionEdit
	for i, k := range keys {
		edits = append(edits, edit(fmt.Sprint("p", i), k, true, int64(100+i)))
	}
	f.sync(f.alice, 0, 0, edits...)
	f.sync(f.bob, 0, 0, edit("bob", "feed:bob.example.com/rss", true, 1))

	first := f.sync(f.alice, 0, 2)
	if len(first.Subscriptions) != 2 || !first.HasMore {
		t.Fatalf("first page: %+v", first)
	}
	second := f.sync(f.alice, first.NextSince, 2)
	if len(second.Subscriptions) != 1 || second.HasMore || second.Subscriptions[0].FeedKey != keys[2] {
		t.Fatalf("second page: %+v", second)
	}
	for _, page := range []*model.PodcastSyncResponse{first, second} {
		for _, sub := range page.Subscriptions {
			if sub.FeedKey == "feed:bob.example.com/rss" {
				t.Fatal("another user's subscription leaked")
			}
		}
	}
	// An alias written before bob's cursor reaches him with his row; it
	// is invisible to alice, who has no row for that feed.
	if _, err := f.s.db.Exec(`INSERT INTO podcast_feed_aliases (alias_key, feed_key, change_seq) VALUES ('feed:old.bob.example.com/rss', 'feed:bob.example.com/rss', 1)`); err != nil {
		t.Fatal(err)
	}
	if out := f.sync(f.alice, 0, 0); len(out.Aliases) != 0 {
		t.Fatalf("alias leaked to alice: %+v", out.Aliases)
	}
	out := f.sync(f.bob, 0, 0)
	if len(out.Aliases) != 1 || out.Aliases[0].From != "feed:old.bob.example.com/rss" {
		t.Fatalf("late subscriber missed the alias: %+v", out.Aliases)
	}
}

func TestSyncEpochOrCursorResetPullsFromZero(t *testing.T) {
	f := newSyncFixture(t)
	f.sync(f.alice, 0, 0, edit("a", "feed:a.example.com/rss", true, 1))
	ctx := context.Background()
	highest, err := maxChangeSeq(ctx, f.s.db)
	if err != nil {
		t.Fatal(err)
	}
	if out := f.sync(f.alice, highest, 0); len(out.Subscriptions) != 0 || out.NextSince != highest {
		t.Fatalf("caught-up pull: %+v", out)
	}
	if out := f.sync(f.alice, highest+50, 0); len(out.Subscriptions) != 1 {
		t.Fatalf("cursor beyond the server must restart: %+v", out)
	}
	out, err := f.s.SyncPodcastSubscriptions(ctx, f.alice, model.PodcastSubscriptionSync{Epoch: "other", Since: highest, Now: f.now})
	if err != nil || len(out.Subscriptions) != 1 || out.Epoch == "other" || len(out.Epoch) != 32 {
		t.Fatalf("foreign epoch must restart: %+v %v", out, err)
	}
}

func TestSyncCatalogCreationRateLimit(t *testing.T) {
	f := newSyncFixture(t)
	var edits []model.PodcastSubscriptionEdit
	for i := 0; i <= MaxCatalogFeedsPerUserHour; i++ {
		edits = append(edits, edit(fmt.Sprint("r", i), fmt.Sprintf("feed:r%d.example.com/rss", i), true, 1))
	}
	out := f.sync(f.alice, 0, 0, edits...)
	if len(out.Acked) != len(edits) {
		t.Fatalf("subscriptions must be acked even when no catalog row is made: %+v", out.Rejected)
	}
	last := fmt.Sprintf("feed:r%d.example.com/rss", MaxCatalogFeedsPerUserHour)
	if f.countFeeds(last) != 0 || f.countFeeds("feed:r0.example.com/rss") != 1 {
		t.Fatal("hourly catalog limit not applied")
	}
	// An hour later a retry of the subscription creates the missing row.
	f.now = f.now.Add(61 * time.Minute)
	f.sync(f.alice, 0, 0, edits[len(edits)-1])
	if f.countFeeds(last) != 1 {
		t.Fatal("retried subscribe did not create the catalog row")
	}
}

func TestSetPodcastSubscriptionAlwaysWins(t *testing.T) {
	f := newSyncFixture(t)
	key := "feed:example.com/rss"
	// A device clock ahead of the server: the server edit still wins.
	future := f.now.Add(time.Minute).UnixMilli()
	f.sync(f.alice, 0, 0, edit("dev", key, false, future))
	feed := &model.PodcastFeed{FeedKey: key, FeedURL: "https://example.com/rss", Title: "T"}
	if err := f.s.SetPodcastSubscription(context.Background(), f.alice, feed, true, f.now); err != nil {
		t.Fatal(err)
	}
	row := f.sub(f.alice, key)
	if !row.Subscribed || row.SubscriptionMS != future+1 || len(row.URLs) != 1 {
		t.Fatalf("server edit: %+v", row)
	}
	states, err := f.s.PodcastSubscriptionStates(context.Background(), f.alice)
	if err != nil || !states[key] {
		t.Fatalf("states: %v %v", states, err)
	}
	if active, err := f.s.HasActivePodcastSubscription(context.Background(), key); err != nil || !active {
		t.Fatalf("active: %v %v", active, err)
	}
	if active, _ := f.s.HasActivePodcastSubscription(context.Background(), "feed:none/"); active {
		t.Fatal("unknown key reported active")
	}
}

func TestDeleteUserRemovesSyncRows(t *testing.T) {
	f := newSyncFixture(t)
	f.sync(f.alice, 0, 0, edit("a", "feed:a.example.com/rss", true, 1))
	if err := f.s.DeleteUser(context.Background(), f.alice); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM podcast_subscriptions WHERE user_id = ?`, f.alice).Scan(&n); err != nil || n != 0 {
		t.Fatalf("subscriptions of a deleted user: %d %v", n, err)
	}
}

func TestActiveSubscriptionMatchesAliasKeys(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	canonical := "feed:example.com/rss"
	f.sync(f.bob, 0, 0, edit("b", canonical, true, 1))
	f.sync(f.bob, 0, 0, edit("b2", canonical, false, 2)) // bob leaves; the row stays
	// Alice's subscription is kept under a key that later became an alias.
	f.sync(f.alice, 0, 0, edit("a", "feed:old.example.com/rss", true, 1))
	if active, _ := f.s.HasActivePodcastSubscription(ctx, canonical); active {
		t.Fatal("no subscriber yet under the canonical key or an alias")
	}
	if _, err := f.s.db.Exec(`INSERT INTO podcast_feed_aliases (alias_key, feed_key, change_seq) VALUES ('feed:old.example.com/rss', ?, 0)`, canonical); err != nil {
		t.Fatal(err)
	}
	if active, err := f.s.HasActivePodcastSubscription(ctx, canonical); err != nil || !active {
		t.Fatalf("subscription under an alias not seen: %v %v", active, err)
	}
	feeds, err := f.s.ListFeedsNeedingCheck(ctx, f.now.Add(time.Hour), f.now.Add(time.Hour))
	found := false
	for _, feed := range feeds {
		found = found || feed.FeedKey == canonical
	}
	if err != nil || !found {
		t.Fatalf("feed with an alias subscriber not checked: %+v %v", feeds, err)
	}
}
