package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/paul/kiss-media-player/internal/clock"
	"github.com/paul/kiss-media-player/internal/model"
	"github.com/paul/kiss-media-player/internal/repository"
)

func TestBCryptHasher_HashAndCompare(t *testing.T) {
	h := NewBCryptHasher(4) // low cost for speed
	tests := []struct {
		name     string
		password string
	}{
		{"simple password", "hello"},
		{"long password", "averylongpasswordthatexceeds32charactersormore"},
		{"unicode password", "пароль密码🔐"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := h.Hash(tt.password)
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if hash == "" {
				t.Fatal("expected non-empty hash")
			}
			if err := h.Compare(hash, tt.password); err != nil {
				t.Fatalf("compare correct password: %v", err)
			}
			if err := h.Compare(hash, tt.password+"x"); err == nil {
				t.Fatal("expected error for wrong password")
			}
			// ensure same password yields different hash (salted)
			hash2, _ := h.Hash(tt.password)
			if hash == hash2 {
				t.Fatal("expected different hashes for same password")
			}
		})
	}
}

func TestSessionManager_CreateSession(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clk := &clock.MockClock{T: now}

	var created *model.Session
	mockRepo := repository.MockSessionRepo{
		CreateSessionFunc: func(ctx context.Context, session *model.Session) error {
			created = session
			return nil
		},
	}

	sm := NewSessionManager(&mockRepo, clk, time.Hour)

	id, err := sm.CreateSession(context.Background(), 42)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session id")
	}
	if created == nil {
		t.Fatal("expected session to be created")
	}
	if created.UserID != 42 {
		t.Fatalf("expected user id 42, got %d", created.UserID)
	}
	if created.ExpiresAt != now.Add(time.Hour) {
		t.Fatalf("unexpected expires at: %v", created.ExpiresAt)
	}
}

func TestSessionManager_ValidateSession(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clk := &clock.MockClock{T: now}

	tests := []struct {
		name       string
		returns    *model.Session
		returnsErr error
		expectNil  bool
		expectDel  bool
	}{
		{
			name: "valid session",
			returns: &model.Session{
				ID:        "abc",
				UserID:    1,
				ExpiresAt: now.Add(time.Hour),
				CreatedAt: now.Add(-time.Hour),
			},
			expectNil: false,
		},
		{
			name:      "session not found",
			returns:   nil,
			expectNil: true,
		},
		{
			name: "expired session",
			returns: &model.Session{
				ID:        "old",
				UserID:    1,
				ExpiresAt: now.Add(-time.Minute),
				CreatedAt: now.Add(-time.Hour),
			},
			expectNil: true,
			expectDel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var deleted string
			repo := repository.MockSessionRepo{
				GetSessionByIDFunc: func(ctx context.Context, id string) (*model.Session, error) {
					return tt.returns, tt.returnsErr
				},
				DeleteSessionFunc: func(ctx context.Context, id string) error {
					deleted = id
					return nil
				},
			}
			sm := NewSessionManager(&repo, clk, time.Hour)
			sess, err := sm.ValidateSession(context.Background(), "testID")
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if (sess == nil) != tt.expectNil {
				t.Fatalf("expected nil=%v, got %v", tt.expectNil, sess)
			}
			if tt.expectDel && deleted == "" {
				t.Fatal("expected expired session to be deleted")
			}
			if !tt.expectDel && deleted != "" {
				t.Fatal("unexpected delete")
			}
		})
	}
}

func TestSessionManager_DeleteSession(t *testing.T) {
	repo := repository.MockSessionRepo{
		DeleteSessionFunc: func(ctx context.Context, id string) error {
			if id != "abc" {
				return errors.New("unexpected id")
			}
			return nil
		},
	}
	sm := NewSessionManager(&repo, &clock.MockClock{T: time.Now()}, time.Hour)
	if err := sm.DeleteSession(context.Background(), "abc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestSessionManager_Cleanup(t *testing.T) {
	now := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	clk := &clock.MockClock{T: now}

	var called time.Time
	repo := repository.MockSessionRepo{
		DeleteExpiredSessionsFunc: func(ctx context.Context, t time.Time) error {
			called = t
			return nil
		},
	}

	sm := NewSessionManager(&repo, clk, time.Hour)
	if err := sm.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if called != now {
		t.Fatalf("expected cleanup called with %v, got %v", now, called)
	}
}
