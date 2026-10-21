package repository

import (
	"context"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// WithProgressTransaction runs progress updates in one SQLite transaction.
func (s *SQLite) WithProgressTransaction(ctx context.Context, fn func(ProgressUpdateStore) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin progress transaction: %w", err)
	}

	txStore := &progressTxStore{tx: tx, now: s.now}
	if err := fn(txStore); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("rollback progress transaction after %w: %v", err, rollbackErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit progress transaction: %w", err)
	}
	return nil
}

// progressTxStore runs the progress service's reads and writes on one
// transaction.
type progressTxStore struct {
	tx sqlTx
	// now is the store's server time (see SQLite.now).
	now func() time.Time
}

// UpsertProgress writes progress, or the episode registers of an episode
// copy (see upsertProgressOrEpisode).
func (s *progressTxStore) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	return upsertProgressOrEpisode(ctx, s.tx, progress, s.now())
}

func (s *progressTxStore) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	return getProgress(ctx, s.tx, userID, mediaID)
}

func (s *progressTxStore) GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
	return getAccumulator(ctx, s.tx, sessionID, mediaID)
}

func (s *progressTxStore) UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error {
	return upsertAccumulator(ctx, s.tx, acc)
}

func (s *progressTxStore) IncrementPlayCount(ctx context.Context, id int64) error {
	return incrementPlayCount(ctx, s.tx, id)
}

var _ ProgressUpdateStore = (*progressTxStore)(nil)
