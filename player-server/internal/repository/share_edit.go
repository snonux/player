package repository

import (
	"context"
	"fmt"
	"time"
)

// UpdateShare changes only supplied editable fields; viewings and source stay intact.
func (s *SQLite) UpdateShare(ctx context.Context, token string, description *string, expiresAt *time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE shares SET description = COALESCE(?, description), expires_at = COALESCE(?, expires_at) WHERE token = ?`, description, expiresAt, token)
	if err != nil {
		return false, fmt.Errorf("update share: %w", err)
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
