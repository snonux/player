package repository

import (
	"context"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestPodcastRepo_ListFeedsNeedingCheck_Backoff(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()

	now := time.Now().Truncate(time.Second)

	setID, err := s.CreateSet(ctx, &model.Set{Name: "Podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: now})
	if err != nil {
		t.Fatalf("create set: %v", err)
	}

	// Feed with last_checked_at far past and no next_check_at.
	feed1 := &model.PodcastFeed{
		SetID:     setID,
		FeedURL:   "https://example.com/1.xml",
		Title:     "Feed 1",
		CreatedAt: now,
	}
	id1, _ := s.CreateFeed(ctx, feed1)

	// Feed with next_check_at in the future.
	future := now.Add(time.Hour)
	feed2 := &model.PodcastFeed{
		SetID:       setID,
		FeedURL:     "https://example.com/2.xml",
		Title:       "Feed 2",
		NextCheckAt: &future,
		CreatedAt:   now,
	}
	id2, _ := s.CreateFeed(ctx, feed2)

	// Feed with last_checked_at recently (not needing check).
	recent := now.Add(-5 * time.Minute)
	feed3 := &model.PodcastFeed{
		SetID:         setID,
		FeedURL:       "https://example.com/3.xml",
		Title:         "Feed 3",
		LastCheckedAt: &recent,
		CreatedAt:     now,
	}
	id3, _ := s.CreateFeed(ctx, feed3)

	before := now.Add(-time.Hour)
	feeds, err := s.ListFeedsNeedingCheck(ctx, now, before)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := map[int64]bool{}
	for _, f := range feeds {
		ids[f.ID] = true
	}
	if !ids[id1] {
		t.Error("expected feed1")
	}
	if ids[id2] {
		t.Error("did not expect feed2 (next_check_at in future)")
	}
	if ids[id3] {
		t.Error("did not expect feed3 (checked recently)")
	}
}

func TestPodcastRepo_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()

	now := time.Now().Truncate(time.Second)

	// Create a user and set first.
	userID, err := s.CreateUser(ctx, &model.User{Username: "podcaster", PasswordHash: "h", CreatedAt: now})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	setID, err := s.CreateSet(ctx, &model.Set{Name: "Test Podcast", RootPath: "test-podcast", IsPodcast: true, CreatedAt: now})
	if err != nil {
		t.Fatalf("create set: %v", err)
	}

	feed := &model.PodcastFeed{
		SetID:       setID,
		FeedURL:     "https://example.com/feed.xml",
		Title:       "Test Feed",
		Description: "A test podcast feed",
		ImageURL:    "https://example.com/cover.jpg",
		CreatedAt:   now,
	}
	feedID, err := s.CreateFeed(ctx, feed)
	if err != nil {
		t.Fatalf("create feed: %v", err)
	}
	if feedID == 0 {
		t.Fatal("expected feed id")
	}

	// Get feed by ID.
	got, err := s.GetFeedByID(ctx, feedID)
	if err != nil {
		t.Fatalf("get feed: %v", err)
	}
	if got == nil || got.Title != "Test Feed" {
		t.Fatalf("unexpected feed: %+v", got)
	}

	// Get feed by set ID.
	gotBySet, err := s.GetFeedBySetID(ctx, setID)
	if err != nil {
		t.Fatalf("get feed by set: %v", err)
	}
	if gotBySet == nil || gotBySet.FeedURL != "https://example.com/feed.xml" {
		t.Fatalf("unexpected feed by set: %+v", gotBySet)
	}

	// Create episode.
	episode := &model.PodcastEpisode{
		FeedID:     feedID,
		GUID:       "ep-1",
		Title:      "Episode 1",
		EpisodeURL: "https://example.com/ep1.mp3",
		CreatedAt:  now,
	}
	epID, err := s.CreateEpisode(ctx, episode)
	if err != nil {
		t.Fatalf("create episode: %v", err)
	}

	// Get episode by ID.
	gotEp, err := s.GetEpisodeByID(ctx, epID)
	if err != nil {
		t.Fatalf("get episode: %v", err)
	}
	if gotEp == nil || gotEp.Title != "Episode 1" {
		t.Fatalf("unexpected episode: %+v", gotEp)
	}

	// Get episode by GUID.
	gotEpGUID, err := s.GetEpisodeByGUID(ctx, feedID, "ep-1")
	if err != nil {
		t.Fatalf("get episode by guid: %v", err)
	}
	if gotEpGUID == nil || gotEpGUID.Title != "Episode 1" {
		t.Fatalf("unexpected episode by guid: %+v", gotEpGUID)
	}

	// Create a media row for linking.
	mediaID, err := s.CreateMedia(ctx, &model.Media{
		SetID:         setID,
		RelPath:       "ep1.mp3",
		FileName:      "ep1.mp3",
		AbsPath:       "/tmp/ep1.mp3",
		Type:          "audio",
		FileSizeBytes: 42,
		CreatedAt:     now,
	})
	if err != nil {
		t.Fatalf("create media: %v", err)
	}

	// Update episode media.
	if err := s.UpdateEpisodeMedia(ctx, epID, mediaID, "ep1.mp3"); err != nil {
		t.Fatalf("update episode media: %v", err)
	}
	updatedEp, err := s.GetEpisodeByID(ctx, epID)
	if err != nil {
		t.Fatalf("get updated episode: %v", err)
	}
	if updatedEp == nil || !updatedEp.IsDownloaded || updatedEp.FileName != "ep1.mp3" {
		t.Fatalf("episode not updated: %+v", updatedEp)
	}

	// Upsert progress.
	status := &model.PodcastStatus{
		UserID:      userID,
		EpisodeID:   epID,
		IsCompleted: true,
		UpdatedAt:   now,
	}
	if err := s.UpsertEpisodeProgress(ctx, status); err != nil {
		t.Fatalf("upsert progress: %v", err)
	}

	// Get progress.
	gotStatus, err := s.GetEpisodeProgress(ctx, userID, epID)
	if err != nil {
		t.Fatalf("get progress: %v", err)
	}
	if gotStatus == nil || !gotStatus.IsCompleted {
		t.Fatalf("unexpected status: %+v", gotStatus)
	}

	// List episodes with status.
	withStatus, err := s.ListEpisodesWithStatus(ctx, userID, feedID, 10, 0)
	if err != nil {
		t.Fatalf("list episodes with status: %v", err)
	}
	if len(withStatus) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(withStatus))
	}
	if !withStatus[0].IsCompleted {
		t.Fatal("expected episode to be completed")
	}

	// Delete episodes by feed.
	if err := s.DeleteEpisodesByFeed(ctx, feedID); err != nil {
		t.Fatalf("delete episodes: %v", err)
	}

	// Delete feed.
	if err := s.DeleteFeed(ctx, feedID); err != nil {
		t.Fatalf("delete feed: %v", err)
	}

	// Confirm deletion.
	deleted, err := s.GetFeedByID(ctx, feedID)
	if err != nil {
		t.Fatalf("get deleted feed: %v", err)
	}
	if deleted != nil {
		t.Fatal("expected feed to be deleted")
	}
}

