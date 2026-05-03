package service

import (
	"context"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// accessHelper encapsulates permission checks used by media sub-services.
type accessHelper struct {
	store repository.AccessHelperStore
}

// checkSetPermission verifies that a user has the required role on a set.
// An empty requiredRole means any role is accepted. Admins are always allowed.
func (h *accessHelper) checkSetPermission(ctx context.Context, setID, userID int64, requiredRole model.Role) error {
	user, err := h.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return nil
	}

	perm, err := h.store.GetPermission(ctx, setID, userID)
	if err != nil {
		return fmt.Errorf("get permission: %w", err)
	}
	if perm != nil && (requiredRole == "" || perm.Role == requiredRole) {
		return nil
	}

	set, err := h.store.GetSetByID(ctx, setID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}
	if set != nil {
		for _, p := range set.Permissions {
			if p.UserID == userID && (requiredRole == "" || p.Role == requiredRole) {
				return nil
			}
		}
	}

	return ErrForbidden
}

func (h *accessHelper) verifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := h.store.GetMediaByID(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil || media.DeletedAt != nil {
		return nil, ErrNotFound
	}

	if err := h.checkSetPermission(ctx, media.SetID, userID, ""); err != nil {
		return nil, err
	}

	return media, nil
}

// verifyModifyAccess checks that the user has access to the media and is an owner or admin.
func (h *accessHelper) verifyModifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := h.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}

	if err := h.checkSetPermission(ctx, media.SetID, userID, model.RoleOwner); err != nil {
		return nil, err
	}

	return media, nil
}

// verifySetModifyAccess checks that the user is an owner or admin for a set.
func (h *accessHelper) verifySetModifyAccess(ctx context.Context, setID, userID int64) error {
	return h.checkSetPermission(ctx, setID, userID, model.RoleOwner)
}
