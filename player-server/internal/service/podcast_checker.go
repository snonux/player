package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// errHostBackoff is returned by fetchFeedWithRetry when the host is still
// within its cool-off window from a previous failure and the request is
// skipped without contacting the network.
var errHostBackoff = errors.New("host in failure backoff window")

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
				RecoverWorker(s.logger, "podcast feed check", recover())
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
	resp, err := s.fetchFeedWithRetry(ctx, &feed)
	if err != nil {
		// Backoff bookkeeping happens inside fetchFeedWithRetry for the
		// host tracker; we still bump the per-feed consecutive_failures so
		// the existing feed-level scheduling honours the failure. Log any
		// UPDATE failure at warn level: silently swallowing it would leave
		// the backoff counter stale and the checker would keep retrying the
		// failing feed at every interval.
		if berr := s.setFeedBackoff(ctx, &feed); berr != nil {
			s.logger.Warn("podcast: failed to record feed backoff", "feed_id", feed.ID, "url", feed.FeedURL, "err", berr)
		}
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
		if berr := s.setFeedBackoff(ctx, &feed); berr != nil {
			s.logger.Warn("podcast: failed to record feed backoff", "feed_id", feed.ID, "url", feed.FeedURL, "err", berr)
		}
		return fmt.Errorf("feed check status %d", resp.StatusCode)
	}

	parsed, err := s.parseFeedReader(resp.Body)
	if err != nil {
		if berr := s.setFeedBackoff(ctx, &feed); berr != nil {
			s.logger.Warn("podcast: failed to record feed backoff", "feed_id", feed.ID, "url", feed.FeedURL, "err", berr)
		}
		return err
	}

	if err := s.updateFeedFromParsed(ctx, &feed, parsed, resp.Header.Get("ETag")); err != nil {
		if berr := s.setFeedBackoff(ctx, &feed); berr != nil {
			s.logger.Warn("podcast: failed to record feed backoff", "feed_id", feed.ID, "url", feed.FeedURL, "err", berr)
		}
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

// setFeedBackoff bumps the feed's consecutive_failures counter, computes the
// next exponential-backoff check time, and persists the updated row. It now
// returns the UPDATE error instead of swallowing it: callers log at warn level
// so a persistent DB failure is visible to operators (otherwise the counter
// never advances and the checker hammers the failing feed every interval).
func (s *podcastFeedChecker) setFeedBackoff(ctx context.Context, feed *model.PodcastFeed) error {
	feed.ConsecutiveFailures++
	backoff := baseFeedRetryBackoff * (1 << max(0, feed.ConsecutiveFailures-1))
	if backoff > maxFeedRetryBackoff {
		backoff = maxFeedRetryBackoff
	}
	next := s.clock.Now().Add(backoff)
	feed.NextCheckAt = &next
	return s.store.UpdateFeed(ctx, feed)
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

// fetchFeedWithRetry performs the conditional GET for a feed with bounded
// retries on transient errors and per-host short-circuiting. It returns the
// last successful response or, on permanent failure, the last error. On
// transport/5xx failure paths it also records the host failure so subsequent
// calls within the configured HostBackoff window skip the network entirely.
//
// Retry rules:
//   - Network/transport errors and 5xx responses are retryable.
//   - 4xx responses (including 304 Not Modified and other client outcomes) are
//     terminal — the response is returned as-is, callers inspect StatusCode.
//   - Retries respect the policy MaxAttempts; the wait between attempts grows
//     exponentially from InitialBackoff up to MaxBackoff and is interrupted
//     by ctx cancellation.
func (s *podcastFeedChecker) fetchFeedWithRetry(ctx context.Context, feed *model.PodcastFeed) (*http.Response, error) {
	policy := s.fetchPolicy.normalize()
	host := hostForURL(feed.FeedURL)

	if s.isHostInBackoff(host, policy.HostBackoff) {
		return nil, fmt.Errorf("%w: host=%s", errHostBackoff, host)
	}

	var lastErr error
	backoff := policy.InitialBackoff
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		resp, err := s.doFeedRequest(ctx, feed)
		if err == nil && !isRetryableStatus(resp.StatusCode) {
			// Either success (2xx/3xx) or a terminal 4xx — let the caller
			// decide. Clear any previous host failure record on real success.
			if resp.StatusCode < 500 {
				s.clearHostFailure(host)
			}
			return resp, nil
		}
		// Drain & close the body before deciding to retry so the connection
		// is returned to the pool.
		if resp != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("feed check status %d", resp.StatusCode)
		} else {
			lastErr = err
		}

		if attempt == policy.MaxAttempts {
			break
		}
		if err := sleepCtx(ctx, backoff); err != nil {
			return nil, err
		}
		backoff *= 2
		if backoff > policy.MaxBackoff {
			backoff = policy.MaxBackoff
		}
	}

	// All retries exhausted — record the host failure so other feeds on the
	// same flaky host are skipped quickly during the cool-off window.
	s.recordHostFailure(host)
	return nil, lastErr
}

// doFeedRequest issues one conditional GET for the feed.
func (s *podcastFeedChecker) doFeedRequest(ctx context.Context, feed *model.PodcastFeed) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.FeedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for feed %d: %w", feed.ID, err)
	}
	if feed.LastETag != "" {
		req.Header.Set("If-None-Match", feed.LastETag)
	}
	if feed.LastCheckedAt != nil {
		req.Header.Set("If-Modified-Since", feed.LastCheckedAt.Format(http.TimeFormat))
	}
	return s.httpClient.Do(req)
}

// isRetryableStatus returns true for 5xx server errors (retryable). 4xx and
// 3xx/2xx are terminal — we do not want to hammer feeds returning 404/410/403.
func isRetryableStatus(code int) bool {
	return code >= 500 && code <= 599
}

// isHostInBackoff reports whether host had a recent failure recorded within
// the window. The lookup is O(1) and guarded by hostFailuresMu.
func (s *podcastFeedChecker) isHostInBackoff(host string, window time.Duration) bool {
	if host == "" {
		return false
	}
	s.hostFailuresMu.Lock()
	defer s.hostFailuresMu.Unlock()
	failedAt, ok := s.hostFailures[host]
	if !ok {
		return false
	}
	if s.clock.Now().Sub(failedAt) >= window {
		// Window has elapsed — drop the stale entry so the map does not grow
		// without bound for transient one-off failures.
		delete(s.hostFailures, host)
		return false
	}
	return true
}

// recordHostFailure stamps the host's last-failure time.
func (s *podcastFeedChecker) recordHostFailure(host string) {
	if host == "" {
		return
	}
	s.hostFailuresMu.Lock()
	s.hostFailures[host] = s.clock.Now()
	s.hostFailuresMu.Unlock()
}

// clearHostFailure removes the host's recorded failure (called on success).
func (s *podcastFeedChecker) clearHostFailure(host string) {
	if host == "" {
		return
	}
	s.hostFailuresMu.Lock()
	delete(s.hostFailures, host)
	s.hostFailuresMu.Unlock()
}

// hostForURL extracts the host component (host:port) from a feed URL. Returns
// empty string for unparseable inputs — callers treat that as "no backoff".
func hostForURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// sleepCtx waits for d or returns early if ctx is cancelled. Returns the
// context error on cancellation so callers can abort the retry loop.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
