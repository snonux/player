package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/snonux/player/internal/model"
)

const (
	// shareViewingKeyName is the server_secrets row holding the key that
	// signs share viewing credentials.
	shareViewingKeyName = "share_viewing_key"
	// shareViewingKeyBytes is the key length; 32 bytes is the output size of
	// HMAC-SHA256, the recommended key size for it.
	shareViewingKeyBytes = 32
)

// CreateShare inserts a new share link.
func (s *SQLite) CreateShare(ctx context.Context, share *model.Share) error {
	ids, err := json.Marshal(share.MediaIDs)
	if err != nil {
		return fmt.Errorf("encode share collection: %w", err)
	}
	var mediaID any = share.MediaID
	if len(share.MediaIDs) > 0 {
		mediaID = nil
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO shares (token, media_id, created_by, created_at, expires_at, max_uses, used_count, collection_title, media_ids, description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		share.Token, mediaID, share.CreatedBy, share.CreatedAt, share.ExpiresAt, sqlNullInt(share.MaxUses), share.UsedCount, share.CollectionTitle, string(ids), share.Description,
	)
	if err != nil {
		return fmt.Errorf("insert share: %w", err)
	}
	return nil
}

func scanShare(row sqlScanner) (*model.Share, error) {
	var sh model.Share
	var maxUses sql.NullInt64
	var ids string
	var mediaID sql.NullInt64
	if err := row.Scan(&sh.Token, &mediaID, &sh.CreatedBy, &sh.CreatedAt, &sh.ExpiresAt, &maxUses, &sh.UsedCount, &sh.CollectionTitle, &ids, &sh.Description); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	sh.MediaID = mediaID.Int64
	if err := json.Unmarshal([]byte(ids), &sh.MediaIDs); err != nil {
		return nil, fmt.Errorf("decode share collection: %w", err)
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
		`SELECT token, media_id, created_by, created_at, expires_at, max_uses, used_count, collection_title, media_ids, description FROM shares WHERE token = ?`, token)
	return scanShare(row)
}

// ListSharesByMedia returns shares for a media item.
func (s *SQLite) ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token, media_id, created_by, created_at, expires_at, max_uses, used_count, collection_title, media_ids, description FROM shares WHERE media_id = ? ORDER BY created_at DESC`, mediaID)
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

// ListSharesByUser returns all shares created by a specific user.
func (s *SQLite) ListSharesByUser(ctx context.Context, userID int64) ([]model.Share, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token, media_id, created_by, created_at, expires_at, max_uses, used_count, collection_title, media_ids, description FROM shares WHERE created_by = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list shares by user: %w", err)
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
func (s *SQLite) UseShare(ctx context.Context, token string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE shares SET used_count = used_count + 1
		WHERE token = ? AND expires_at > ? AND (max_uses IS NULL OR used_count < max_uses)`, token, now)
	if err != nil {
		return false, fmt.Errorf("use share: %w", err)
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

// ShareViewingKey returns the key that signs share viewing credentials,
// creating it on first use.
//
// The key is stored in the database rather than kept in memory or taken from
// the environment: viewings then survive a server restart without any
// configuration, and two instances that briefly share the database during a
// rolling update agree on it (INSERT OR IGNORE lets the first writer win and
// both read back the stored value).
func (s *SQLite) ShareViewingKey(ctx context.Context) ([]byte, error) {
	candidate := make([]byte, shareViewingKeyBytes)
	if _, err := rand.Read(candidate); err != nil {
		return nil, fmt.Errorf("generate share viewing key: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO server_secrets (name, value) VALUES (?, ?)`, shareViewingKeyName, candidate); err != nil {
		return nil, fmt.Errorf("store share viewing key: %w", err)
	}
	var key []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT value FROM server_secrets WHERE name = ?`, shareViewingKeyName).Scan(&key); err != nil {
		return nil, fmt.Errorf("load share viewing key: %w", err)
	}
	return key, nil
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
