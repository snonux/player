package service

import (
	"context"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// checkSetPermission verifies that a user has the required role on a set.
// An empty requiredRole means any role is accepted. Admins are always allowed.
func (s *mediaService) checkSetPermission(ctx context.Context, setID, userID int64, requiredRole model.Role) error {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return nil
	}

	perm, err := s.store.GetPermission(ctx, setID, userID)
	if err != nil {
		return fmt.Errorf("get permission: %w", err)
	}
	if perm != nil && (requiredRole == "" || perm.Role == requiredRole) {
		return nil
	}

	set, err := s.store.GetSetByID(ctx, setID)
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

func (s *mediaService) verifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := s.store.GetMediaByID(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil || media.DeletedAt != nil {
		return nil, ErrNotFound
	}

	if err := s.checkSetPermission(ctx, media.SetID, userID, ""); err != nil {
		return nil, err
	}

	return media, nil
}

// verifyModifyAccess checks that the user has access to the media and is an owner or admin.
func (s *mediaService) verifyModifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := s.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}

	if err := s.checkSetPermission(ctx, media.SetID, userID, model.RoleOwner); err != nil {
		return nil, err
	}

	return media, nil
}

// verifySetModifyAccess checks that the user is an owner or admin for a set.
func (s *mediaService) verifySetModifyAccess(ctx context.Context, setID, userID int64) error {
	return s.checkSetPermission(ctx, setID, userID, model.RoleOwner)
}
