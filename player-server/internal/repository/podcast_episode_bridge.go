package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// The bridge between the media progress routes and episode state
// (docs/podcast-sync-contract.md §7.1). For a media row that is the server
// copy of a podcast episode, the legacy routes (progress, batch, status,
// playback-sync) become views onto the caller's episode registers:
//
//   - their last-writer-wins decisions use the registers' clocks, not
//     playback_progress, so a position save and a completion never block
//     each other (§4 rule 5);
//   - their clocks are clamped like sync API clocks (§4 rule 4);
//   - playback_progress is only the projection the registers write
//     (writeEpisodeMirror), which old clients and the In progress list
//     read. Each route keeps its own effect on the media-scoped play-count
//     state: a save raises accumulated_seconds, a finish or reset that
//     changed a register starts a new playback cycle as before.
//
// Media that is no episode copy keeps the original media-scoped behaviour.

// linkedEpisode returns the user's episode state key of the podcast episode
// whose server copy is mediaID; ok is false for any other media.
func linkedEpisode(ctx context.Context, q sqlQueryRower, userID, mediaID int64) (episodeStateKey, bool, error) {
	k := episodeStateKey{userID: userID}
	err := q.QueryRowContext(ctx, `SELECT f.feed_key, COALESCE(a.episode_key, e.episode_key) FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id
LEFT JOIN podcast_episode_aliases a ON a.feed_key = f.feed_key AND a.alias_key = e.episode_key WHERE e.media_id = ? ORDER BY e.id LIMIT 1`, mediaID).Scan(&k.feedKey, &k.episodeKey)
	if err == sql.ErrNoRows {
		return k, false, nil
	}
	if err != nil {
		return k, false, fmt.Errorf("find linked episode: %w", err)
	}
	return k, true, nil
}

// applyLinkedEdit merges an edit a legacy route made on an episode copy,
// with its clocks clamped against the server's time now (the store's
// clock, the same as the services').
func applyLinkedEdit(ctx context.Context, q sqlTx, k episodeStateKey, e episodeEdit, now time.Time) (episodeMerge, error) {
	return mergeEpisodeEdit(ctx, q, k, e.clamped(now), "")
}

// editTime returns the time a new edit of the media's progress must be
// later than. For an episode copy that is the position register's clock
// when positionOnly (a position save only competes with positions),
// otherwise the later of both registers' clocks (a server action that must
// not tie with either). For other media it is the latest progress or
// reset time, whatever the edit.
func editTime(ctx context.Context, q sqlQueryRower, userID, mediaID int64, positionOnly bool) (time.Time, error) {
	k, linked, err := linkedEpisode(ctx, q, userID, mediaID)
	if err != nil {
		return time.Time{}, err
	}
	if !linked {
		return latestProgressTime(ctx, q, userID, mediaID)
	}
	r, _, err := loadRegisters(ctx, q, k)
	if err != nil {
		return time.Time{}, err
	}
	ms := r.positionMS
	if !positionOnly {
		ms = max(ms, r.completionMS)
	}
	if ms == 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(ms).UTC(), nil
}

