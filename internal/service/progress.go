package service

import (
	"context"
	"fmt"

	"github.com/paul/kiss-media-player/internal/clock"
	"github.com/paul/kiss-media-player/internal/model"
	"github.com/paul/kiss-media-player/internal/repository"
)

// progressService is the concrete implementation of ProgressService.
type progressService struct {
	store repository.ProgressServiceStore
	clock clock.Clock
}

// NewProgressService creates a concrete ProgressService.
func NewProgressService(store repository.ProgressServiceStore, clk clock.Clock) ProgressService {
	return &progressService{
		store: store,
		clock: clk,
	}
}

func (s *progressService) UpdateProgress(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error {
	now := s.clock.Now()

	if err := s.store.UpsertProgress(ctx, &model.PlaybackProgress{
		UserID:          userID,
		MediaID:         mediaID,
		PositionSeconds: position,
		UpdatedAt:       now,
	}); err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}

	acc, err := s.store.GetAccumulator(ctx, sessionID, mediaID)
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
			UpdatedAt:          now,
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
	acc.UpdatedAt = now

	if acc.AccumulatedSeconds >= 60 && !acc.Counted {
		if err := s.store.IncrementPlayCount(ctx, mediaID); err != nil {
			return fmt.Errorf("increment play count: %w", err)
		}
		acc.Counted = true
	}

	if err := s.store.UpsertAccumulator(ctx, acc); err != nil {
		return fmt.Errorf("upsert accumulator: %w", err)
	}

	return nil
}
