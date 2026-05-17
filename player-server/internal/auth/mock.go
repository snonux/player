package auth

import (
	"context"

	"codeberg.org/snonux/player/internal/model"
)

// Compile-time check that MockSessionManager satisfies the interface.
var _ SessionManager = (*MockSessionManager)(nil)

// MockSessionManager is a test double for SessionManager.
type MockSessionManager struct {
	CreateSessionFunc   func(ctx context.Context, userID int64) (string, error)
	ValidateSessionFunc func(ctx context.Context, id string) (*model.Session, error)
	DeleteSessionFunc   func(ctx context.Context, id string) error
	CleanupFunc         func(ctx context.Context) error
}

// CreateSession delegates to CreateSessionFunc.
func (m *MockSessionManager) CreateSession(ctx context.Context, userID int64) (string, error) {
	if m.CreateSessionFunc != nil {
		return m.CreateSessionFunc(ctx, userID)
	}
	return "", nil
}

// ValidateSession delegates to ValidateSessionFunc.
func (m *MockSessionManager) ValidateSession(ctx context.Context, id string) (*model.Session, error) {
	if m.ValidateSessionFunc != nil {
		return m.ValidateSessionFunc(ctx, id)
	}
	return nil, nil
}

// DeleteSession delegates to DeleteSessionFunc.
func (m *MockSessionManager) DeleteSession(ctx context.Context, id string) error {
	if m.DeleteSessionFunc != nil {
		return m.DeleteSessionFunc(ctx, id)
	}
	return nil
}

// Cleanup delegates to CleanupFunc.
func (m *MockSessionManager) Cleanup(ctx context.Context) error {
	if m.CleanupFunc != nil {
		return m.CleanupFunc(ctx)
	}
	return nil
}
