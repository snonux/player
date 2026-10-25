package repository

import (
	"context"
	"fmt"
	"time"
)

// UpdateShare changes only supplied editable fields; viewings and source stay intact.
func (s *SQLite) UpdateShare(ctx context.Context, token string, description *string, expiresAt *time.Time, audioQuality ...*string) (bool, error) {
	var quality *string
	if len(audioQuality) > 0 {
		quality = audioQuality[0]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE shares SET description = COALESCE(?, description), expires_at = COALESCE(?, expires_at), audio_quality = COALESCE(?, audio_quality) WHERE token = ?`, description, expiresAt, quality, token)
	if err != nil {
		return false, fmt.Errorf("update share: %w", err)
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