// saveLinkedProgress records a position save (or, for a finished row, a
// manual finish) of an episode copy and raises the copy's accumulated play
// time like an ordinary progress save.
func saveLinkedProgress(ctx context.Context, tx sqlTx, k episodeStateKey, p *model.PlaybackProgress, now time.Time) error {
	if p.Finished {
		return finishLinkedProgress(ctx, tx, k, p.MediaID, p.UpdatedAt, now)
	}
	if _, err := applyLinkedEdit(ctx, tx, k, positionEdit(p.PositionSeconds, p.UpdatedAt.UnixMilli()), now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE playback_progress SET accumulated_seconds = MAX(accumulated_seconds, ?)
WHERE user_id = ? AND media_id = ?`, p.AccumulatedSeconds, k.userID, p.MediaID)
	if err != nil {
		return fmt.Errorf("save progress accumulation: %w", err)
	}
	return nil
}

// finishLinkedProgress records POST /progress/status "finished" on an
// episode copy: a manual completion that keeps the position register (the
// route cannot tell the web's Mark as finished from a cast end).
func finishLinkedProgress(ctx context.Context, tx sqlTx, k episodeStateKey, mediaID int64, observed, now time.Time) error {
	m, err := applyLinkedEdit(ctx, tx, k, completionEdit(true, completionManual, observed.UnixMilli()), now)
	if err != nil || !m.changed() {
		return err
	}
	return restartPlaybackCycle(ctx, tx, k.userID, mediaID)
}

// resetLinkedProgress records POST /progress/status "not_started" on an
// episode copy: position 0 and a manual uncompletion with the same clock.
func resetLinkedProgress(ctx context.Context, tx sqlTx, k episodeStateKey, mediaID int64, observed, now time.Time) error {
	ms := observed.UnixMilli()
	m, err := applyLinkedEdit(ctx, tx, k, positionEdit(0, ms).withCompletion(false, completionManual, ms), now)
	if err != nil || !m.changed() {
		return err
	}
	return restartPlaybackCycle(ctx, tx, k.userID, mediaID)
}

// syncLinkedProgress records a playback-sync progress edit of an episode
// copy: a save is a position edit; a natural end also an automatic
// completion with the same clock, which starts a new playback cycle.
func syncLinkedProgress(ctx context.Context, tx sqlTx, k episodeStateKey, mediaID int64, p *model.SyncedProgress, now time.Time) error {
	e := positionEdit(p.PositionSeconds, p.ObservedMS)
	if p.Finished {
		e = e.withCompletion(true, completionAuto, p.ObservedMS)
	}
	m, err := applyLinkedEdit(ctx, tx, k, e, now)
	if err != nil || !p.Finished || !m.completionChanged() {
		return err
	}
	return restartPlaybackCycle(ctx, tx, k.userID, mediaID)
}

// linkedSyncResult projects the registers of k into the playback-sync
// answer for copy mediaID exactly as the mirror projects them: position
// (0 when unfinished near the end), finished and the later of both clocks;
// nil when neither register was ever set.
func linkedSyncResult(ctx context.Context, q sqlQueryRower, k episodeStateKey, mediaID int64) (*model.SyncedProgress, error) {
	r, _, err := loadRegisters(ctx, q, k)
	if err != nil {
		return nil, err
	}
	ms := max(r.positionMS, r.completionMS)
	if ms == 0 {
		return nil, nil
	}
	duration, err := mediaDuration(ctx, q, mediaID)
	if err != nil {
		return nil, err
	}
	return &model.SyncedProgress{PositionSeconds: r.mirrorPosition(duration), Finished: r.mirrorFinished(), ObservedMS: ms}, nil
}

// restartPlaybackCycle clears the media-scoped play-count state of one
// user's copy after a finish or reset, as those routes do for any media:
// the session accumulators go and the mirror's accumulated time is zeroed.
func restartPlaybackCycle(ctx context.Context, tx sqlTx, userID, mediaID int64) error {
	if err := deleteUserAccumulators(ctx, tx, userID, mediaID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE playback_progress SET accumulated_seconds = 0 WHERE user_id = ? AND media_id = ?`,
		userID, mediaID)
	if err != nil {
		return fmt.Errorf("restart playback cycle: %w", err)
	}
	return nil
}

// seedEpisodeMirrors projects every user's state of an episode onto its
// new server copy mediaID, so a fresh download starts from the history
// every client already shares (§7.1).
func seedEpisodeMirrors(ctx context.Context, tx sqlTx, episodeID, mediaID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.user_id, s.position_seconds, s.position_ms, s.completed, s.completion_ms
FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id
LEFT JOIN podcast_episode_aliases a ON a.feed_key = f.feed_key AND a.alias_key = e.episode_key
JOIN podcast_episode_state s ON s.feed_key = f.feed_key AND s.episode_key = COALESCE(a.episode_key, e.episode_key)
WHERE e.id = ? AND (s.position_ms > 0 OR s.completion_ms > 0)`, episodeID)
	if err != nil {
		return fmt.Errorf("list episode states: %w", err)
	}
	type userState struct {
		userID int64
		r      episodeRegisters
	}
	var states []userState
	for rows.Next() {
		var st userState
		if err := rows.Scan(&st.userID, &st.r.position, &st.r.positionMS, &st.r.completed, &st.r.completionMS); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan episode state: %w", err)
		}
		states = append(states, st)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, st := range states {
		if err := writeEpisodeMirror(ctx, tx, st.userID, mediaID, &st.r); err != nil {
			return fmt.Errorf("seed progress mirror: %w", err)
		}
	}
	return nil
}
