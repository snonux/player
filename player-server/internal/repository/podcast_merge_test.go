package repository

import (
	"context"
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

const (
	oldFeedURL = "https://old.example.com/rss"
	oldFeedKey = "feed:old.example.com/rss"
	newFeedURL = "https://new.example.com/rss"
	newFeedKey = "feed:new.example.com/rss"
)

// podcastSet creates the shared podcast set.
func (f *syncFixture) podcastSet() int64 {
	f.t.Helper()
	setID, err := f.s.CreateSet(context.Background(), &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: f.now})
	f.must(err)
	return setID
}

// catalogFeed creates a catalog feed at url with one episode per GUID
// (enclosure https://cdn.example.com/<guid>.mp3) and returns the feed and
// the episode ids by GUID.
func (f *syncFixture) catalogFeed(setID int64, url string, guids ...string) (*model.PodcastFeed, map[string]int64) {
	f.t.Helper()
	ctx := context.Background()
	feed := &model.PodcastFeed{SetID: setID, FeedURL: url, Title: url, CreatedAt: f.now}
	id, err := f.s.CreateFeed(ctx, feed)
	f.must(err)
	feed.ID = id
	ids := map[string]int64{}
	for _, g := range guids {
		ids[g], err = f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: id, GUID: g, Title: g,
			EpisodeURL: "https://cdn.example.com/" + g + ".mp3", CreatedAt: f.now})
		f.must(err)
	}
	return feed, ids
}

// rowCount counts the rows a query selects.
func (f *syncFixture) rowCount(query string, args ...any) int {
	f.t.Helper()
	var n int
	f.must(f.s.db.QueryRow(query, args...).Scan(&n))
	return n
}

func TestFeedMergeTargetWinsAndMovesOnlyItsOwnSubscribers(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	old, _ := f.catalogFeed(setID, oldFeedURL, "shared", "only-old")
	target, _ := f.catalogFeed(setID, newFeedURL, "shared")
	// Alice listens to the old feed (and once unsubscribed from the new
	// one, earlier); Bob only to the new one.
	oldSub := edit("a1", oldFeedKey, true, 10)
	oldSub.URLs = []string{oldFeedURL}
	f.sync(f.alice, 0, 0, oldSub, edit("a2", newFeedKey, false, 3))
	f.sync(f.bob, 0, 0, edit("b1", newFeedKey, true, 5))
	f.episodes(f.alice, posItem("a3", oldFeedKey, "guid:shared", 100, 50), posItem("a4", oldFeedKey, "guid:only-old", 7, 60),
		compItem("a5", newFeedKey, "guid:shared", true, "manual", 40), posItem("a6", newFeedKey, "guid:shared", 20, 30))
	f.episodes(f.bob, posItem("b2", newFeedKey, "guid:shared", 9, 5))
	bobSub := f.sub(f.bob, newFeedKey)
	_, bobSeq := f.state(f.bob, newFeedKey, "guid:shared")
	rows := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_state`)

	if move, err := f.s.MoveFeed(ctx, old.ID, newFeedURL); move != FeedMerged || err != nil {
		t.Fatalf("move into another catalog feed: %v %v", move, err)
	}
	// The old row is merged into the target and keeps its episodes under
	// the target's key; the old key is an alias of the target.
	if got, _ := f.s.GetFeedByID(ctx, old.ID); got.MergedInto == nil || *got.MergedInto != target.ID || got.FeedKey != newFeedKey {
		t.Fatalf("merged row: %+v", got)
	}
	if got, _ := f.s.GetFeedByKey(ctx, oldFeedKey); got == nil || got.ID != target.ID {
		t.Fatalf("old key resolves to %+v", got)
	}
	// Alice's registers merged by clock: her old subscription (10) beats
	// the older tombstone (3); positions and completions merge per register.
	if sub := f.sub(f.alice, newFeedKey); sub == nil || !sub.Subscribed || sub.SubscriptionMS != 10 || len(sub.URLs) != 1 {
		t.Fatalf("alice's subscription: %+v", sub)
	}
	if f.sub(f.alice, oldFeedKey) != nil || f.rowCount(`SELECT COUNT(*) FROM podcast_episode_state WHERE feed_key = ?`, oldFeedKey) != 0 {
		t.Fatal("state left under the old key")
	}
	if r, _ := f.state(f.alice, newFeedKey, "guid:shared"); r.position != 100 || r.positionMS != 50 || !r.completed || r.completionMS != 40 {
		t.Fatalf("alice's merged episode: %+v", r)
	}
	if r, _ := f.state(f.alice, newFeedKey, "guid:only-old"); r.position != 7 {
		t.Fatalf("alice's old-only episode: %+v", r)
	}
	// Bob's registers of the target were not rewritten.
	if sub := f.sub(f.bob, newFeedKey); sub.changeSeq != bobSub.changeSeq || !sub.Subscribed {
		t.Fatalf("bob's subscription was rewritten: %+v → %+v", bobSub, sub)
	}
	if r, seq := f.state(f.bob, newFeedKey, "guid:shared"); seq != bobSeq || r.position != 9 {
		t.Fatalf("bob's episode was rewritten: %+v seq %d → %d", r, bobSeq, seq)
	}
	// A merge never adds a state row (quota), and a client learns the alias.
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_state`); n != rows-1 {
		t.Fatalf("state rows %d → %d", rows, n)
	}
	out := f.sync(f.alice, 0, 0)
	if !hasAlias(out.Aliases, model.PodcastKeyKindFeed, oldFeedKey, newFeedKey) {
		t.Fatalf("alias not pulled: %+v", out.Aliases)
	}
	// The old episodes are part of the target's catalog for resolution.
	tx, err := f.s.db.BeginTx(ctx, nil)
	f.must(err)
	known, err := loadKnownEpisodesByKey(ctx, tx, newFeedKey)
	f.must(err)
	_ = tx.Rollback()
	if len(known) != 2 {
		t.Fatalf("known episodes after the merge: %+v", known)
	}
}

