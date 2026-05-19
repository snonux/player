package service

import (
	"context"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// authService is the concrete implementation of AuthService.
type authService struct {
	store  repository.AuthServiceStore
	clock  clock.Clock
	hasher auth.Hasher
	sm     auth.SessionManager
	tm     auth.TokenManager
}

// NewAuthService creates a concrete AuthService.
// tm is required and must not be nil — passing nil will panic. This is
// intentional: the service depends on an injected TokenManager per DIP and
// refuses to silently fabricate one. Production wiring (cmd/player/main.go)
// constructs the TokenManager at the composition root via auth.NewTokenManager.
func NewAuthService(store repository.AuthServiceStore, clk clock.Clock, hasher auth.Hasher, sm auth.SessionManager, tm auth.TokenManager) *authService {
	if tm == nil {
		panic("service.NewAuthService: tm (auth.TokenManager) must not be nil")
	}
	return &authService{
		store:  store,
		clock:  clk,
		hasher: hasher,
		sm:     sm,
		tm:     tm,
	}
}

// Bootstrap creates the first admin user when no users exist.
func (s *authService) Bootstrap(ctx context.Context, username, password string) (*AuthResult, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil, ErrAlreadyBootstrapped
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      true,
		CreatedAt:    s.clock.Now(),
	}

	id, err := s.store.CreateUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	user.ID = id

	sessID, err := s.sm.CreateSession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &AuthResult{User: user, SessionID: sessID}, nil
}

// Login authenticates a user and creates a session.
func (s *authService) Login(ctx context.Context, username, password string) (*AuthResult, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	if err := s.hasher.Compare(user.PasswordHash, password); err != nil {
		return nil, ErrInvalidCredentials
	}

	sessID, err := s.sm.CreateSession(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &AuthResult{User: user, SessionID: sessID}, nil
}

// CreateAPIToken creates a hashed API token and returns the one-time plaintext value.
// A token generation failure (e.g. crypto/rand.Read returning an error) is
// surfaced to the caller; the HTTP layer translates it into a 500 response.
func (s *authService) CreateAPIToken(ctx context.Context, userID int64, name string, expiresAt *time.Time) (*CreateAPITokenResult, error) {
	plaintext, hash, err := s.tm.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate api token: %w", err)
	}
	now := s.clock.Now()
	token := &model.APIToken{
		UserID:    userID,
		TokenHash: hash,
		Name:      name,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	id, err := s.store.Create(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("create api token: %w", err)
	}
	token.ID = id

	return &CreateAPITokenResult{Token: token, Plaintext: plaintext}, nil
}

// ListAPITokens returns API tokens owned by a user.
func (s *authService) ListAPITokens(ctx context.Context, userID int64) ([]model.APIToken, error) {
	return s.store.ListByUser(ctx, userID)
}

// RevokeAPIToken deletes an API token owned by a user.
func (s *authService) RevokeAPIToken(ctx context.Context, userID, tokenID int64) error {
	tokens, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list api tokens: %w", err)
	}
	if !hasAPIToken(tokens, tokenID) {
		return ErrNotFound
	}
	if err := s.store.DeleteByID(ctx, tokenID); err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	return nil
}

// AuthenticateBearer validates a Bearer token and returns a synthetic session.
func (s *authService) AuthenticateBearer(ctx context.Context, plaintext string) (*model.Session, error) {
	if plaintext == "" {
		return nil, ErrInvalidCredentials
	}

	token, err := s.store.GetByHash(ctx, s.tm.Hash(plaintext))
	if err != nil {
		return nil, fmt.Errorf("get api token: %w", err)
	}
	if token == nil {
		return nil, ErrInvalidCredentials
	}

	now := s.clock.Now()
	if token.ExpiresAt != nil && now.After(*token.ExpiresAt) {
		return nil, ErrInvalidCredentials
	}
	_ = s.store.TouchLastUsed(ctx, token.ID, now)

	return syntheticSession(token, now), nil
}

// CountUsers returns the number of user accounts.
func (s *authService) CountUsers(ctx context.Context) (int, error) {
	return s.store.CountUsers(ctx)
}

// GetUserByID returns a user by database ID.
func (s *authService) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return s.store.GetUserByID(ctx, id)
}

func hasAPIToken(tokens []model.APIToken, tokenID int64) bool {
	for _, token := range tokens {
		if token.ID == tokenID {
			return true
		}
	}
	return false
}

func syntheticSession(token *model.APIToken, now time.Time) *model.Session {
	expiresAt := now.Add(100 * 365 * 24 * time.Hour)
	if token.ExpiresAt != nil {
		expiresAt = *token.ExpiresAt
	}
	return &model.Session{
		ID:        fmt.Sprintf("api-token:%d", token.ID),
		UserID:    token.UserID,
		ExpiresAt: expiresAt,
		CreatedAt: token.CreatedAt,
	}
}
