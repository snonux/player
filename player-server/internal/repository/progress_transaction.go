package repository

import (
	"context"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// WithProgressTransaction runs progress updates in one SQLite transaction.
func (s *SQLite) WithProgressTransaction(ctx context.Context, fn func(ProgressUpdateStore) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin progress transaction: %w", err)
	}

	txStore := &progressTxStore{tx: tx}
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

type progressTxStore struct {
	tx sqlProgressTx
}

type sqlProgressTx interface {
	sqlExecer
	sqlQueryRower
}

func (s *progressTxStore) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	return upsertProgress(ctx, s.tx, progress)
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