// hasAlias reports whether aliases contain the mapping from → to of kind.
func hasAlias(aliases []model.PodcastKeyMapping, kind, from, to string) bool {
	for _, a := range aliases {
		if a.Kind == kind && a.From == from && a.To == to {
			return true
		}
	}
	return false
}

func TestFeedMergeMergesEpisodesTwoKeysDenote(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	// The old feed knows the episode as guid:old; the new feed saw its GUID
	// change and recorded guid:old as an alias of guid:new.
	old, oldIDs := f.catalogFeed(setID, oldFeedURL, "old")
	_, newIDs := f.catalogFeed(setID, newFeedURL, "new")
	f.must(execSQL(f, `INSERT INTO podcast_episode_aliases (feed_key, alias_key, episode_key, change_seq) VALUES (?, 'guid:old', 'guid:new', 0)`, newFeedKey))
	f.sync(f.alice, 0, 0, edit("a1", oldFeedKey, true, 10))
	f.episodes(f.alice, posItem("a2", oldFeedKey, "guid:old", 30, 50), compItem("a3", newFeedKey, "guid:new", true, "auto", 60))
	f.episodes(f.bob, posItem("b1", newFeedKey, "guid:new", 5, 70))
	mediaID := f.serverCopy(setID, newIDs["new"], "new.mp3")

	if move, err := f.s.MoveFeed(ctx, old.ID, newFeedURL); move != FeedMerged || err != nil {
		t.Fatalf("merge: %v %v", move, err)
	}
	// The older catalog entry (the old feed's row) wins: the new row takes
	// guid:old and guid:new becomes its alias.
	if got, _ := f.s.GetEpisodeByID(ctx, newIDs["new"]); got.EpisodeKey != "guid:old" {
		t.Fatalf("newer episode row: %+v", got)
	}
	if got, _ := f.s.GetEpisodeByID(ctx, oldIDs["old"]); got.EpisodeKey != "guid:old" {
		t.Fatalf("older episode row: %+v", got)
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_aliases WHERE feed_key = ? AND alias_key = 'guid:new' AND episode_key = 'guid:old'`, newFeedKey); n != 1 {
		t.Fatal("guid:new is no alias of guid:old")
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_aliases WHERE alias_key = 'guid:old'`); n != 0 {
		t.Fatal("the winning key is still an alias")
	}
	if r, _ := f.state(f.alice, newFeedKey, "guid:old"); r.position != 30 || !r.completed || r.completionSource != "auto" {
		t.Fatalf("alice's merged episode: %+v", r)
	}
	if r, _ := f.state(f.bob, newFeedKey, "guid:old"); r.position != 5 {
		t.Fatalf("bob's state did not follow its episode: %+v", r)
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_state WHERE episode_key = 'guid:new'`); n != 0 {
		t.Fatal("state left under the losing episode key")
	}
	// The server copy now projects the merged registers.
	if m := f.mirror(f.alice, mediaID); m == nil || m.PositionSeconds != 30 || !m.Finished {
		t.Fatalf("alice's mirror: %+v", m)
	}
	// A client still holding guid:new resolves to guid:old.
	out := f.episodes(f.alice, posItem("a4", newFeedKey, "guid:new", 40, 80))
	if len(out.Resolved) != 1 || out.Resolved[0].To != "guid:old" {
		t.Fatalf("resolution of the losing key: %+v", out.Resolved)
	}
}

func TestMoveFeedAliasPullsInStateOnlyKey(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	feed, _ := f.catalogFeed(setID, oldFeedURL, "ep")
	// Alice kept a subscription and episode state under the new location
	// before it had a catalog row (a skipped catalog creation).
	f.must(execSQL(f, `INSERT INTO podcast_subscriptions (user_id, feed_key, subscribed, subscription_ms, change_seq)
VALUES (?, ?, 1, 20, 0)`, f.alice, newFeedKey))
	f.episodes(f.alice, posItem("a1", newFeedKey, "guid:ep", 12, 30))
	f.sync(f.alice, 0, 0, edit("a2", oldFeedKey, false, 10))
	f.sync(f.bob, 0, 0, edit("b1", oldFeedKey, true, 10))

	if move, err := f.s.MoveFeed(ctx, feed.ID, newFeedURL); move != FeedMoved || err != nil {
		t.Fatalf("move: %v %v", move, err)
	}
	if got, _ := f.s.GetFeedByKey(ctx, newFeedKey); got == nil || got.ID != feed.ID || got.FeedKey != oldFeedKey {
		t.Fatalf("new location resolves to %+v", got)
	}
	if sub := f.sub(f.alice, oldFeedKey); sub == nil || !sub.Subscribed || sub.SubscriptionMS != 20 {
		t.Fatalf("alice's state-only subscription not merged: %+v", sub)
	}
	if f.sub(f.alice, newFeedKey) != nil {
		t.Fatal("subscription left under the alias")
	}
	if r, _ := f.state(f.alice, oldFeedKey, "guid:ep"); r.position != 12 {
		t.Fatalf("alice's state-only episode not moved: %+v", r)
	}
	if sub := f.sub(f.bob, oldFeedKey); sub == nil || sub.SubscriptionMS != 10 {
		t.Fatalf("bob's subscription changed: %+v", sub)
	}
}

func TestLegacyFeedKeyRetirement(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	legacy, _ := f.catalogFeed(setID, "https://user:secret@legacy.example.com/rss", "ep")
	legacyKey := legacy.FeedKey
	other, _ := f.catalogFeed(setID, newFeedURL, "ep")
	f.sync(f.alice, 0, 0, edit("a1", legacyKey, true, 10))
	f.episodes(f.alice, posItem("a2", legacyKey, "guid:ep", 15, 20))

	// A corrected URL retires the legacy key into the URL's key.
	if move, err := f.s.MoveFeed(ctx, legacy.ID, "https://legacy.example.com/rss"); move != FeedMoved || err != nil {
		t.Fatalf("retire: %v %v", move, err)
	}
	const retired = "feed:legacy.example.com/rss"
	if got, _ := f.s.GetFeedByID(ctx, legacy.ID); got.FeedKey != retired || got.MergedInto != nil {
		t.Fatalf("retired feed: %+v", got)
	}
	if sub := f.sub(f.alice, retired); sub == nil || !sub.Subscribed {
		t.Fatalf("subscription not re-keyed: %+v", sub)
	}
	if r, _ := f.state(f.alice, retired, "guid:ep"); r.position != 15 {
		t.Fatalf("episode state not re-keyed: %+v", r)
	}
	// Clients may still send the retired key; it resolves like an alias.
	out := f.sync(f.alice, 0, 0, edit("a3", legacyKey, true, 30))
	if len(out.Rejected) != 0 || len(out.Resolved) != 1 || out.Resolved[0].To != retired {
		t.Fatalf("retired key: %+v", out)
	}
	// A legacy feed whose corrected URL is another catalog feed merges into it.
	legacy2, _ := f.catalogFeed(setID, "ftp://legacy2/rss", "ep")
	if move, err := f.s.MoveFeed(ctx, legacy2.ID, newFeedURL); move != FeedMerged || err != nil {
		t.Fatalf("retire into another feed: %v %v", move, err)
	}
	if got, _ := f.s.GetFeedByKey(ctx, legacy2.FeedKey); got == nil || got.ID != other.ID {
		t.Fatalf("second legacy key resolves to %+v", got)
	}
}

func TestIngestRetiresLegacyEpisodeAndMovesStateOnlyKeys(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	feed, _ := f.catalogFeed(setID, oldFeedURL, "a")
	// A migrated row whose key collided got a legacy key; its enclosure is
	// still known.
	legacyID, err := f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed.ID, EpisodeKey: "legacy-episode:77",
		EpisodeURL: "https://cdn.example.com/x.mp3", CreatedAt: f.now})
	f.must(err)
	f.sync(f.alice, 0, 0, edit("a1", oldFeedKey, true, 10))
	f.episodes(f.alice, posItem("a2", oldFeedKey, "legacy-episode:77", 11, 20),
		posItem("a3", oldFeedKey, "guid:b", 22, 30), compItem("a4", oldFeedKey, "guid:a", true, "manual", 25))
	f.episodes(f.bob, posItem("b1", oldFeedKey, "guid:a", 3, 5))
	_, bobSeq := f.state(f.bob, oldFeedKey, "guid:a")

	// The new document carries the legacy episode under guid:x and shows
	// that episode a's GUID changed to b (same enclosure).
	doc := ingestDoc([2]string{"x", "https://cdn.example.com/x.mp3"}, [2]string{"b", "https://cdn.example.com/a.mp3"})
	for range 2 { // idempotent
		f.must(f.s.IngestFeedEpisodes(ctx, feed, doc, f.now))
	}
	if got, _ := f.s.GetEpisodeByID(ctx, legacyID); got.EpisodeKey != "guid:x" {
		t.Fatalf("legacy episode not retired: %+v", got)
	}
	if r, _ := f.state(f.alice, oldFeedKey, "guid:x"); r.position != 11 {
		t.Fatalf("legacy episode state not re-keyed: %+v", r)
	}
	// Alice's state-only guid:b joins episode a: position from b, completion from a.
	if r, _ := f.state(f.alice, oldFeedKey, "guid:a"); r.position != 22 || !r.completed {
		t.Fatalf("state-only key not merged: %+v", r)
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_state WHERE episode_key IN ('guid:b', 'legacy-episode:77')`); n != 0 {
		t.Fatal("state left under alias keys")
	}
	if r, seq := f.state(f.bob, oldFeedKey, "guid:a"); seq != bobSeq || r.position != 3 {
		t.Fatalf("bob's state rewritten: %+v", r)
	}
	// The retired key is still accepted from clients.
	out := f.episodes(f.alice, posItem("a5", oldFeedKey, "legacy-episode:77", 40, 90))
	if len(out.Rejected) != 0 || len(out.Resolved) != 1 || out.Resolved[0].To != "guid:x" {
		t.Fatalf("retired episode key: %+v", out)
	}
}

