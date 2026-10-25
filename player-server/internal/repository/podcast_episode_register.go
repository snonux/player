package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/snonux/player/internal/model"
)

// Writing the per-account episode registers (docs/podcast-sync-contract.md
// §4, §7.1). They are the only history of a podcast episode: the sync API,
// the legacy toggle and the media progress routes of a server copy all
// merge their edits here, and the playback_progress row of every server
// copy is rewritten afterwards as a projection, never the other way round.

// PodcastEpisodeStateRepo writes per-account episode registers outside the
// sync API.
type PodcastEpisodeStateRepo interface {
	// GetMediaEpisodeState returns the linked episode identity and caller's
	// registers, or nil for ordinary media. Access is checked by the service.
	GetMediaEpisodeState(ctx context.Context, userID, mediaID int64) (*model.PodcastEpisodeState, error)
	// ToggleEpisodeCompletion flips the user's completion register of a
	// catalog episode by a manual edit at server time (the legacy toggle)
	// and returns the new value. It wraps sql.ErrNoRows when the episode
	// does not exist.
	ToggleEpisodeCompletion(ctx context.Context, userID, episodeID int64, now time.Time) (bool, error)
}

var _ PodcastEpisodeStateRepo = (*SQLite)(nil)

// GetMediaEpisodeState implements PodcastEpisodeStateRepo.
func (s *SQLite) GetMediaEpisodeState(ctx context.Context, userID, mediaID int64) (*model.PodcastEpisodeState, error) {
	k, linked, err := linkedEpisode(ctx, s.db, userID, mediaID)
	if err != nil || !linked {
		return nil, err
	}
	state, err := getEpisodeState(ctx, s.db, userID, k.feedKey, k.episodeKey)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &model.PodcastEpisodeState{FeedKey: k.feedKey, EpisodeKey: k.episodeKey}
	}
	return state, nil
}

