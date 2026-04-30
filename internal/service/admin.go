package service

import (
	"context"
	"fmt"

	"codeberg.org/snonux/play/internal/auth"
	"codeberg.org/snonux/play/internal/clock"
	"codeberg.org/snonux/play/internal/model"
	"codeberg.org/snonux/play/internal/repository"
)

// adminService is the concrete implementation of AdminService.
type adminService struct {
	store  repository.AdminServiceStore
	clock  clock.Clock
	hasher auth.Hasher
}

// NewAdminService creates a concrete AdminService.
func NewAdminService(store repository.AdminServiceStore, clk clock.Clock, hasher auth.Hasher) AdminService {
	return &adminService{
		store:  store,
		clock:  clk,
		hasher: hasher,
	}
}

func (s *adminService) ListTrash(ctx context.Context) ([]model.Media, error) {
	return s.store.ListDeletedMedia(ctx)
}

func (s *adminService) TriggerRescan(ctx context.Context) error {
	// No-op; scanner will be wired later.
	return nil
}

func (s *adminService) ListUsers(ctx context.Context) ([]model.User, error) {
	return s.store.ListUsers(ctx)
}

func (s *adminService) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*model.User, error) {
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

func (s *adminService) DeleteUser(ctx context.Context, id int64) error {
	return s.store.DeleteUser(ctx, id)
}

func (s *adminService) ListPermissions(ctx context.Context) ([]model.SetPermission, error) {
	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sets: %w", err)
	}

	var perms []model.SetPermission
	for _, set := range sets {
		setPerms, err := s.store.ListPermissionsBySet(ctx, set.ID)
		if err != nil {
			return nil, fmt.Errorf("list permissions by set: %w", err)
		}
		perms = append(perms, setPerms...)
	}

	return perms, nil
}

func (s *adminService) GrantPermission(ctx context.Context, setID, userID int64, role model.Role) error {
	perm := &model.SetPermission{
		SetID:     setID,
		UserID:    userID,
		Role:      role,
		CreatedAt: s.clock.Now(),
	}
	return s.store.GrantPermission(ctx, perm)
}

func (s *adminService) RevokePermission(ctx context.Context, setID, userID int64) error {
	return s.store.RevokePermission(ctx, setID, userID)
}