func TestIngestKeepsLegacyEpisodeWhoseItemKeyIsTaken(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	feed, _ := f.catalogFeed(setID, oldFeedURL, "a")
	legacyID, err := f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed.ID, EpisodeKey: "legacy-episode:5",
		EpisodeURL: "https://cdn.example.com/x.mp3", CreatedAt: f.now})
	f.must(err)
	// guid:a belongs to another episode, so it can never retire the legacy key.
	f.must(f.s.IngestFeedEpisodes(ctx, feed, ingestDoc([2]string{"a", "https://cdn.example.com/x.mp3"}), f.now))
	if got, _ := f.s.GetEpisodeByID(ctx, legacyID); got.EpisodeKey != "legacy-episode:5" {
		t.Fatalf("legacy key retired into a taken key: %+v", got)
	}
}

func TestRepairPodcastIdentities(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	feed, _ := f.catalogFeed(setID, newFeedURL, "ep")
	// Two duplicate rows whose winner was deleted by an older release.
	dup1, err := f.s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "https://dup.example.com/rss", CreatedAt: f.now})
	f.must(err)
	dup2, err := f.s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "http://dup.example.com/rss", FeedKey: "feed:dup-tmp", CreatedAt: f.now})
	f.must(err)
	f.must(execSQL(f, `UPDATE podcast_feeds SET merged_into = 9999, feed_key = 'feed:dup.example.com/rss' WHERE id IN (?, ?)`, dup1, dup2))
	// State kept under a key that became an alias before state followed aliases.
	f.must(execSQL(f, `INSERT INTO podcast_feed_aliases (alias_key, feed_key, change_seq) VALUES (?, ?, 0)`, oldFeedKey, newFeedKey))
	f.must(execSQL(f, `INSERT INTO podcast_subscriptions (user_id, feed_key, subscribed, subscription_ms, change_seq) VALUES (?, ?, 1, 5, 0)`, f.alice, oldFeedKey))
	f.must(execSQL(f, `INSERT INTO podcast_episode_state (user_id, feed_key, episode_key, position_seconds, position_ms, change_seq)
VALUES (?, ?, 'guid:ep', 8, 9, 0)`, f.alice, oldFeedKey))

	for range 2 { // idempotent
		f.must(repairPodcastIdentities(f.s.db))
	}
	if got, _ := f.s.GetFeedByID(ctx, dup1); got.MergedInto != nil {
		t.Fatalf("oldest dangling row not promoted: %+v", got)
	}
	if got, _ := f.s.GetFeedByID(ctx, dup2); got.MergedInto == nil || *got.MergedInto != dup1 {
		t.Fatalf("younger dangling row not re-attached: %+v", got)
	}
	if sub := f.sub(f.alice, newFeedKey); sub == nil || !sub.Subscribed || f.sub(f.alice, oldFeedKey) != nil {
		t.Fatalf("subscription not re-keyed: %+v", sub)
	}
	if r, _ := f.state(f.alice, newFeedKey, "guid:ep"); r.position != 8 {
		t.Fatalf("episode state not re-keyed: %+v", r)
	}
	if got, _ := f.s.GetFeedByID(ctx, feed.ID); got.MergedInto != nil {
		t.Fatalf("a healthy feed changed: %+v", got)
	}
}

