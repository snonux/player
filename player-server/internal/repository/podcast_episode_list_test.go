package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// viewFixture creates two catalog feeds: "a" (three episodes published on
// consecutive days, a1 oldest) and "b" (one episode). Alice subscribes to a
// through an alias key and to b, then unsubscribes from b.
func viewFixture(t *testing.T) (*syncFixture, map[string]int64, int64) {
	t.Helper()
	f := newSyncFixture(t)
	ctx := context.Background()
	setID, err := f.s.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: f.now})
	f.must(err)
	ids := map[string]int64{}
	for _, feed := range []struct {
		name string
		eps  int
	}{{"a", 3}, {"b", 1}} {
		feedID, err := f.s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "https://" + feed.name + ".example.com/rss", Title: feed.name, CreatedAt: f.now})
		f.must(err)
		for i := 1; i <= feed.eps; i++ {
			published := f.now.Add(time.Duration(i) * 24 * time.Hour)
			name := fmt.Sprintf("%s%d", feed.name, i)
			ids[name], err = f.s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: name, Title: name,
				PublishedAt: &published, EpisodeURL: "https://cdn.example.com/" + name + ".mp3", CreatedAt: f.now})
			f.must(err)
		}
	}
	f.must(execSQL(f, `INSERT INTO podcast_feed_aliases (alias_key, feed_key, change_seq) VALUES ('feed:old-a.example.com/rss', 'feed:a.example.com/rss', 0)`))
	f.sync(f.alice, 0, 0, edit("a", "feed:old-a.example.com/rss", true, 1), edit("b", "feed:b.example.com/rss", true, 1))
	f.sync(f.alice, 0, 0, edit("b2", "feed:b.example.com/rss", false, 2))
	return f, ids, setID
}

// titles returns the titles of a view page.
func titles(views []model.PodcastEpisodeView) string {
	var out []string
	for _, v := range views {
		out = append(out, v.Title)
	}
	return fmt.Sprint(out)
}

func TestListSubscribedEpisodesFiltersAndPages(t *testing.T) {
	f, ids, setID := viewFixture(t)
	ctx := context.Background()
	feedA := "feed:a.example.com/rss"
	f.episodes(f.alice, compItem("done", feedA, "guid:a1", true, "auto", 10), posItem("started", feedA, "guid:a2", 33, 10))
	list := func(q PodcastEpisodeQuery) []model.PodcastEpisodeView {
		t.Helper()
		views, err := f.s.ListSubscribedEpisodes(ctx, f.alice, q)
		f.must(err)
		return views
	}
	cases := []struct {
		name string
		q    PodcastEpisodeQuery
		want string
	}{
		{"uncompleted includes started", PodcastEpisodeQuery{Filter: model.PodcastFilterUncompleted, Limit: 10}, "[a3 a2]"},
		{"completed", PodcastEpisodeQuery{Filter: model.PodcastFilterCompleted, Limit: 10}, "[a1]"},
		{"all, newest first", PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10}, "[a3 a2 a1]"},
		{"second page", PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 2, Offset: 2}, "[a1]"},
		{"feed by alias key", PodcastEpisodeQuery{FeedKey: "feed:old-a.example.com/rss", Filter: model.PodcastFilterAll, Limit: 10}, "[a3 a2 a1]"},
		{"unsubscribed feed", PodcastEpisodeQuery{FeedKey: "feed:b.example.com/rss", Filter: model.PodcastFilterAll, Limit: 10}, "[]"},
		{"unknown feed", PodcastEpisodeQuery{FeedKey: "feed:none.example.com/rss", Filter: model.PodcastFilterAll, Limit: 10}, "[]"},
	}
	for _, tc := range cases {
		if got := titles(list(tc.q)); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	all := list(PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10})
	if a2 := all[1]; a2.PositionSeconds != 33 || a2.Completed || a2.FeedKey != feedA || a2.EpisodeKey != "guid:a2" || a2.FeedTitle != "a" {
		t.Fatalf("a2: %+v", a2)
	}
	if a1 := all[2]; !a1.Completed || a1.CompletionSource != "auto" || a1.ServerCopy != nil {
		t.Fatalf("a1: %+v", a1)
	}
	// A live server copy is reported, a deleted one is not.
	mediaID := f.serverCopy(setID, ids["a3"], "a3.mp3")
	if v := list(PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 1})[0]; v.ServerCopy == nil || v.ServerCopy.MediaID != mediaID || v.SetID != setID {
		t.Fatalf("server copy: %+v", v)
	}
	f.must(f.s.SoftDeleteMedia(ctx, mediaID))
	if v := list(PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 1})[0]; v.ServerCopy != nil {
		t.Fatalf("deleted server copy listed: %+v", v.ServerCopy)
	}
	// Bob has no subscriptions and sees nothing.
	if views, err := f.s.ListSubscribedEpisodes(ctx, f.bob, PodcastEpisodeQuery{Filter: model.PodcastFilterAll, Limit: 10}); err != nil || len(views) != 0 {
		t.Fatalf("bob: %+v %v", views, err)
	}
}
