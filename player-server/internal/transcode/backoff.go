package transcode

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// failureBackoff is the pause after a first failed transcode; it doubles
	// per consecutive failure up to maxFailureBackoff.
	failureBackoff    = time.Minute
	maxFailureBackoff = 30 * time.Minute
	// timeoutBackoff is the pause after a job hit the job timeout. Such a
	// job has already burned hours of CPU and would do so again on every
	// retry, so it is only repeated a day later or when the source changes.
	timeoutBackoff = 24 * time.Hour
)

// failure is a negative-cache entry. Entries are keyed by media id, not by
// rendition name: a file that keeps changing (a slow copy) gets a new
// rendition name with every change, and a per-name backoff would never
// apply to it.
type failure struct {
	count   int
	retryAt time.Time
	// err is what requests get during the backoff: ErrSourceChanged for a
	// source that was still changing (clients are told to retry), otherwise
	// ErrFailedRecently.
	err error
	// name is set for timeouts only and ties the entry to one version of
	// the source: a replaced file deserves a new attempt despite the long
	// timeout backoff.
	name string
}

// backoffError returns the error to answer with while mediaID is in backoff,
// or nil when a transcode may start. The caller holds c.mu.
func (c *Cache) backoffError(mediaID int64, name string) error {
	f, ok := c.failures[mediaID]
	if !ok {
		return nil
	}
	if f.name != "" && f.name != name {
		delete(c.failures, mediaID)
		return nil
	}
	if !c.clk.Now().Before(f.retryAt) {
		// Keep the entry: its count makes the next backoff longer.
		return nil
	}
	return f.err
}

// settle turns the outcome of produce into the error reported to waiting
// requests and maintains the negative cache.
//
// Context errors are replaced, never wrapped: waiters must not mistake a
// stopped job for their own request being cancelled. Conditions that are
// nobody's fault and heal by themselves (shutdown, full disk) are not
// remembered. Everything else is, so that polling clients cannot start one
// ffmpeg run per request for a file that will fail again.
func (c *Cache) settle(jobCtx, workCtx context.Context, name string, src Source, err error) error {
	switch {
	case err == nil:
		c.mu.Lock()
		delete(c.failures, src.MediaID)
		c.lastUse[name] = c.clk.Now()
		c.mu.Unlock()
		return nil
	case jobCtx.Err() != nil:
		c.logger.Info("transcode aborted", "media_id", src.MediaID, "source", src.Path)
		return ErrAborted
	case errors.Is(err, ErrNoSpace):
		c.logger.Warn("transcode skipped: no space", "media_id", src.MediaID, "source", src.Path, "err", err)
		return err
	case errors.Is(err, ErrSourceChanged):
		retryIn := c.recordFailure(src.MediaID, failure{err: ErrSourceChanged})
		c.logger.Warn("transcode discarded: source changed while it ran", "media_id", src.MediaID, "source", src.Path, "retry_in", retryIn)
		return err
	case workCtx.Err() != nil:
		err = fmt.Errorf("transcode timed out after %s", c.opts.JobTimeout)
		c.recordFailure(src.MediaID, failure{err: ErrFailedRecently, name: name})
		c.logger.Error("transcode timed out; not retried until the source changes or the pause is over",
			"media_id", src.MediaID, "source", src.Path, "timeout", c.opts.JobTimeout, "retry_in", timeoutBackoff)
		return err
	}
	retryIn := c.recordFailure(src.MediaID, failure{err: ErrFailedRecently})
	c.logger.Error("transcode failed", "media_id", src.MediaID, "source", src.Path, "retry_in", retryIn, "err", err)
	return err
}

// recordFailure stores a negative-cache entry and returns how long new
// attempts are refused. The pause doubles with every consecutive failure of
// the same media item; a timeout entry (f.name set) always pauses for
// timeoutBackoff.
func (c *Cache) recordFailure(mediaID int64, f failure) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clk.Now()
	// Forget entries that expired long ago so the map cannot grow forever.
	for id, old := range c.failures {
		if now.Sub(old.retryAt) > timeoutBackoff {
			delete(c.failures, id)
		}
	}
	f.count = c.failures[mediaID].count + 1
	backoff := maxFailureBackoff
	switch {
	case f.name != "":
		backoff = timeoutBackoff
	case f.count < 6: // 1m, 2m, 4m, 8m, 16m, then the 30m cap
		backoff = failureBackoff << (f.count - 1)
	}
	f.retryAt = now.Add(backoff)
	c.failures[mediaID] = f
	return backoff
}