func TestFeedMergeConflictingEpisodeAliasesKeepsBothCopies(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	old, _ := f.catalogFeed(setID, oldFeedURL, "a")
	_, ids := f.catalogFeed(setID, newFeedURL, "a", "b")
	f.must(execSQL(f, `INSERT INTO podcast_episode_aliases(feed_key, alias_key, episode_key, change_seq) VALUES (?, 'guid:common', 'guid:a', 0), (?, 'guid:common', 'guid:b', 0)`, oldFeedKey, newFeedKey))
	f.must(f.s.GrantPermission(ctx, &model.SetPermission{UserID: f.alice, SetID: setID, Role: model.RoleViewer, CreatedAt: f.now}))
	copyA := f.serverCopy(setID, ids["a"], "a.mp3")
	copyB := f.serverCopy(setID, ids["b"], "b.mp3")
	f.sync(f.alice, 0, 0, edit("s", newFeedKey, true, 10))
	f.episodes(f.alice, posItem("a", newFeedKey, "guid:a", 7, 20), compItem("b", newFeedKey, "guid:b", true, "manual", 30))
	_, err := f.s.MoveFeed(ctx, old.ID, newFeedURL)
	f.must(err)
	if r, _ := f.state(f.alice, newFeedKey, "guid:a"); r.position != 7 || !r.completed {
		t.Fatalf("shared alias did not merge registers: %+v", r)
	}
	for _, id := range []int64{copyA, copyB} {
		if m := f.mirror(f.alice, id); m == nil || m.PositionSeconds != 7 || !m.Finished {
			t.Fatalf("copy %d did not project merged state: %+v", id, m)
		}
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episodes`); n != 3 {
		t.Fatal("episode merge deleted a catalog copy")
	}
	views, err := f.s.ListSubscribedEpisodes(ctx, f.alice, PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10})
	f.must(err)
	if len(views) != 1 || views[0].EpisodeKey != "guid:a" {
		t.Fatalf("physical losing key leaked into canonical views: %+v", views)
	}
	// Both the media bridge and the old completion route resolve physical
	// rows of the losing key through the alias.
	f.must(f.s.UpsertProgress(ctx, &model.PlaybackProgress{UserID: f.alice, MediaID: copyB, PositionSeconds: 12, UpdatedAt: f.now}))
	if r, _ := f.state(f.alice, newFeedKey, "guid:a"); r.position != 12 {
		t.Fatalf("copy progress kept a separate history: %+v", r)
	}
	_, err = f.s.ToggleEpisodeCompletion(ctx, f.alice, ids["b"], f.now)
	f.must(err)
	if r, _ := f.state(f.alice, newFeedKey, "guid:a"); r.completed {
		t.Fatal("legacy toggle did not reach merged episode")
	}
}

func TestMoveFeedInvalidAndAlreadyMergedDoNothing(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	old, _ := f.catalogFeed(setID, oldFeedURL, "a")
	_, _ = f.catalogFeed(setID, newFeedURL, "a")
	for _, target := range []string{"", "ftp://example.com/rss", "https://user:secret@example.com/rss"} {
		move, err := f.s.MoveFeed(ctx, old.ID, target)
		f.must(err)
		if move != FeedNotMoved {
			t.Fatalf("invalid move applied: %q", target)
		}
	}
	_, err := f.s.MoveFeed(ctx, old.ID, newFeedURL)
	f.must(err)
	move, err := f.s.MoveFeed(ctx, old.ID, "https://unrelated.example.com/rss")
	f.must(err)
	if move != FeedNotMoved {
		t.Fatal("merged source redirected target's subscribers")
	}
}

func TestRepairRetiresAlreadyCorrectedLegacyFeedURL(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	legacy, _ := f.catalogFeed(setID, "ftp://legacy.example.com/rss", "ep")
	f.sync(f.alice, 0, 0, edit("sub", legacy.FeedKey, true, 10))
	f.must(execSQL(f, `UPDATE podcast_feeds SET feed_url = ? WHERE id = ?`, oldFeedURL, legacy.ID))
	f.must(repairPodcastIdentities(f.s.db))
	if got, err := f.s.GetFeedByKey(ctx, legacy.FeedKey); err != nil || got == nil || got.FeedKey != oldFeedKey {
		t.Fatalf("stored corrected URL not retired at startup: %+v %v", got, err)
	}
	if sub := f.sub(f.alice, oldFeedKey); sub == nil || !sub.Subscribed {
		t.Fatalf("corrected legacy subscription not rekeyed: %+v", sub)
	}
}

func TestEpisodeRekeyRepublishesTargetOnTie(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	feed, _ := f.catalogFeed(setID, oldFeedURL, "a")
	f.sync(f.alice, 0, 0, edit("sub", oldFeedKey, true, 1))
	f.episodes(f.alice, posItem("a", oldFeedKey, "guid:a", 10, 20), posItem("b", oldFeedKey, "guid:b", 99, 20))
	before, err := maxChangeSeq(ctx, f.s.db)
	f.must(err)
	f.must(f.s.IngestFeedEpisodes(ctx, feed, ingestDoc([2]string{"b", "https://cdn.example.com/a.mp3"}), f.now))
	out := f.sync(f.alice, before, 1)
	found := false
	for {
		for _, ep := range out.Episodes {
			if ep.EpisodeKey == "guid:a" && ep.Position != nil && ep.Position.Seconds == 10 {
				found = true
			}
		}
		if out.HasMore == nil || !*out.HasMore {
			break
		}
		out = f.sync(f.alice, *out.NextSince, 1)
	}
	if !found {
		t.Fatal("alias merge did not republish authoritative target tie winner after cursor")
	}
}

func TestFeedMergeResolvesAliasesAfterEarlierEpisodeMerge(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	setID := f.podcastSet()
	// The target episode is older. The common alias merges source b into
	// target a before the source's next snapshotted alias is transferred.
	_, _ = f.catalogFeed(setID, newFeedURL, "a")
	source, _ := f.catalogFeed(setID, oldFeedURL, "b")
	f.sync(f.alice, 0, 0, edit("target-sub", newFeedKey, true, 1), edit("source-sub", oldFeedKey, true, 1))
	f.episodes(f.alice, posItem("target-pos", newFeedKey, "guid:a", 10, 20),
		compItem("target-done", newFeedKey, "guid:a", true, "manual", 50),
		posItem("source-extra", oldFeedKey, "guid:extra", 30, 40),
		compItem("source-extra-done", oldFeedKey, "guid:extra", false, "manual", 30))
	// The extra state predates the server learning this alias. A feed
	// merge must rekey it alongside all other source episode state.
	f.must(execSQL(f, `INSERT INTO podcast_episode_aliases (feed_key, alias_key, episode_key, change_seq)
VALUES (?, 'guid:common', 'guid:a', 1), (?, 'guid:common', 'guid:b', 2), (?, 'guid:extra', 'guid:b', 3)`,
		newFeedKey, oldFeedKey, oldFeedKey))
	_, err := f.s.MoveFeed(ctx, source.ID, newFeedURL)
	f.must(err)
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_aliases
WHERE feed_key = ? AND alias_key = 'guid:extra' AND episode_key = 'guid:a'`, newFeedKey); n != 1 {
		t.Fatal("transferred extra alias did not point directly to the older winner")
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_state WHERE episode_key IN ('guid:extra', 'guid:b')`); n != 0 {
		t.Fatal("state remained under an alias or a losing episode key")
	}
	if r, _ := f.state(f.alice, newFeedKey, "guid:a"); r.position != 30 || r.positionMS != 40 || !r.completed || r.completionMS != 50 {
		t.Fatalf("pre-existing alias state did not merge register by register: %+v", r)
	}
	out := f.episodes(f.alice, posItem("later-extra", newFeedKey, "guid:extra", 60, 70))
	if len(out.Resolved) != 1 || out.Resolved[0].To != "guid:a" {
		t.Fatalf("incoming transferred alias did not resolve to the winner: %+v", out.Resolved)
	}
	if r, _ := f.state(f.alice, newFeedKey, "guid:a"); r.position != 60 || r.positionMS != 70 || !r.completed {
		t.Fatalf("incoming alias edit did not update the merged episode: %+v", r)
	}
	if n := f.rowCount(`SELECT COUNT(*) FROM podcast_episode_state WHERE episode_key = 'guid:b'`); n != 0 {
		t.Fatal("incoming alias edit recreated the losing history")
	}
}
