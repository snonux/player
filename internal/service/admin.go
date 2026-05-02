package service

import (
	"context"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/scanner"
)

// adminService is the concrete implementation of AdminService.
type adminService struct {
	store     repository.AdminServiceStore
	clock     clock.Clock
	hasher    auth.Hasher
	scanner   scanner.Scanner
	mediaRoot string
	progress  *model.ScanProgress
}

// NewAdminService creates a concrete AdminService.
func NewAdminService(store repository.AdminServiceStore, clk clock.Clock, hasher auth.Hasher, sc scanner.Scanner, mediaRoot string) AdminService {
	return &adminService{
		store:     store,
		clock:     clk,
		hasher:    hasher,
		scanner:   sc,
		mediaRoot: mediaRoot,
		progress:  &model.ScanProgress{},
	}
}

func (s *adminService) ListTrash(ctx context.Context) ([]model.Media, error) {
	return s.store.ListDeletedMedia(ctx)
}

func (s *adminService) TriggerRescan(ctx context.Context) error {
	if s.scanner == nil {
		return fmt.Errorf("scanner not configured")
	}
	// Run the scan in a background goroutine so the HTTP request
	// returns immediately and the scan continues asynchronously.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := s.scanner.Scan(ctx, s.mediaRoot, s.progress); err != nil {
			s.progress.Done(err)
			fmt.Printf("[rescan] scan failed: %v\n", err)
		} else {
			s.progress.Done(nil)
			fmt.Printf("[rescan] scan completed\n")
		}
	}()
	return nil
}

func (s *adminService) ScanProgress(ctx context.Context) model.ScanProgress {
	return s.progress.Copy()
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

func (s *adminService) ListPermissions(ctx context.Context) (*PermissionsMatrix, error) {
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
