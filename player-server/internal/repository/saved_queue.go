package repository

import (
	"codeberg.org/snonux/player/internal/model"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// SavedQueueRepo persists portable sessions using optimistic concurrency.
type SavedQueueRepo interface {
	GetSavedQueue(context.Context, int64) (*model.SavedQueue, error)
	PutSavedQueue(context.Context, int64, int64, model.SavedQueue) (*model.SavedQueue, bool, error)
}

func initializeSavedQueues(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS saved_queues (user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, revision INTEGER NOT NULL, snapshot TEXT NOT NULL)`)
	return err
}

// GetSavedQueue returns revision zero for an account without a saved session.
func (s *SQLite) GetSavedQueue(ctx context.Context, userID int64) (*model.SavedQueue, error) {
	return readSavedQueue(ctx, s.db, userID)
}

// PutSavedQueue atomically compares the revision. Retries of the last successful
// mutation are acknowledged without incrementing the revision again.
func (s *SQLite) PutSavedQueue(ctx context.Context, userID, expected int64, q model.SavedQueue) (*model.SavedQueue, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	q.Revision = expected + 1
	raw, err := json.Marshal(q)
	if err != nil {
		return nil, false, err
	}
	// One conditional write also serializes competing initial (revision 0) saves.
	result, err := tx.ExecContext(ctx, `INSERT INTO saved_queues(user_id,revision,snapshot) SELECT ?,?,? WHERE ?=0 OR EXISTS(SELECT 1 FROM saved_queues WHERE user_id=?) ON CONFLICT(user_id) DO UPDATE SET revision=excluded.revision,snapshot=excluded.snapshot WHERE saved_queues.revision=?`, userID, q.Revision, string(raw), expected, userID, expected)
	if err != nil {
		return nil, false, fmt.Errorf("save queue: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	stored, err := readSavedQueue(ctx, tx, userID)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return stored, n == 1 || (q.MutationID != "" && stored.MutationID == q.MutationID), nil
}

func readSavedQueue(ctx context.Context, db sqlQueryRower, userID int64) (*model.SavedQueue, error) {
	q := &model.SavedQueue{Entries: []model.QueueOccurrence{}, Canonical: []int64{}, UpNext: []int64{}, Repeat: "off"}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT snapshot FROM saved_queues WHERE user_id=?`, userID).Scan(&raw)
	if err == sql.ErrNoRows {
		return q, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), q); err != nil {
		return nil, fmt.Errorf("read saved queue: %w", err)
	}
	return q, nil
}