// sqlTx is what register writes need from a transaction: they read the
// linked server copies with a multi-row query besides single-row reads
// and writes.
type sqlTx interface {
	sqlExecer
	sqlQueryRower
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// episodeEdit is an edit of one or both registers of an episode. A nil
// register is not edited; each register carries its own clock.
type episodeEdit struct {
	position     *float64
	positionMS   int64
	completed    *bool
	source       string
	completionMS int64
}

// positionEdit is a position-only edit at ms.
func positionEdit(seconds float64, ms int64) episodeEdit {
	return episodeEdit{position: &seconds, positionMS: ms}
}

// completionEdit is a completion-only edit at ms.
func completionEdit(completed bool, source string, ms int64) episodeEdit {
	return episodeEdit{completed: &completed, source: source, completionMS: ms}
}

// withCompletion adds a completion edit at ms to e.
func (e episodeEdit) withCompletion(completed bool, source string, ms int64) episodeEdit {
	e.completed, e.source, e.completionMS = &completed, source, ms
	return e
}

// clamped replaces every clock more than model.PodcastClockSkew ahead of
// now by now (contract §4 rule 4), so an edit from a device with a future
// clock cannot block every later edit of that register.
func (e episodeEdit) clamped(now time.Time) episodeEdit {
	limit, at := now.Add(model.PodcastClockSkew).UnixMilli(), now.UnixMilli()
	if e.positionMS > limit {
		e.positionMS = at
	}
	if e.completionMS > limit {
		e.completionMS = at
	}
	return e
}

// apply merges e into r by the registers' own clocks.
func (r *episodeRegisters) apply(e episodeEdit) {
	if e.position != nil {
		r.setPosition(*e.position, e.positionMS)
	}
	if e.completed != nil {
		r.setCompletion(*e.completed, e.source, e.completionMS)
	}
}

// episodeMerge is the outcome of merging an edit: the registers before and
// after it, and whether a stored row existed before.
type episodeMerge struct {
	before, after episodeRegisters
	existed       bool
}

// changed reports whether the edit replaced a register.
func (m episodeMerge) changed() bool { return m.before != m.after }

// completionChanged reports whether the edit replaced the completion
// register.
func (m episodeMerge) completionChanged() bool {
	return m.before.completionMS != m.after.completionMS
}

// mergeEpisodeEdit merges e into the registers of k. When a register
// changed, the row is stored under a new change sequence number and every
// server copy's playback_progress mirror is rewritten, all inside q's
// transaction. A stale or replayed edit changes nothing. enclosureURL is
// the latest evidence for alias resolution; "" keeps the stored one.
func mergeEpisodeEdit(ctx context.Context, q sqlTx, k episodeStateKey, e episodeEdit, enclosureURL string) (episodeMerge, error) {
	r, existed, err := loadRegisters(ctx, q, k)
	if err != nil {
		return episodeMerge{}, err
	}
	m := episodeMerge{before: r, after: r, existed: existed}
	m.after.apply(e)
	if !m.changed() {
		return m, nil
	}
	if err := putEpisodeState(ctx, q, k, m.after, enclosureURL); err != nil {
		return m, err
	}
	return m, writeEpisodeMirrors(ctx, q, k, &m.after)
}

// putEpisodeState stores the registers of k under a fresh change sequence
// number.
func putEpisodeState(ctx context.Context, q sqlTx, k episodeStateKey, r episodeRegisters, enclosureURL string) error {
	seq, err := nextChangeSeq(ctx, q)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO podcast_episode_state
(user_id, feed_key, episode_key, position_seconds, position_ms, completed, completion_source, completion_ms, enclosure_url, change_seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, feed_key, episode_key) DO UPDATE SET position_seconds = excluded.position_seconds,
 position_ms = excluded.position_ms, completed = excluded.completed, completion_source = excluded.completion_source,
 completion_ms = excluded.completion_ms, change_seq = excluded.change_seq,
 enclosure_url = CASE WHEN excluded.enclosure_url = '' THEN podcast_episode_state.enclosure_url ELSE excluded.enclosure_url END`,
		k.userID, k.feedKey, k.episodeKey, r.position, r.positionMS, boolToInt(r.completed), r.completionSource,
		r.completionMS, enclosureURL, seq)
	if err != nil {
		return fmt.Errorf("store episode state: %w", err)
	}
	return nil
}

// loadRegisters returns the stored registers of k and whether a row
// exists; empty registers when none does.
func loadRegisters(ctx context.Context, q sqlQueryRower, k episodeStateKey) (episodeRegisters, bool, error) {
	var r episodeRegisters
	err := q.QueryRowContext(ctx, `SELECT position_seconds, position_ms, completed, completion_source, completion_ms
FROM podcast_episode_state WHERE user_id = ? AND feed_key = ? AND episode_key = ?`, k.userID, k.feedKey, k.episodeKey).Scan(
		&r.position, &r.positionMS, &r.completed, &r.completionSource, &r.completionMS)
	if err == sql.ErrNoRows {
		return episodeRegisters{}, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("load episode state: %w", err)
	}
	return r, true, nil
}

// writeEpisodeMirrors projects the registers of k onto the user's
// playback_progress row of every server copy of the episode. Catalog rows
// merged into another share its feed key, so their copies are included.
func writeEpisodeMirrors(ctx context.Context, q sqlTx, k episodeStateKey, r *episodeRegisters) error {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT e.media_id FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id
JOIN media m ON m.id = e.media_id
LEFT JOIN podcast_episode_aliases a ON a.feed_key = f.feed_key AND a.alias_key = e.episode_key
WHERE f.feed_key = ? AND COALESCE(a.episode_key, e.episode_key) = ?`, k.feedKey, k.episodeKey)
	if err != nil {
		return fmt.Errorf("list server copies: %w", err)
	}
	var mediaIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan server copy: %w", err)
		}
		mediaIDs = append(mediaIDs, id)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, id := range mediaIDs {
		if err := writeEpisodeMirror(ctx, q, k.userID, id, r); err != nil {
			return fmt.Errorf("write progress mirror: %w", err)
		}
	}
	return nil
}

// ToggleEpisodeCompletion implements PodcastEpisodeStateRepo. The edit is
// manual, at server time advanced 1 ms past the stored clock (contract §4
// rule 3), so the toggle always wins, even over a stored clock ahead of
// the server. Reading the register and writing the flipped value happen in
// one transaction, together with the mirrors of the episode's server copies.
func (s *SQLite) ToggleEpisodeCompletion(ctx context.Context, userID, episodeID int64, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("toggle episode completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	k := episodeStateKey{userID: userID}
	err = tx.QueryRowContext(ctx, `SELECT f.feed_key, COALESCE(a.episode_key, e.episode_key) FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id
LEFT JOIN podcast_episode_aliases a ON a.feed_key = f.feed_key AND a.alias_key = e.episode_key WHERE e.id = ?`, episodeID).Scan(&k.feedKey, &k.episodeKey)
	if err != nil {
		return false, fmt.Errorf("toggle episode completion: find episode %d: %w", episodeID, err)
	}
	r, _, err := loadRegisters(ctx, tx, k)
	if err != nil {
		return false, err
	}
	completed := !r.completed
	ms := max(now.UnixMilli(), r.completionMS+1)
	if _, err := mergeEpisodeEdit(ctx, tx, k, completionEdit(completed, completionManual, ms), ""); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("toggle episode completion: %w", err)
	}
	return completed, nil
}
