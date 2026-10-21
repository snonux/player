package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Episode state registers (docs/podcast-sync-contract.md, §4 and §7.1).
// Position and completion are independent last-writer-wins registers: an
// edit replaces a register only when its clock is strictly greater, so ties
// keep the stored value and a replayed edit changes nothing.

// Completion sources of the completion register.
const (
	completionManual = "manual"
	completionAuto   = "auto"
)

// episodeStateKey identifies one user's state of one episode.
type episodeStateKey struct {
	userID              int64
	feedKey, episodeKey string
}

// episodeRegisters are the two registers of an episode state row. A clock
// of 0 means the register was never set.
type episodeRegisters struct {
	position         float64
	positionMS       int64
	completed        bool
	completionSource string
	completionMS     int64
}

// setPosition applies a position edit if its clock is newer.
func (r *episodeRegisters) setPosition(seconds float64, ms int64) {
	if ms > r.positionMS {
		r.position, r.positionMS = seconds, ms
	}
}

// setCompletion applies a completion edit if its clock is newer.
func (r *episodeRegisters) setCompletion(completed bool, source string, ms int64) {
	if ms > r.completionMS {
		r.completed, r.completionSource, r.completionMS = completed, source, ms
	}
}

// mirrorFinished is the legacy single-register projection of completion:
// the later edit decides, so a completed episode replayed after its
// completion projects as unfinished (old clients then resume it).
func (r *episodeRegisters) mirrorFinished() bool {
	return r.completed && r.completionMS >= r.positionMS
}

// mirrorTime is the projection's edit time: the later of both clocks.
func (r *episodeRegisters) mirrorTime() time.Time {
	return time.UnixMilli(max(r.positionMS, r.completionMS)).UTC()
}

// backfillEpisodeState is steps 5 and 6: merge the episode history kept in
// podcast_status and in the playback progress and resets of downloaded
// episodes into per-account registers, then store one row per episode.
func backfillEpisodeState(ctx context.Context, tx *sql.Tx, seq *migrationSeq) error {
	states := map[episodeStateKey]*episodeRegisters{}
	for _, load := range []func(context.Context, *sql.Tx, map[episodeStateKey]*episodeRegisters) error{
		mergePodcastStatus, mergeLinkedProgress, mergeLinkedResets,
	} {
		if err := load(ctx, tx, states); err != nil {
			return err
		}
	}
	keys := make([]episodeStateKey, 0, len(states))
	for k := range states {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.userID != b.userID {
			return a.userID < b.userID
		}
		if a.feedKey != b.feedKey {
			return a.feedKey < b.feedKey
		}
		return a.episodeKey < b.episodeKey
	})
	for _, k := range keys {
		if err := insertEpisodeState(ctx, tx, k, states[k], seq.take()); err != nil {
			return err
		}
	}
	return nil
}

// registersOf returns the registers of k, creating empty ones.
func registersOf(states map[episodeStateKey]*episodeRegisters, k episodeStateKey) *episodeRegisters {
	r := states[k]
	if r == nil {
		r = &episodeRegisters{}
		states[k] = r
	}
	return r
}

// episodeHistoryFrom joins a per-episode history table to the episode's
// feed and episode keys, skipping rows of deleted users.
const episodeHistoryFrom = `
JOIN podcast_feeds f ON f.id = e.feed_id
JOIN users u ON u.id = h.user_id`

// mergePodcastStatus is step 5: the legacy toggle becomes a manual
// completion edit; a stored position (never written by today's code) only
// when it is positive.
func mergePodcastStatus(ctx context.Context, tx *sql.Tx, states map[episodeStateKey]*episodeRegisters) error {
	rows, err := tx.QueryContext(ctx, `SELECT h.user_id, f.feed_key, e.episode_key,
 h.is_completed, h.position_seconds, h.updated_at
FROM podcast_status h JOIN podcast_episodes e ON e.id = h.episode_id`+episodeHistoryFrom+`
ORDER BY h.user_id, e.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k episodeStateKey
		var completed bool
		var position float64
		var updated sql.NullTime
		if err := rows.Scan(&k.userID, &k.feedKey, &k.episodeKey, &completed, &position, &updated); err != nil {
			_ = rows.Close()
			return err
		}
		r := registersOf(states, k)
		r.setCompletion(completed, completionManual, clockOf(updated))
		if position > 0 {
			r.setPosition(position, clockOf(updated))
		}
	}
	return closeRows(rows)
}

// mergeLinkedProgress is the first half of step 6: media progress of a
// downloaded episode is a position edit, and a finish an automatic
// completion, both at the progress's edit time.
func mergeLinkedProgress(ctx context.Context, tx *sql.Tx, states map[episodeStateKey]*episodeRegisters) error {
	rows, err := tx.QueryContext(ctx, `SELECT h.user_id, f.feed_key, e.episode_key,
 h.position_seconds, h.finished, h.updated_at
FROM playback_progress h JOIN podcast_episodes e ON e.media_id = h.media_id`+episodeHistoryFrom+`
ORDER BY h.user_id, e.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k episodeStateKey
		var position float64
		var finished bool
		var updated sql.NullTime
		if err := rows.Scan(&k.userID, &k.feedKey, &k.episodeKey, &position, &finished, &updated); err != nil {
			_ = rows.Close()
			return err
		}
		r := registersOf(states, k)
		r.setPosition(position, clockOf(updated))
		if finished {
			r.setCompletion(true, completionAuto, clockOf(updated))
		}
	}
	return closeRows(rows)
}

