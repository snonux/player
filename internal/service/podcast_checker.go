package service

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
)

const (
	baseFeedRetryBackoff = 15 * time.Minute
	maxFeedRetryBackoff  = 24 * time.Hour
)

// podcastFeedChecker triggers background feed refresh and updates episodes.
type podcastFeedChecker struct {
	*podcastService
}

func newPodcastFeedChecker(svc *podcastService) *podcastFeedChecker {
	return &podcastFeedChecker{podcastService: svc}
}

// CheckFeeds refreshes all podcast feeds that are due for a check.
func (s *podcastFeedChecker) CheckFeeds(ctx context.Context) error {
	before := s.clock.Now().Add(-time.Duration(s.checkInterval) * time.Minute)
	feeds, err := s.store.ListFeedsNeedingCheck(ctx, s.clock.Now(), before)
	if err != nil {
		return fmt.Errorf("list feeds needing check: %w", err)
	}

	s.logger.Info("podcast feed check starting", "count", len(feeds))

	var wg sync.WaitGroup
	for _, feed := range feeds {
		wg.Add(1)
		go func(f model.PodcastFeed) {
			defer wg.Done()
			defer func() {
				handleWorkerPanic(s.logger, "podcast feed check", recover())
			}()
			if err := s.checkFeed(ctx, f); err != nil {
				s.logger.Warn("podcast feed check failed", "feed_id", f.ID, "feed_url", f.FeedURL, "err", err)
			} else {
				s.logger.Info("podcast feed check ok", "feed_id", f.ID, "feed_url", f.FeedURL)
			}
		}(feed)
	}
	wg.Wait()

	s.logger.Info("podcast feed check finished", "count", len(feeds))
	return nil
}

func (s *podcastFeedChecker) checkFeed(ctx context.Context, feed model.PodcastFeed) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.FeedURL, nil)
	if err != nil {
		return fmt.Errorf("build request for feed %d: %w", feed.ID, err)
	}

	// Conditional GET headers.
	if feed.LastETag != "" {
		req.Header.Set("If-None-Match", feed.LastETag)
	}
	if feed.LastCheckedAt != nil {
		req.Header.Set("If-Modified-Since", feed.LastCheckedAt.Format(http.TimeFormat))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.setFeedBackoff(ctx, &feed)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		now := s.clock.Now()
		feed.LastCheckedAt = &now
		feed.ConsecutiveFailures = 0
		feed.NextCheckAt = nil
		if err := s.store.UpdateFeed(ctx, &feed); err != nil {
			return fmt.Errorf("update feed after 304: %w", err)
		}
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		s.setFeedBackoff(ctx, &feed)
		return fmt.Errorf("feed check status %d", resp.StatusCode)
	}

	parsed, err := s.parseFeedReader(resp.Body)
	if err != nil {
		s.setFeedBackoff(ctx, &feed)
		return err
	}

	if err := s.updateFeedFromParsed(ctx, &feed, parsed, resp.Header.Get("ETag")); err != nil {
		s.setFeedBackoff(ctx, &feed)
		return err
	}

	if err := s.upsertFeedEpisodes(ctx, &feed, parsed); err != nil {
		s.logger.Warn("upsert feed episodes failed", "error", err, "feed", feed.FeedURL)
	}

	if parsed.ImageURL != "" {
		set, err := s.store.GetSetByID(ctx, feed.SetID)
		if err == nil && set != nil {
			setPath := filepath.Join(s.mediaRoot, set.RootPath)
			if err := s.downloadCover(s.httpClient, parsed.ImageURL, setPath); err != nil {
				s.logger.Warn("download cover failed", "error", err, "feed", feed.FeedURL)
			}
		}
	}

	return nil
}

func (s *podcastFeedChecker) setFeedBackoff(ctx context.Context, feed *model.PodcastFeed) {
	feed.ConsecutiveFailures++
	backoff := baseFeedRetryBackoff * (1 << max(0, feed.ConsecutiveFailures-1))
	if backoff > maxFeedRetryBackoff {
		backoff = maxFeedRetryBackoff
	}
	next := s.clock.Now().Add(backoff)
	feed.NextCheckAt = &next
	_ = s.store.UpdateFeed(ctx, feed)
}

func (s *podcastFeedChecker) updateFeedFromParsed(ctx context.Context, feed *model.PodcastFeed, parsed *podcast.ParsedFeed, etag string) error {
	feed.Title = parsed.Title
	feed.Description = parsed.Description
	feed.ImageURL = parsed.ImageURL
	feed.LastETag = etag
	now := s.clock.Now()
	feed.LastCheckedAt = &now
	feed.ConsecutiveFailures = 0
	feed.NextCheckAt = nil
	return s.store.UpdateFeed(ctx, feed)
}

func (s *podcastFeedChecker) upsertFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, parsed *podcast.ParsedFeed) error {
	var failed []string
	for _, ep := range parsed.Episodes {
		existing, err := s.store.GetEpisodeByGUID(ctx, feed.ID, ep.GUID)
		if err != nil {
			s.logger.Warn("failed to look up episode by GUID", "error", err, "guid", ep.GUID)
			failed = append(failed, ep.GUID)
			continue
		}
		if existing != nil {
			continue
		}
		episode := &model.PodcastEpisode{
			FeedID:          feed.ID,
			GUID:            ep.GUID,
			Title:           ep.Title,
			Description:     ep.Description,
			PublishedAt:     ep.PublishedAt,
			EpisodeURL:      ep.EpisodeURL,
			DurationSeconds: ep.DurationSeconds,
			FileSize:        ep.FileSize,
			CreatedAt:       s.clock.Now(),
		}
		if _, err := s.store.CreateEpisode(ctx, episode); err != nil {
			s.logger.Warn("failed to insert episode", "error", err, "guid", ep.GUID)
			failed = append(failed, ep.GUID)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("episode upserts failed for %d episode(s): %v", len(failed), failed)
	}
	return nil
}
