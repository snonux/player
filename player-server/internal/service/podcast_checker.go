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

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/podcast"
	"github.com/snonux/player/internal/repository"
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
	if err := s.store.CreateDeferredPodcastFeeds(ctx, s.clock.Now(), s.checkInterval); err != nil {
		return fmt.Errorf("create deferred podcast feeds: %w", err)
	}
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

// checkFeed fetches one catalog feed (conditional GET, through the client
// its creator is allowed), stores its metadata and episodes, follows
// permanent moves, and refreshes its cover. It never downloads audio.
func (s *podcastFeedChecker) checkFeed(ctx context.Context, feed model.PodcastFeed) error {
	moves := &redirectLog{}
	client := moves.wrap(s.feedClient(ctx, &feed))
	resp, err := s.fetchFeedWithRetry(ctx, &feed, client)
	if err != nil {
		// Host backoff bookkeeping happened inside fetchFeedWithRetry;
		// the per-feed counter keeps the feed's own schedule honest.
		s.recordFeedBackoff(ctx, &feed)
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
		s.applyFeedMoves(ctx, &feed, moves.permanent, "")
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		s.recordFeedBackoff(ctx, &feed)
		return fmt.Errorf("feed check status %d", resp.StatusCode)
	}
	parsed, err := s.parseFeedReader(resp.Body)
	if err == nil {
		err = s.updateFeedFromParsed(ctx, &feed, parsed, resp.Header.Get("ETag"))
	}
	if err != nil {
		s.recordFeedBackoff(ctx, &feed)
		return err
	}
	if s.applyFeedMoves(ctx, &feed, moves.permanent, parsed.NewFeedURL) {
		return nil
	}
	if err := s.upsertFeedEpisodes(ctx, &feed, parsed); err != nil {
		s.logger.Warn("upsert feed episodes failed", "error", err, "feed", feed.FeedURL)
	}
	s.refreshCover(ctx, &feed, parsed, client)
	return nil
}

// recordFeedBackoff bumps the feed's failure counter and logs when that
// cannot be stored: a silently stale counter would make the checker retry
// a failing feed at every interval.
func (s *podcastFeedChecker) recordFeedBackoff(ctx context.Context, feed *model.PodcastFeed) {
	if err := s.setFeedBackoff(ctx, feed); err != nil {
		s.logger.Warn("podcast: failed to record feed backoff", "feed_id", feed.ID, "url", feed.FeedURL, "err", err)
	}
}

// applyFeedMoves records a permanent redirect and an <itunes:new-feed-url>
// (in that order, so the explicit statement decides the final fetch URL).
// A move into another catalog feed merges this feed into it (the target
// wins, contract §3.3). It reports whether the feed was merged: a merged
// feed is no catalog feed of its own any more, so the caller stores
// nothing else for it and the target refreshes itself.
func (s *podcastFeedChecker) applyFeedMoves(ctx context.Context, feed *model.PodcastFeed, targets ...string) bool {
	merged := false
	for _, target := range targets {
		if target == "" || target == feed.FeedURL {
			continue
		}
		move, err := s.store.MoveFeed(ctx, feed.ID, target)
		if err != nil {
			s.logger.Warn("podcast: feed move not applied", "feed_id", feed.ID, "target", target, "err", err)
			continue
		}
		switch move {
		case repository.FeedMerged:
			s.logger.Info("podcast: feed merged into the feed it moved to", "feed_id", feed.ID, "from", feed.FeedURL, "to", target)
			merged = true
			// The fetched document belongs to the redirect target. Its
			// explicit new-feed-url still applies after an intermediate
			// merge, and must be applied to that target's canonical row.
			key, err := podcast.FeedKey(target)
			if err != nil {
				s.logger.Warn("podcast: invalid merged feed target", "target", target, "err", err)
				return true
			}
			winner, err := s.store.GetFeedByKey(ctx, key)
			if err != nil || winner == nil {
				s.logger.Warn("podcast: cannot follow merged feed", "target", target, "err", err)
				return true
			}
			*feed = *winner
		case repository.FeedMoved:
			s.logger.Info("podcast: feed moved", "feed_id", feed.ID, "from", feed.FeedURL, "to", target)
			feed.FeedURL = target
		}
	}
	return merged
}

