package service

import (
	"context"

	"codeberg.org/snonux/player/internal/model"
)

func (s *mediaService) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	if _, err := s.verifyAccess(ctx, mediaID, userID); err != nil {
		return nil, err
	}
	return s.store.GetNote(ctx, mediaID, userID)
}

func (s *mediaService) UpsertNote(ctx context.Context, note *model.Note) error {
	if _, err := s.verifyAccess(ctx, note.MediaID, note.UserID); err != nil {
		return err
	}
	note.UpdatedAt = s.clock.Now()
	return s.store.UpsertNote(ctx, note)
}

func (s *mediaService) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	if _, err := s.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	return s.store.DeleteNote(ctx, mediaID, userID)
}
