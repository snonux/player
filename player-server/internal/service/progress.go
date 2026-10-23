package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// progressService is the concrete implementation of ProgressService.
type progressService struct {
	store  repository.ProgressServiceStore
	helper *accessHelper
	clock  clock.Clock
}

// NewProgressService creates a concrete ProgressService.
func NewProgressService(store repository.ProgressServiceStore, clk clock.Clock) *progressService {
	return &progressService{
		store:  store,
		helper: NewAccessHelper(store),
		clock:  clk,
	}
}

func (s *progressService) UpdateProgress(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error {
	if sessionID == "" {
		return errors.New("session_id required")
	}
	if mediaID == 0 {
		return errors.New("media_id required")
	}

	// verifyAccess catches three cases the raw upsert misses: missing
	// media (ErrNotFound → 404 instead of an FK-violation 500), soft-
	// deleted media (DeletedAt != nil → ErrNotFound), and access denied
	// (user has no permission on the media's set → ErrForbidden → 403).
	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}

	apply := func(store repository.ProgressUpdateStore) error {
		return s.applyProgress(ctx, store, sessionID, userID, mediaID, position, s.clock.Now().Truncate(time.Millisecond))
	}
	if txStore, ok := s.store.(repository.ProgressTransactionStore); ok {
		return txStore.WithProgressTransaction(ctx, apply)
	}
	return apply(s.store)
}

func (s *progressService) BatchUpdateProgress(ctx context.Context, sessionID string, userID int64, updates []ProgressUpdate) error {
	if sessionID == "" {
		return errors.New("session_id required")
	}

	normalized := make([]ProgressUpdate, len(updates))
	now := s.clock.Now()
	for i, update := range updates {
		if update.MediaID == 0 {
			return fmt.Errorf("updates[%d].media_id required", i)
		}
		// Verify access up-front per item so a bad/forbidden/deleted
		// media_id is rejected with ErrNotFound/ErrForbidden (404/403)
		// rather than triggering an FK-violation 500 mid-transaction or
		// silently recording progress on media the user cannot see.
		if _, err := s.helper.verifyAccess(ctx, update.MediaID, userID); err != nil {
			return fmt.Errorf("updates[%d]: %w", i, err)
		}
		if update.ObservedAt.IsZero() {
			update.ObservedAt = now
		}
		update.ObservedAt = update.ObservedAt.Truncate(time.Millisecond)
		normalized[i] = update
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].ObservedAt.Before(normalized[j].ObservedAt)
	})

	apply := func(store repository.ProgressUpdateStore) error {
		for _, update := range normalized {
			if err := s.applyProgress(
				ctx,
				store,
				sessionID,
				userID,
				update.MediaID,
				update.PositionSeconds,
				update.ObservedAt,
			); err != nil {
				return fmt.Errorf("apply progress media_id %d: %w", update.MediaID, err)
			}
		}
		return nil
	}
	if txStore, ok := s.store.(repository.ProgressTransactionStore); ok {
		return txStore.WithProgressTransaction(ctx, apply)
	}
	return apply(s.store)
}

// applyProgress saves one position of a session unless a newer edit
// exists, and advances the session's play-count accumulator: at most 12
// seconds per save count as listening, and 60 counted seconds increment the
// media's play count once per session.
func (s *progressService) applyProgress(
	ctx context.Context,
	store repository.ProgressUpdateStore,
	sessionID string,
	userID, mediaID int64,
	position float64,
	observedAt time.Time,
) error {
	existing, err := store.GetProgress(ctx, userID, mediaID)
	if err != nil {
		return fmt.Errorf("get progress: %w", err)
	}
	if newer, err := newerPositionExists(ctx, store, existing, userID, mediaID, observedAt); err != nil || newer {
		return err
	}
	acc, err := sessionAccumulator(ctx, store, existing, sessionID, mediaID, observedAt)
	if err != nil {
		return err
	}
	delta := min(max(position-acc.LastPosition, 0), 12)
	acc.AccumulatedSeconds += delta
	acc.LastPosition = position
	acc.UpdatedAt = observedAt

	var total float64
	if existing != nil {
		total = existing.AccumulatedSeconds
	}
	if err := store.UpsertProgress(ctx, &model.PlaybackProgress{
		UserID:             userID,
		MediaID:            mediaID,
		PositionSeconds:    position,
		AccumulatedSeconds: total + delta,
		UpdatedAt:          observedAt,
	}); err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}
	if personal, ok := store.(repository.MusicProgressStore); ok {
		initial := float64(0)
		if existing != nil && !existing.Finished {
			initial = existing.PositionSeconds
		}
		if err := personal.ApplyMusicProgress(ctx, userID, sessionID, mediaID, position, initial, observedAt); err != nil {
			return fmt.Errorf("personal music progress: %w", err)
		}
	}
	return countPlay(ctx, store, acc)
}

