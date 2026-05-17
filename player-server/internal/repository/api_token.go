package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// Create inserts a new API token and returns the generated ID.
func (s *SQLite) Create(ctx context.Context, token *model.APIToken) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, token_hash, name, last_used_at, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		token.UserID, token.TokenHash, token.Name, sqlNullTime(token.LastUsedAt), sqlNullTime(token.ExpiresAt), token.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert api token: %w", err)
	}
	return res.LastInsertId()
}

func scanAPIToken(row sqlScanner) (*model.APIToken, error) {
	var token model.APIToken
	var lastUsedAt, expiresAt sql.NullTime
	err := row.Scan(&token.ID, &token.UserID, &token.TokenHash, &token.Name, &lastUsedAt, &expiresAt, &token.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		token.ExpiresAt = &expiresAt.Time
	}
	return &token, nil
}

// GetByHash retrieves an API token by token hash.
func (s *SQLite) GetByHash(ctx context.Context, tokenHash string) (*model.APIToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, name, last_used_at, expires_at, created_at FROM api_tokens WHERE token_hash = ?`,
		tokenHash,
	)
	return scanAPIToken(row)
}

// ListByUser returns all API tokens for a user ordered newest first.
func (s *SQLite) ListByUser(ctx context.Context, userID int64) ([]model.APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, token_hash, name, last_used_at, expires_at, created_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list api tokens by user: %w", err)
	}
	defer rows.Close()

	var tokens []model.APIToken
	for rows.Next() {
		token, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, *token)
	}
	return tokens, rows.Err()
}

// DeleteByID removes an API token by database ID.
func (s *SQLite) DeleteByID(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete api token: %w", err)
	}
	return nil
}

// TouchLastUsed updates an API token's last-used timestamp.
func (s *SQLite) TouchLastUsed(ctx context.Context, id int64, lastUsedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, lastUsedAt, id)
	if err != nil {
		return fmt.Errorf("touch api token last used: %w", err)
	}
	return nil
}
