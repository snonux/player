package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/snonux/player/internal/model"
)

// GrantPermission inserts or replaces a set permission.
func (s *SQLite) GrantPermission(ctx context.Context, perm *model.SetPermission) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO set_permissions (set_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
		perm.SetID, perm.UserID, string(perm.Role), perm.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("grant permission: %w", err)
	}
	return nil
}

// RevokePermission deletes a set permission.
func (s *SQLite) RevokePermission(ctx context.Context, setID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM set_permissions WHERE set_id = ? AND user_id = ?`, setID, userID,
	)
	if err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}
	return nil
}

func scanPermission(row sqlScanner) (*model.SetPermission, error) {
	var p model.SetPermission
	err := row.Scan(&p.SetID, &p.UserID, &p.Role, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPermission retrieves a single permission for a set and user.
func (s *SQLite) GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT set_id, user_id, role, created_at FROM set_permissions WHERE set_id = ? AND user_id = ?`,
		setID, userID,
	)
	return scanPermission(row)
}

// ListPermissionsBySet returns all permissions for a set.
func (s *SQLite) ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error) {
	return queryList(ctx, s.db, "list permissions by set", byValue(scanPermission),
		`SELECT set_id, user_id, role, created_at FROM set_permissions WHERE set_id = ?`, setID)
}

// ListPermissionsByUser returns all permissions for a user.
func (s *SQLite) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error) {
	return queryList(ctx, s.db, "list permissions by user", byValue(scanPermission),
		`SELECT set_id, user_id, role, created_at FROM set_permissions WHERE user_id = ?`, userID)
}