// refreshCover downloads the feed's cover art (never audio) with the same
// client the feed was fetched with.
func (s *podcastFeedChecker) refreshCover(ctx context.Context, feed *model.PodcastFeed, parsed *podcast.ParsedFeed, client *http.Client) {
	if parsed.ImageURL == "" {
		return
	}
	set, err := s.store.GetSetByID(ctx, feed.SetID)
	if err != nil || set == nil {
		return
	}
	setPath := filepath.Join(s.mediaRoot, set.RootPath)
	if err := s.downloadCover(client, parsed.ImageURL, setPath); err != nil {
		s.logger.Warn("download cover failed", "error", err, "feed", feed.FeedURL)
	}
}

// redirectLog records where a fetch was permanently redirected: the target
// of the leading run of 301/308 hops. A temporary hop (302, 303, 307) ends
// the run, so whatever follows it changes nothing (contract §3.3).
type redirectLog struct {
	base      func(*http.Request, []*http.Request) error
	permanent string
	broken    bool
}

// wrap returns a copy of client whose redirects are recorded, keeping the
// client's own redirect policy.
func (l *redirectLog) wrap(client *http.Client) *http.Client {
	c := *client
	l.base = client.CheckRedirect
	c.CheckRedirect = l.check
	return &c
}

func (l *redirectLog) check(req *http.Request, via []*http.Request) error {
	if len(via) == 1 { // first hop of a new attempt
		l.permanent, l.broken = "", false
	}
	if l.base != nil {
		if err := l.base(req, via); err != nil {
			return err
		}
	} else if len(via) >= maxFeedRedirects {
		return fmt.Errorf("stopped after %d redirects", maxFeedRedirects)
	}
	if l.broken || req.Response == nil {
		return nil
	}
	switch req.Response.StatusCode {
	case http.StatusMovedPermanently, http.StatusPermanentRedirect:
		l.permanent = req.URL.String()
	default:
		l.broken = true
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

// upsertFeedEpisodes ingests a fetched feed document: episodes are matched
// to the catalog by their stable keys (podcast.MatchEpisodes, contract
// §3.3), new ones are created and the aliases ingest finds are recorded. It
// stores metadata only; episode audio is never downloaded here.
func (s *podcastFeedChecker) upsertFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, parsed *podcast.ParsedFeed) error {
	return ingestParsedEpisodes(ctx, s.podcastService, feed, parsed)
}

// ingestParsedEpisodes hands the episodes of a parsed feed to the store's
// keyed ingest. Subscribing and the background checker share it, so both
// treat a feed document the same way.
func ingestParsedEpisodes(ctx context.Context, svc *podcastService, feed *model.PodcastFeed, parsed *podcast.ParsedFeed) error {
	episodes := make([]model.PodcastEpisode, len(parsed.Episodes))
	for i, ep := range parsed.Episodes {
		episodes[i] = model.PodcastEpisode{
			FeedID:          feed.ID,
			GUID:            ep.GUID,
			Title:           ep.Title,
			Description:     ep.Description,
			PublishedAt:     ep.PublishedAt,
			EpisodeURL:      ep.EpisodeURL,
			DurationSeconds: ep.DurationSeconds,
			FileSize:        ep.FileSize,
		}
	}
	if err := svc.store.IngestFeedEpisodes(ctx, feed, episodes, svc.clock.Now()); err != nil {
		return fmt.Errorf("ingest episodes of feed %d: %w", feed.ID, err)
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
//   - A connection the outbound policy refused is returned at once and
//     not recorded as a host failure.
//   - Network/transport errors and 5xx responses are retryable.
//   - 4xx responses (including 304 Not Modified and other client outcomes) are
//     terminal — the response is returned as-is, callers inspect StatusCode.
//   - Retries respect the policy MaxAttempts; the wait between attempts grows
//     exponentially from InitialBackoff up to MaxBackoff and is interrupted
//     by ctx cancellation.
func (s *podcastFeedChecker) fetchFeedWithRetry(ctx context.Context, feed *model.PodcastFeed, client *http.Client) (*http.Response, error) {
	policy := s.fetchPolicy.normalize()
	host := hostForURL(feed.FeedURL)

	if s.isHostInBackoff(host, policy.HostBackoff) {
		return nil, fmt.Errorf("%w: host=%s", errHostBackoff, host)
	}

	var lastErr error
	backoff := policy.InitialBackoff
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		resp, err := s.doFeedRequest(ctx, feed, client)
		if errors.Is(err, errBlockedAddress) {
			// A policy refusal, not a host failure: retrying cannot help,
			// and backing off the host would hold up other feeds on it.
			return nil, err
		}
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

// doFeedRequest issues one conditional GET for the feed with client.
func (s *podcastFeedChecker) doFeedRequest(ctx context.Context, feed *model.PodcastFeed, client *http.Client) (*http.Response, error) {
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
	return client.Do(req)
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
