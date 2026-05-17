package repository

import (
	"context"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestSQLite_APITokenRepo(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()

	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	userID := mustCreateAPITokenUser(t, ctx, s, "api-user")
	otherUserID := mustCreateAPITokenUser(t, ctx, s, "other-api-user")

	expiresAt := now.Add(24 * time.Hour)
	firstID, err := s.Create(ctx, &model.APIToken{
		UserID:    userID,
		TokenHash: "hash-one",
		Name:      "first token",
		ExpiresAt: &expiresAt,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create first token: %v", err)
	}

	got, err := s.GetByHash(ctx, "hash-one")
	if err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if got == nil {
		t.Fatal("expected token, got nil")
	}
	assertAPIToken(t, got, firstID, userID, "hash-one", "first token", nil, &expiresAt, now)

	lastUsedAt := now.Add(time.Hour)
	secondID, err := s.Create(ctx, &model.APIToken{
		UserID:     userID,
		TokenHash:  "hash-two",
		Name:       "second token",
		LastUsedAt: &lastUsedAt,
		CreatedAt:  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create second token: %v", err)
	}
	if _, err := s.Create(ctx, &model.APIToken{
		UserID:    otherUserID,
		TokenHash: "hash-other",
		Name:      "other token",
		CreatedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("create other user token: %v", err)
	}

	tokens, err := s.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].ID != secondID || tokens[1].ID != firstID {
		t.Fatalf("unexpected list order: %+v", tokens)
	}

	touchedAt := now.Add(2 * time.Hour)
	if err := s.TouchLastUsed(ctx, firstID, touchedAt); err != nil {
		t.Fatalf("touch last used: %v", err)
	}
	got, err = s.GetByHash(ctx, "hash-one")
	if err != nil {
		t.Fatalf("get touched token: %v", err)
	}
	assertTimePtr(t, got.LastUsedAt, &touchedAt, "last used after touch")

	if err := s.DeleteByID(ctx, firstID); err != nil {
		t.Fatalf("delete by id: %v", err)
	}
	got, err = s.GetByHash(ctx, "hash-one")
	if err != nil {
		t.Fatalf("get deleted token: %v", err)
	}
	if got != nil {
		t.Fatalf("expected deleted token to be nil, got %+v", got)
	}
}

func mustCreateAPITokenUser(t *testing.T, ctx context.Context, s *SQLite, username string) int64 {
	t.Helper()
	id, err := s.CreateUser(ctx, &model.User{
		Username:     username,
		PasswordHash: "hash",
		CreatedAt:    time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	return id
}

func assertAPIToken(t *testing.T, token *model.APIToken, id, userID int64, tokenHash, name string, lastUsedAt, expiresAt *time.Time, createdAt time.Time) {
	t.Helper()
	if token.ID != id {
		t.Fatalf("expected id %d, got %d", id, token.ID)
	}
	if token.UserID != userID {
		t.Fatalf("expected user id %d, got %d", userID, token.UserID)
	}
	if token.TokenHash != tokenHash {
		t.Fatalf("expected token hash %q, got %q", tokenHash, token.TokenHash)
	}
	if token.Name != name {
		t.Fatalf("expected name %q, got %q", name, token.Name)
	}
	assertTimePtr(t, token.LastUsedAt, lastUsedAt, "last used")
	assertTimePtr(t, token.ExpiresAt, expiresAt, "expires")
	if !token.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created at %s, got %s", createdAt, token.CreatedAt)
	}
}

func assertTimePtr(t *testing.T, got, want *time.Time, field string) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("expected %s %v, got %v", field, want, got)
		}
		return
	}
	if !got.Equal(*want) {
		t.Fatalf("expected %s %s, got %s", field, *want, *got)
	}
}
