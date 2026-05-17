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

	return s.applyProgress(ctx, s.store, sessionID, userID, mediaID, position, s.clock.Now())
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
		if update.ObservedAt.IsZero() {
			update.ObservedAt = now
		}
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
	if existing != nil && existing.UpdatedAt.After(observedAt) {
		return nil
	}

	if err := store.UpsertProgress(ctx, &model.PlaybackProgress{
		UserID:          userID,
		MediaID:         mediaID,
		PositionSeconds: position,
		UpdatedAt:       observedAt,
	}); err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}

	acc, err := store.GetAccumulator(ctx, sessionID, mediaID)
	if err != nil {
		return fmt.Errorf("get accumulator: %w", err)
	}
	if acc == nil {
		acc = &model.PlaybackAccumulator{
			SessionID:          sessionID,
			MediaID:            mediaID,
			LastPosition:       0,
			AccumulatedSeconds: 0,
			Counted:            false,
			UpdatedAt:          observedAt,
		}
	}

	delta := position - acc.LastPosition
	if delta < 0 {
		delta = 0
	}
	if delta > 12 {
		delta = 12
	}
	acc.AccumulatedSeconds += delta
	acc.LastPosition = position
	acc.UpdatedAt = observedAt

	if acc.AccumulatedSeconds >= 60 && !acc.Counted {
		if err := store.IncrementPlayCount(ctx, mediaID); err != nil {
			return fmt.Errorf("increment play count: %w", err)
		}
		acc.Counted = true
	}

	if err := store.UpsertAccumulator(ctx, acc); err != nil {
		return fmt.Errorf("upsert accumulator: %w", err)
	}

	return nil
}

func (s *progressService) MarkFinished(ctx context.Context, userID, mediaID int64) error {
	if mediaID == 0 {
		return errors.New("media_id required")
	}

	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}

	if err := s.store.UpsertProgress(ctx, &model.PlaybackProgress{
		UserID:          userID,
		MediaID:         mediaID,
		PositionSeconds: media.Duration,
		Finished:        true,
		UpdatedAt:       s.clock.Now(),
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

	if err := s.store.DeleteProgress(ctx, userID, mediaID); err != nil {
		return fmt.Errorf("delete progress: %w", err)
	}
	if err := s.store.DeleteAccumulatorByMedia(ctx, mediaID); err != nil {
		return fmt.Errorf("delete accumulator: %w", err)
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
