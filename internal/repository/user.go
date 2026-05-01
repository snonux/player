package repository

import (
	"context"
	"database/sql"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// CreateUser inserts a new user and returns the generated ID.
func (s *SQLite) CreateUser(ctx context.Context, user *model.User) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		user.Username, user.PasswordHash, boolToInt(user.IsAdmin), user.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert user: %w", err)
	}
	return res.LastInsertId()
}

func scanUser(row sqlScanner) (*model.User, error) {
	var u model.User
	var admin int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &admin, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = admin != 0
	return &u, nil
}

// GetUserByID retrieves a user by ID.
func (s *SQLite) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE id = ?`, id,
	)
	return scanUser(row)
}

// GetUserByUsername retrieves a user by username.
func (s *SQLite) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = ?`, username,
	)
	return scanUser(row)
}

// ListUsers returns all users ordered by username.
func (s *SQLite) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	return scanUsers(rows)
}

func scanUsers(rows *sql.Rows) ([]model.User, error) {
	var users []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// DeleteUser removes a user by ID.
func (s *SQLite) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// CountUsers returns the number of users.
func (s *SQLite) CountUsers(ctx context.Context) (int, error) {
	var n int
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}
