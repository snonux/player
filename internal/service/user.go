package service

import (
	"context"
	"fmt"

	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// userAdminService handles user account management.
type userAdminService struct {
	store  repository.UserAdminServiceStore
	clock  clock.Clock
	hasher auth.Hasher
}

// NewUserAdminService creates a UserAdminService.
func NewUserAdminService(store repository.UserAdminServiceStore, clk clock.Clock, hasher auth.Hasher) *userAdminService {
	return &userAdminService{store: store, clock: clk, hasher: hasher}
}

func (s *userAdminService) ListUsers(ctx context.Context) ([]model.User, error) {
	return s.store.ListUsers(ctx)
}

func (s *userAdminService) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*model.User, error) {
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      isAdmin,
		CreatedAt:    s.clock.Now(),
	}

	id, err := s.store.CreateUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	user.ID = id
	return user, nil
}

func (s *userAdminService) DeleteUser(ctx context.Context, id int64) error {
	return s.store.DeleteUser(ctx, id)
}
