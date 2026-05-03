package service

import (
	"context"

	"codeberg.org/snonux/player/internal/repository"
)

// favService handles toggling favorite status.
type favService struct {
	store  repository.FavoriteServiceStore
	helper *accessHelper
}

// NewFavService creates a FavService.
func NewFavService(store repository.FavoriteServiceStore, helper *accessHelper) MediaFavoriteService {
	return &favService{
		store:  store,
		helper: helper,
	}
}

func (s *favService) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return false, err
	}
	return s.store.ToggleFavorite(ctx, userID, mediaID)
}
