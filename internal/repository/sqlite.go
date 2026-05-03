package repository

import (
	"context"
	"database/sql"
	"fmt"

	// Register the pure-Go SQLite driver with database/sql.
	_ "modernc.org/sqlite"
)

// SQLite is a concrete Store implementation backed by SQLite.
type SQLite struct {
	db *sql.DB
}

// New creates a SQLite store from an existing *sql.DB after migrating the schema.
func New(db *sql.DB) (*SQLite, error) {
	if err := Migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLite{db: db}, nil
}

// Open opens a SQLite database at the given DSN and returns a connected Store.
func Open(dsn string) (*SQLite, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s, err := New(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *SQLite) Close() error {
	return s.db.Close()
}

// Ping checks the database connection health.
func (s *SQLite) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

var _ Store = (*SQLite)(nil)

type sqlScanner interface {
	Scan(dest ...any) error
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