// mergeLinkedResets is the second half of step 6: a media progress reset
// of a downloaded episode is position 0 and a manual uncompletion, where
// newer than what is known.
func mergeLinkedResets(ctx context.Context, tx *sql.Tx, states map[episodeStateKey]*episodeRegisters) error {
	rows, err := tx.QueryContext(ctx, `SELECT h.user_id, f.feed_key, e.episode_key, h.observed_ms
FROM playback_resets h JOIN podcast_episodes e ON e.media_id = h.media_id`+episodeHistoryFrom+`
ORDER BY h.user_id, e.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k episodeStateKey
		var ms int64
		if err := rows.Scan(&k.userID, &k.feedKey, &k.episodeKey, &ms); err != nil {
			_ = rows.Close()
			return err
		}
		r := registersOf(states, k)
		r.setPosition(0, ms)
		r.setCompletion(false, completionManual, ms)
	}
	return closeRows(rows)
}

// insertEpisodeState writes a new episode state row.
func insertEpisodeState(ctx context.Context, tx *sql.Tx, k episodeStateKey, r *episodeRegisters, changeSeq int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO podcast_episode_state
(user_id, feed_key, episode_key, position_seconds, position_ms, completed, completion_source, completion_ms, change_seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.userID, k.feedKey, k.episodeKey, r.position, r.positionMS, boolToInt(r.completed),
		r.completionSource, r.completionMS, changeSeq)
	return err
}

// rewriteAllEpisodeMirrors is step 7: project the episode state of every
// downloaded episode onto its playback_progress row, so legacy readers see
// the migrated history from the first request on. Catalog rows merged into
// another share its feed key and are projected too.
func rewriteAllEpisodeMirrors(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.user_id, e.media_id, s.position_seconds, s.position_ms,
 s.completed, s.completion_ms
FROM podcast_episode_state s
JOIN podcast_feeds f ON f.feed_key = s.feed_key
JOIN podcast_episodes e ON e.feed_id = f.id AND e.episode_key = s.episode_key
JOIN media m ON m.id = e.media_id
ORDER BY s.user_id, e.media_id`)
	if err != nil {
		return err
	}
	type mirror struct {
		userID, mediaID int64
		r               episodeRegisters
	}
	var mirrors []mirror
	for rows.Next() {
		var m mirror
		if err := rows.Scan(&m.userID, &m.mediaID, &m.r.position, &m.r.positionMS, &m.r.completed, &m.r.completionMS); err != nil {
			_ = rows.Close()
			return err
		}
		mirrors = append(mirrors, m)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, m := range mirrors {
		if err := writeEpisodeMirror(ctx, tx, m.userID, m.mediaID, &m.r); err != nil {
			return err
		}
	}
	return nil
}

// writeEpisodeMirror rewrites one media item's playback_progress row as the
// projection of episode state (§7.1). It is its own statement, not
// upsertProgress or a finish/reset helper, so accumulated_seconds and the
// play-count state are kept.
func writeEpisodeMirror(ctx context.Context, tx sqlExecer, userID, mediaID int64, r *episodeRegisters) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO playback_progress (user_id, media_id, position_seconds, finished, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id, media_id) DO UPDATE SET
 position_seconds = excluded.position_seconds, finished = excluded.finished, updated_at = excluded.updated_at`,
		userID, mediaID, r.position, boolToInt(r.mirrorFinished()), r.mirrorTime())
	return err
}

// Dual-write of legacy edits (until the episode registers become the only
// history, task wh3). The legacy routes still write podcast_status and
// playback_progress; each of those writes also records the same edit in
// the caller's episode registers, in the same transaction, so nothing a
// user does between this release and the switch of the readers is lost.

// linkedEdit is an edit of one or both episode registers. A nil register
// is not edited.
type linkedEdit struct {
	position   *float64
	completed  *bool
	source     string
	observedMS int64
}

// positionEdit is a position-only edit at ms.
func positionEdit(seconds float64, ms int64) linkedEdit {
	return linkedEdit{position: &seconds, observedMS: ms}
}

// completionEdit is a completion-only edit at ms.
func completionEdit(completed bool, source string, ms int64) linkedEdit {
	return linkedEdit{completed: &completed, source: source, observedMS: ms}
}

// recordLinkedEpisodeEdit applies e to the user's registers of the podcast
// episode whose server copy is mediaID; media that is no episode copy is
// left alone. Registers merge by their own clocks, so a stale edit changes
// nothing and a changed row takes a new change sequence number.
func recordLinkedEpisodeEdit(ctx context.Context, q sqlProgressTx, userID, mediaID int64, e linkedEdit) error {
	var k episodeStateKey
	err := q.QueryRowContext(ctx, `SELECT f.feed_key, e.episode_key FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id WHERE e.media_id = ?`, mediaID).Scan(&k.feedKey, &k.episodeKey)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find linked episode: %w", err)
	}
	k.userID = userID
	return recordEpisodeEdit(ctx, q, k, e)
}

// recordEpisodeEdit merges e into the registers of k and stores them when
// they changed.
func recordEpisodeEdit(ctx context.Context, q sqlProgressTx, k episodeStateKey, e linkedEdit) error {
	r, err := loadRegisters(ctx, q, k)
	if err != nil {
		return err
	}
	before := r
	if e.position != nil {
		r.setPosition(*e.position, e.observedMS)
	}
	if e.completed != nil {
		r.setCompletion(*e.completed, e.source, e.observedMS)
	}
	if r == before {
		return nil
	}
	seq, err := nextChangeSeq(ctx, q)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO podcast_episode_state
(user_id, feed_key, episode_key, position_seconds, position_ms, completed, completion_source, completion_ms, change_seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, feed_key, episode_key) DO UPDATE SET position_seconds = excluded.position_seconds,
 position_ms = excluded.position_ms, completed = excluded.completed, completion_source = excluded.completion_source,
 completion_ms = excluded.completion_ms, change_seq = excluded.change_seq`,
		k.userID, k.feedKey, k.episodeKey, r.position, r.positionMS, boolToInt(r.completed), r.completionSource, r.completionMS, seq)
	if err != nil {
		return fmt.Errorf("record episode state: %w", err)
	}
	return nil
}

