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
	// longBackoff is the pause for outcomes that cost a lot and will repeat
	// unchanged: a job that hit the job timeout (hours of CPU) and a
	// rendition that does not fit the cache budget (possibly gigabytes
	// written). They are only tried again a day later or when the source
	// file changes.
	longBackoff = 24 * time.Hour
)

// failure is a negative-cache entry, kept per media id.
type failure struct {
	count   int
	retryAt time.Time
	// err is what requests get during the backoff: ErrSourceChanged for a
	// source that was still changing (clients are told to retry),
	// ErrTooLarge for a rendition over budget, otherwise ErrFailedRecently.
	err error
	// name ties the entry to one version of the source (its rendition
	// name): a replaced file deserves a new attempt at once, whatever the
	// previous file did. It is empty only for ErrSourceChanged entries,
	// which must survive the name changing — a file that is still being
	// copied gets a new name with every write, and a per-name entry would
	// never apply to it.
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
// nobody's fault and heal by themselves (shutdown, a full disk) are not
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
	case workCtx.Err() != nil:
		err = fmt.Errorf("transcode timed out after %s", c.opts.JobTimeout)
	}
	return c.remember(name, src, err, workCtx.Err() != nil)
}

// remember records a failed job in the negative cache, logs it once, and
// returns the error for the waiting requests.
func (c *Cache) remember(name string, src Source, err error, timedOut bool) error {
	attrs := []any{"media_id", src.MediaID, "source", src.Path}
	switch {
	case errors.Is(err, ErrTooLarge):
		c.recordFailure(src.MediaID, failure{err: ErrTooLarge, name: name}, longBackoff)
		c.logger.Warn("transcode refused: rendition does not fit the cache budget; not retried until the source changes",
			append(attrs, "budget_bytes", c.opts.MaxBytes, "err", err)...)
	case errors.Is(err, ErrSourceChanged):
		retryIn := c.recordFailure(src.MediaID, failure{err: ErrSourceChanged}, 0)
		c.logger.Warn("transcode discarded: source changed while it ran", append(attrs, "retry_in", retryIn)...)
	case timedOut:
		c.recordFailure(src.MediaID, failure{err: ErrFailedRecently, name: name}, longBackoff)
		c.logger.Error("transcode timed out; not retried until the source changes or the pause is over",
			append(attrs, "timeout", c.opts.JobTimeout, "retry_in", longBackoff)...)
	default:
		retryIn := c.recordFailure(src.MediaID, failure{err: ErrFailedRecently, name: name}, 0)
		c.logger.Error("transcode failed", append(attrs, "retry_in", retryIn, "err", err)...)
	}
	return err
}

// recordFailure stores a negative-cache entry and returns how long new
// attempts are refused: fixed when given, otherwise a pause that doubles
// with every consecutive failure of the same media item.
func (c *Cache) recordFailure(mediaID int64, f failure, fixed time.Duration) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clk.Now()
	// Forget entries that expired long ago so the map cannot grow forever.
	for id, old := range c.failures {
		if now.Sub(old.retryAt) > longBackoff {
			delete(c.failures, id)
		}
	}
	f.count = c.failures[mediaID].count + 1
	backoff := maxFailureBackoff
	switch {
	case fixed > 0:
		backoff = fixed
	case f.count < 6: // 1m, 2m, 4m, 8m, 16m, then the 30m cap
		backoff = failureBackoff << (f.count - 1)
	}
	f.retryAt = now.Add(backoff)
	c.failures[mediaID] = f
	return backoff
}
