package service

import (
	"context"
	"fmt"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// permissionAdminService handles set permission management.
type permissionAdminService struct {
	store repository.PermissionAdminServiceStore
	clock clock.Clock
}

// NewPermissionAdminService creates a PermissionAdminService.
func NewPermissionAdminService(store repository.PermissionAdminServiceStore, clk clock.Clock) *permissionAdminService {
	return &permissionAdminService{store: store, clock: clk}
}

func (s *permissionAdminService) ListPermissions(ctx context.Context) (*PermissionsMatrix, error) {
	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sets: %w", err)
	}

	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	var perms []model.SetPermission
	for _, set := range sets {
		setPerms, err := s.store.ListPermissionsBySet(ctx, set.ID)
		if err != nil {
			return nil, fmt.Errorf("list permissions by set: %w", err)
		}
		perms = append(perms, setPerms...)
	}

	return &PermissionsMatrix{
		Sets:        sets,
		Users:       users,
		Permissions: perms,
	}, nil
}

func (s *permissionAdminService) GrantPermission(ctx context.Context, setID, userID int64, role model.Role) error {
	perm := &model.SetPermission{
		SetID:     setID,
		UserID:    userID,
		Role:      role,
		CreatedAt: s.clock.Now(),
	}
	return s.store.GrantPermission(ctx, perm)
}

func (s *permissionAdminService) RevokePermission(ctx context.Context, setID, userID int64) error {
	return s.store.RevokePermission(ctx, setID, userID)
}
