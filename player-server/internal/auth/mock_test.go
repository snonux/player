package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestMockSessionManager_Defaults(t *testing.T) {
	ctx := context.Background()
	m := &MockSessionManager{}

	if id, err := m.CreateSession(ctx, 1); err != nil || id != "" {
		t.Fatalf("expected empty id, got %q, err %v", id, err)
	}
	if sess, err := m.ValidateSession(ctx, "abc"); err != nil || sess != nil {
		t.Fatalf("expected nil session, got %v, err %v", sess, err)
	}
	if err := m.DeleteSession(ctx, "abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Cleanup(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockSessionManager_WithFuncs(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	m := &MockSessionManager{
		CreateSessionFunc: func(ctx context.Context, userID int64) (string, error) {
			return "sess-id", nil
		},
		ValidateSessionFunc: func(ctx context.Context, id string) (*model.Session, error) {
			return &model.Session{ID: id, UserID: 1, ExpiresAt: now.Add(time.Hour)}, nil
		},
		DeleteSessionFunc: func(ctx context.Context, id string) error {
			return errors.New("boom")
		},
		CleanupFunc: func(ctx context.Context) error {
			return errors.New("cleanup boom")
		},
	}

	if id, err := m.CreateSession(ctx, 1); err != nil || id != "sess-id" {
		t.Fatalf("unexpected create result: %q, %v", id, err)
	}
	if sess, err := m.ValidateSession(ctx, "abc"); err != nil || sess == nil || sess.ID != "abc" {
		t.Fatalf("unexpected validate result: %v, %v", sess, err)
	}
	if err := m.DeleteSession(ctx, "abc"); err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
	if err := m.Cleanup(ctx); err == nil || err.Error() != "cleanup boom" {
		t.Fatalf("expected cleanup boom error, got %v", err)
	}
}