func TestPodcastRepo_ListEpisodesByFeedIDsWithStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()

	now := time.Now().Truncate(time.Second)

	userID, err := s.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "h", CreatedAt: now})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	setID, err := s.CreateSet(ctx, &model.Set{Name: "Podcasts", RootPath: "podcasts", IsPodcast: true, CreatedAt: now})
	if err != nil {
		t.Fatalf("create set: %v", err)
	}

	feed1, _ := s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "https://example.com/f1.xml", Title: "F1", CreatedAt: now})
	feed2, _ := s.CreateFeed(ctx, &model.PodcastFeed{SetID: setID, FeedURL: "https://example.com/f2.xml", Title: "F2", CreatedAt: now})

	p1 := now.Add(-1 * time.Hour)
	p2 := now.Add(-2 * time.Hour)
	p3 := now.Add(-3 * time.Hour)

	_, err = s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed1, GUID: "g1", Title: "Ep1", PublishedAt: &p1, CreatedAt: now})
	if err != nil {
		t.Fatalf("create ep1: %v", err)
	}
	_, err = s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed2, GUID: "g2", Title: "Ep2", PublishedAt: &p2, CreatedAt: now})
	if err != nil {
		t.Fatalf("create ep2: %v", err)
	}
	_, err = s.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed1, GUID: "g3", Title: "Ep3", PublishedAt: &p3, CreatedAt: now})
	if err != nil {
		t.Fatalf("create ep3: %v", err)
	}

	// Both feeds, global limit=2 offset=0 should return Ep1 (newest) and Ep2.
	eps, err := s.ListEpisodesByFeedIDsWithStatus(ctx, userID, []int64{feed1, feed2}, 2, 0)
	if err != nil {
		t.Fatalf("list by feed ids with status: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(eps))
	}
	if eps[0].Title != "Ep1" {
		t.Errorf("expected first episode Ep1, got %s", eps[0].Title)
	}
	if eps[1].Title != "Ep2" {
		t.Errorf("expected second episode Ep2, got %s", eps[1].Title)
	}

	// offset=1, limit=1 should skip Ep1 and return Ep2.
	eps, err = s.ListEpisodesByFeedIDsWithStatus(ctx, userID, []int64{feed1, feed2}, 1, 1)
	if err != nil {
		t.Fatalf("list by feed ids with status offset: %v", err)
	}
	if len(eps) != 1 || eps[0].Title != "Ep2" {
		t.Fatalf("expected single episode Ep2, got %+v", eps)
	}

	// offset=2, limit=10 should return the remaining Ep3 from feed1.
	eps, err = s.ListEpisodesByFeedIDsWithStatus(ctx, userID, []int64{feed1, feed2}, 10, 2)
	if err != nil {
		t.Fatalf("list by feed ids with status offset2: %v", err)
	}
	if len(eps) != 1 || eps[0].Title != "Ep3" {
		t.Fatalf("expected single episode Ep3, got %+v", eps)
	}

	// Empty feed IDs returns empty slice.
	eps, err = s.ListEpisodesByFeedIDsWithStatus(ctx, userID, []int64{}, 10, 0)
	if err != nil {
		t.Fatalf("empty feed ids: %v", err)
	}
	if len(eps) != 0 {
		t.Fatalf("expected 0 episodes for empty feed ids, got %d", len(eps))
	}
}
