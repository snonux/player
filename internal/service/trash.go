package service

import (
	"context"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// trashService handles listing soft-deleted media.
type trashService struct {
	store repository.TrashServiceStore
}

// NewTrashService creates a TrashService.
func NewTrashService(store repository.TrashServiceStore) *trashService {
	return &trashService{store: store}
}

func (s *trashService) ListTrash(ctx context.Context) ([]model.Media, error) {
	return s.store.ListDeletedMedia(ctx)
}