// sessionAccumulator returns the session's accumulator of the media, or a
// new one starting at the saved position (0 after a finish).
func sessionAccumulator(ctx context.Context, store repository.ProgressUpdateStore, existing *model.PlaybackProgress, sessionID string, mediaID int64, observedAt time.Time) (*model.PlaybackAccumulator, error) {
	acc, err := store.GetAccumulator(ctx, sessionID, mediaID)
	if err != nil {
		return nil, fmt.Errorf("get accumulator: %w", err)
	}
	if acc != nil {
		return acc, nil
	}
	lastPosition := float64(0)
	if existing != nil && !existing.Finished {
		lastPosition = existing.PositionSeconds
	}
	return &model.PlaybackAccumulator{SessionID: sessionID, MediaID: mediaID, LastPosition: lastPosition, UpdatedAt: observedAt}, nil
}

// countPlay increments the play count once the session accumulated 60
// seconds, then stores the accumulator.
func countPlay(ctx context.Context, store repository.ProgressUpdateStore, acc *model.PlaybackAccumulator) error {
	if acc.AccumulatedSeconds >= 60 && !acc.Counted {
		if err := store.IncrementPlayCount(ctx, acc.MediaID); err != nil {
			return fmt.Errorf("increment play count: %w", err)
		}
		acc.Counted = true
	}
	if err := store.UpsertAccumulator(ctx, acc); err != nil {
		return fmt.Errorf("upsert accumulator: %w", err)
	}
	return nil
}

// newerPositionExists reports whether a position save at observedAt is
// stale. A store that knows edit barriers (resets, and for the server copy
// of a podcast episode the position register's own clock) decides; other
// stores compare with the stored progress row.
func newerPositionExists(ctx context.Context, store repository.ProgressUpdateStore, existing *model.PlaybackProgress, userID, mediaID int64, observedAt time.Time) (bool, error) {
	if barrier, ok := store.(interface {
		PositionEditTime(context.Context, int64, int64) (time.Time, error)
	}); ok {
		latest, err := barrier.PositionEditTime(ctx, userID, mediaID)
		if err != nil {
			return false, err
		}
		return !observedAt.After(latest), nil
	}
	return existing != nil && existing.UpdatedAt.After(observedAt), nil
}

// Immediate server actions in the same clock millisecond are distinct edits.
// Advance only ties; a future client edit still wins over an earlier clock.
func (s *progressService) serverProgressTime(ctx context.Context, userID, mediaID int64) (time.Time, error) {
	observed := s.clock.Now().Truncate(time.Millisecond)
	if store, ok := s.store.(interface {
		ProgressEditTime(context.Context, int64, int64) (time.Time, error)
	}); ok {
		latest, err := store.ProgressEditTime(ctx, userID, mediaID)
		if err != nil {
			return observed, err
		}
		if observed.Equal(latest) {
			observed = observed.Add(time.Millisecond)
		}
	}
	return observed, nil
}

func (s *progressService) MarkFinished(ctx context.Context, userID, mediaID int64) error {
	observed, err := s.serverProgressTime(ctx, userID, mediaID)
	if err != nil {
		return err
	}
	return s.SetProgressStatusAt(ctx, userID, mediaID, "finished", observed)
}

func (s *progressService) SetProgressStatusAt(ctx context.Context, userID, mediaID int64, status string, observed time.Time) error {
	observed = observed.Truncate(time.Millisecond)
	if status == "not_started" {
		if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
			return err
		}
		if timed, ok := s.store.(interface {
			ResetProgressAt(context.Context, int64, int64, time.Time) error
		}); ok {
			return timed.ResetProgressAt(ctx, userID, mediaID, observed)
		}
		return s.store.ResetProgress(ctx, userID, mediaID)
	}
	if mediaID == 0 {
		return errors.New("media_id required")
	}

	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}

	if err := s.store.FinishProgress(ctx, &model.PlaybackProgress{
		UserID:          userID,
		MediaID:         mediaID,
		PositionSeconds: media.Duration,
		Finished:        true,
		UpdatedAt:       observed,
	}); err != nil {
		return fmt.Errorf("mark finished: %w", err)
	}

	return nil
}

func (s *progressService) MarkNotStarted(ctx context.Context, userID, mediaID int64) error {
	if mediaID == 0 {
		return errors.New("media_id required")
	}

	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}

	observed, err := s.serverProgressTime(ctx, userID, mediaID)
	if err != nil {
		return err
	}
	if err := s.SetProgressStatusAt(ctx, userID, mediaID, "not_started", observed); err != nil {
		return fmt.Errorf("reset progress: %w", err)
	}

	return nil
}

func (s *progressService) ListInProgress(ctx context.Context, userID int64) ([]model.Media, error) {
	allowed, err := s.helper.allowedSetIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if allowed != nil && len(allowed) == 0 {
		return []model.Media{}, nil
	}

	return s.store.ListInProgressMedia(ctx, userID, repository.MediaFilter{
		AllowedSetIDs: allowed,
	})
}

func (h *accessHelper) allowedSetIDs(ctx context.Context, userID int64) ([]int64, error) {
	user, err := h.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return nil, nil
	}

	perms, err := h.store.ListPermissionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	allowed := make([]int64, 0, len(perms))
	for _, p := range perms {
		allowed = append(allowed, p.SetID)
	}

	return allowed, nil
}
