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
