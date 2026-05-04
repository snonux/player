package service

import (
	"context"
	"errors"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// tagService handles tagging of media items.
type tagService struct {
	store  repository.TagServiceStore
	helper *accessHelper
}

// NewTagService creates a TagService.
func NewTagService(store repository.TagServiceStore, helper *accessHelper) MediaTagService {
	return &tagService{
		store:  store,
		helper: helper,
	}
}

func (s *tagService) AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error {
	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	tag, err := s.store.GetTagByName(ctx, tagName)
	if err != nil {
		return fmt.Errorf("get tag: %w", err)
	}
	if tag == nil {
		id, err := s.store.CreateTag(ctx, tagName)
		if err != nil {
			return fmt.Errorf("create tag: %w", err)
		}
		tag = &model.Tag{ID: id, Name: tagName}
	}
	return s.store.AssignTag(ctx, mediaID, tag.ID)
}

func (s *tagService) RemoveTag(ctx context.Context, mediaID, userID int64, tagName string) error {
	if _, err := s.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	tag, err := s.store.GetTagByName(ctx, tagName)
	if err != nil {
		return fmt.Errorf("get tag: %w", err)
	}
	if tag == nil {
		return errors.New("tag not found")
	}
	return s.store.RemoveTag(ctx, mediaID, tag.ID)
}
