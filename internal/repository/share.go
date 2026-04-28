package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/paul/kiss-media-player/internal/model"
)

// CreateShare inserts a new share link.
func (s *SQLite) CreateShare(ctx context.Context, share *model.Share) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO shares (token, media_id, created_by, created_at, expires_at, max_uses, used_count) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		share.Token, share.MediaID, share.CreatedBy, share.CreatedAt, share.ExpiresAt, sqlNullInt(share.MaxUses), share.UsedCount,
	)
	if err != nil {
		return fmt.Errorf("insert share: %w", err)
	}
	return nil
}

func scanShare(row sqlScanner) (*model.Share, error) {
	var sh model.Share
	var maxUses sql.NullInt64
	if err := row.Scan(&sh.Token, &sh.MediaID, &sh.CreatedBy, &sh.CreatedAt, &sh.ExpiresAt, &maxUses, &sh.UsedCount); err != nil {
		return nil, err
	}
	if maxUses.Valid {
		n := int(maxUses.Int64)
		sh.MaxUses = &n
	}
	return &sh, nil
}

// GetShareByToken retrieves a share by its token.
func (s *SQLite) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT token, media_id, created_by, created_at, expires_at, max_uses, used_count FROM shares WHERE token = ?`, token)
	return scanShare(row)
}

// ListSharesByMedia returns shares for a media item.
func (s *SQLite) ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token, media_id, created_by, created_at, expires_at, max_uses, used_count FROM shares WHERE media_id = ? ORDER BY created_at DESC`, mediaID)
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	defer rows.Close()
	var shares []model.Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		shares = append(shares, *sh)
	}
	return shares, rows.Err()
}

// UseShare increments the used_count of a share token.
func (s *SQLite) UseShare(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shares SET used_count = used_count + 1 WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("use share: %w", err)
	}
	return nil
}

// DeleteShare removes a share by token.
func (s *SQLite) DeleteShare(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM shares WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("delete share: %w", err)
	}
	return nil
}

// DeleteExpiredShares removes shares with expired_at older than now.
func (s *SQLite) DeleteExpiredShares(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM shares WHERE expires_at < ?`, now)
	if err != nil {
		return fmt.Errorf("delete expired shares: %w", err)
	}
	return nil
}

func sqlNullInt(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*n), Valid: true}
}
