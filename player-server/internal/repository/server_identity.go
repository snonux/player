package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// ServerIdentityRepo exposes the installation identifier used to pair addresses.
// It identifies a database, not a user, and is not an authentication secret.
type ServerIdentityRepo interface {
	ServerIdentity(context.Context) (string, error)
}

func initializeServerIdentity(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS server_identity (singleton INTEGER PRIMARY KEY CHECK(singleton = 1), identity TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create server identity: %w", err)
	}
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Errorf("generate server identity: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO server_identity(singleton, identity) VALUES(1, ?)`, hex.EncodeToString(value[:])); err != nil {
		return fmt.Errorf("save server identity: %w", err)
	}
	return nil
}

// ServerIdentity returns the persistent, public installation identifier.
func (s *SQLite) ServerIdentity(ctx context.Context) (string, error) {
	var identity string
	if err := s.db.QueryRowContext(ctx, `SELECT identity FROM server_identity WHERE singleton = 1`).Scan(&identity); err != nil {
		return "", fmt.Errorf("read server identity: %w", err)
	}
	return identity, nil
}

var _ ServerIdentityRepo = (*SQLite)(nil)
