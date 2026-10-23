package service

import (
	"context"
	"errors"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// userTrashService exposes recoverable items only to set owners and admins.
// The admin trash service remains unrestricted behind its admin middleware.
type userTrashService struct {
	store  repository.AccessHelperStore
	helper *accessHelper
}

// NewUserTrashService creates the permission-filtered trash view.
func NewUserTrashService(store repository.AccessHelperStore) *userTrashService {
	return &userTrashService{store: store, helper: NewAccessHelper(store)}
}

func (s *userTrashService) ListTrash(ctx context.Context, userID int64) ([]model.Media, error) {
	items, err := s.store.ListDeletedMedia(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]model.Media, 0, len(items))
	allowed := make(map[int64]bool)
	for _, item := range items {
		canRestore, checked := allowed[item.SetID]
		if !checked {
			err := s.helper.verifySetModifyAccess(ctx, item.SetID, userID)
			if err != nil && !errors.Is(err, ErrForbidden) {
				return nil, err
			}
			canRestore = err == nil
			allowed[item.SetID] = canRestore
		}
		if canRestore {
			result = append(result, item)
		}
	}
	return result, nil
}