// loadRegisters returns the stored registers of k, empty when none exist.
func loadRegisters(ctx context.Context, q sqlQueryRower, k episodeStateKey) (episodeRegisters, error) {
	var r episodeRegisters
	err := q.QueryRowContext(ctx, `SELECT position_seconds, position_ms, completed, completion_source, completion_ms
FROM podcast_episode_state WHERE user_id = ? AND feed_key = ? AND episode_key = ?`, k.userID, k.feedKey, k.episodeKey).Scan(
		&r.position, &r.positionMS, &r.completed, &r.completionSource, &r.completionMS)
	if err != nil && err != sql.ErrNoRows {
		return r, fmt.Errorf("load episode state: %w", err)
	}
	return r, nil
}

// SetEpisodeCompletion implements PodcastEpisodeStateRepo: a manual
// completion edit at server time, advanced 1 ms past the stored clock so a
// server action always wins (contract §4 rule 3).
func (s *SQLite) SetEpisodeCompletion(ctx context.Context, userID, episodeID int64, completed bool, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set episode completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	k := episodeStateKey{userID: userID}
	err = tx.QueryRowContext(ctx, `SELECT f.feed_key, e.episode_key FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id WHERE e.id = ?`, episodeID).Scan(&k.feedKey, &k.episodeKey)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find episode: %w", err)
	}
	r, err := loadRegisters(ctx, tx, k)
	if err != nil {
		return err
	}
	ms := max(now.UnixMilli(), r.completionMS+1)
	if err := recordEpisodeEdit(ctx, tx, k, completionEdit(completed, completionManual, ms)); err != nil {
		return err
	}
	return tx.Commit()
}
