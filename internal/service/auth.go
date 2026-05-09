package service

import (
	"context"
	"fmt"

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
}

// NewAuthService creates a concrete AuthService.
func NewAuthService(store repository.AuthServiceStore, clk clock.Clock, hasher auth.Hasher, sm auth.SessionManager) AuthService {
	return &authService{
		store:  store,
		clock:  clk,
		hasher: hasher,
		sm:     sm,
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

// CountUsers returns the number of user accounts.
func (s *authService) CountUsers(ctx context.Context) (int, error) {
	return s.store.CountUsers(ctx)
}

// GetUserByID returns a user by database ID.
func (s *authService) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return s.store.GetUserByID(ctx, id)
}
