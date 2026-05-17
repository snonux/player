package service

import (
	"context"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// noteService handles CRUD for per-user per-media notes.
type noteService struct {
	store  repository.NoteServiceStore
	clock  clock.Clock
	helper *accessHelper
}

// NewNoteService creates a NoteService.
func NewNoteService(store repository.NoteServiceStore, clk clock.Clock, helper *accessHelper) *noteService {
	return &noteService{
		store:  store,
		clock:  clk,
		helper: helper,
	}
}

func (s *noteService) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return nil, err
	}
	return s.store.GetNote(ctx, mediaID, userID)
}

func (s *noteService) UpsertNote(ctx context.Context, note *model.Note) error {
	if _, err := s.helper.verifyAccess(ctx, note.MediaID, note.UserID); err != nil {
		return err
	}
	note.UpdatedAt = s.clock.Now()
	return s.store.UpsertNote(ctx, note)
}

func (s *noteService) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	return s.store.DeleteNote(ctx, mediaID, userID)
}
