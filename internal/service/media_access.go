package service

import (
	"context"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

func (s *mediaService) verifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := s.store.GetMediaByID(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil || media.DeletedAt != nil {
		return nil, ErrNotFound
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return media, nil
	}

	set, err := s.store.GetSetByID(ctx, media.SetID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	for _, p := range set.Permissions {
		if p.UserID == userID {
			return media, nil
		}
	}

	perm, err := s.store.GetPermission(ctx, media.SetID, userID)
	if err != nil {
		return nil, fmt.Errorf("get permission: %w", err)
	}
	if perm != nil {
		return media, nil
	}

	return nil, ErrForbidden
}

// verifyModifyAccess checks that the user has access to the media and is an owner or admin.
func (s *mediaService) verifyModifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := s.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return media, nil
	}

	perm, err := s.store.GetPermission(ctx, media.SetID, userID)
	if err != nil {
		return nil, fmt.Errorf("get permission: %w", err)
	}
	if perm != nil && perm.Role == model.RoleOwner {
		return media, nil
	}

	set, err := s.store.GetSetByID(ctx, media.SetID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set != nil {
		for _, p := range set.Permissions {
			if p.UserID == userID && p.Role == model.RoleOwner {
				return media, nil
			}
		}
	}

	return nil, ErrForbidden
}

// verifySetModifyAccess checks that the user is an owner or admin for a set.
func (s *mediaService) verifySetModifyAccess(ctx context.Context, setID, userID int64) error {
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
	if perm != nil && perm.Role == model.RoleOwner {
		return nil
	}

	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}
	if set != nil {
		for _, p := range set.Permissions {
			if p.UserID == userID && p.Role == model.RoleOwner {
				return nil
			}
		}
	}

	return ErrForbidden
}
