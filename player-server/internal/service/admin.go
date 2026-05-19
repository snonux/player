package service

import (
	"context"
	"log/slog"

	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/scanner"
)

// Compile-time check that *adminService satisfies AdminService.
var _ AdminService = (*adminService)(nil)

// adminService is the concrete implementation of AdminService.
// It composes role-focused sub-services to satisfy SRP.
type adminService struct {
	*trashService
	*scanService
	*userAdminService
	*permissionAdminService
}

// NewAdminService creates a concrete AdminService.
func NewAdminService(store repository.AdminServiceStore, clk clock.Clock, hasher auth.Hasher, sc scanner.Scanner, mediaRoot string, appCtx context.Context) *adminService {
	return NewAdminServiceWithLogger(store, clk, hasher, sc, mediaRoot, appCtx, slog.Default())
}

// NewAdminServiceWithLogger creates a concrete AdminService with an injected logger.
func NewAdminServiceWithLogger(store repository.AdminServiceStore, clk clock.Clock, hasher auth.Hasher, sc scanner.Scanner, mediaRoot string, appCtx context.Context, logger *slog.Logger) *adminService {
	return &adminService{
		trashService:           NewTrashService(store),
		scanService:            NewScanService(appCtx, sc, mediaRoot, clk, logger),
		userAdminService:       NewUserAdminService(store, clk, hasher),
		permissionAdminService: NewPermissionAdminService(store, clk),
	}
}

// ListTrash delegates to trashService.
func (s *adminService) ListTrash(ctx context.Context) ([]model.Media, error) {
	return s.trashService.ListTrash(ctx)
}

// TriggerRescan delegates to scanService.
func (s *adminService) TriggerRescan(ctx context.Context) error {
	return s.scanService.TriggerRescan(ctx)
}

// ScanProgress delegates to scanService.
func (s *adminService) ScanProgress(ctx context.Context) model.ScanProgress {
	return s.scanService.ScanProgress(ctx)
}

// ListUsers delegates to userAdminService.
func (s *adminService) ListUsers(ctx context.Context) ([]model.User, error) {
	return s.userAdminService.ListUsers(ctx)
}

// CreateUser delegates to userAdminService.
func (s *adminService) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*model.User, error) {
	return s.userAdminService.CreateUser(ctx, username, password, isAdmin)
}

// DeleteUser delegates to userAdminService.
func (s *adminService) DeleteUser(ctx context.Context, callerID, id int64) error {
	return s.userAdminService.DeleteUser(ctx, callerID, id)
}

// ListPermissions delegates to permissionAdminService.
func (s *adminService) ListPermissions(ctx context.Context) (*PermissionsMatrix, error) {
	return s.permissionAdminService.ListPermissions(ctx)
}

// GrantPermission delegates to permissionAdminService.
func (s *adminService) GrantPermission(ctx context.Context, setID, userID int64, role model.Role) error {
	return s.permissionAdminService.GrantPermission(ctx, setID, userID, role)
}

// RevokePermission delegates to permissionAdminService.
func (s *adminService) RevokePermission(ctx context.Context, setID, userID int64) error {
	return s.permissionAdminService.RevokePermission(ctx, setID, userID)
}
