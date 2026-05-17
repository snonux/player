package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// Compile-time check that *sessionManager satisfies the SessionManager interface.
var _ SessionManager = (*sessionManager)(nil)

// sessionManager is the concrete implementation of SessionManager.
type sessionManager struct {
	repo    repository.SessionRepo
	clock   clock.Clock
	timeout time.Duration
}

// NewSessionManager creates a SessionManager.
func NewSessionManager(repo repository.SessionRepo, clock clock.Clock, timeout time.Duration) SessionManager {
	return &sessionManager{
		repo:    repo,
		clock:   clock,
		timeout: timeout,
	}
}

func (m *sessionManager) generateID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateSession creates a new session for a user and returns the session ID.
func (m *sessionManager) CreateSession(ctx context.Context, userID int64) (string, error) {
	id, err := m.generateID()
	if err != nil {
		return "", err
	}
	now := m.clock.Now()
	sess := &model.Session{
		ID:        id,
		UserID:    userID,
		ExpiresAt: now.Add(m.timeout),
		CreatedAt: now,
	}
	if err := m.repo.CreateSession(ctx, sess); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// ValidateSession checks if a session ID is valid and not expired.
func (m *sessionManager) ValidateSession(ctx context.Context, id string) (*model.Session, error) {
	sess, err := m.repo.GetSessionByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if sess == nil {
		return nil, nil
	}
	if m.clock.Now().After(sess.ExpiresAt) {
		_ = m.repo.DeleteSession(ctx, id)
		return nil, nil
	}
	return sess, nil
}

// DeleteSession removes a session.
func (m *sessionManager) DeleteSession(ctx context.Context, id string) error {
	return m.repo.DeleteSession(ctx, id)
}

// Cleanup removes expired sessions.
func (m *sessionManager) Cleanup(ctx context.Context) error {
	return m.repo.DeleteExpiredSessions(ctx, m.clock.Now())
}
