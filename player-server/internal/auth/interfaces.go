package auth

import (
	"context"

	"codeberg.org/snonux/player/internal/model"
)

// SessionManager is the abstraction for session lifecycle operations.
type SessionManager interface {
	CreateSession(ctx context.Context, userID int64) (string, error)
	ValidateSession(ctx context.Context, id string) (*model.Session, error)
	DeleteSession(ctx context.Context, id string) error
	Cleanup(ctx context.Context) error
}

// TokenManager is the abstraction for API token generation and hashing.
// Generate returns an error when the system random source fails so callers
// can surface the failure (e.g. via HTTP 500) instead of panicking.
type TokenManager interface {
	Generate() (plaintext, hash string, err error)
	Hash(plaintext string) string
}
