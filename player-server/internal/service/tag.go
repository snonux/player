package service

import (
	"context"
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
func NewTagService(store repository.TagServiceStore, helper *accessHelper) *tagService {
	return &tagService{
		store:  store,
		helper: helper,
	}
}

func (s *tagService) ListTags(ctx context.Context, userID int64) ([]model.Tag, error) {
	tags, err := s.store.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	return tags, nil
}

func (s *tagService) AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error {
	// Tags are global state: every other user with access to this media
	// sees the change. Require owner-level access so a viewer cannot
	// mutate shared metadata. Personal annotations (favorites, notes)
	// stay on verifyAccess because they're per-user.
	if _, err := s.helper.verifyModifyAccess(ctx, mediaID, userID); err != nil {
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
	// Tags are global state: every other user with access to this media
	// sees the change. Require owner-level access so a viewer cannot
	// mutate shared metadata. Personal annotations (favorites, notes)
	// stay on verifyAccess because they're per-user.
	if _, err := s.helper.verifyModifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	tag, err := s.store.GetTagByName(ctx, tagName)
	if err != nil {
		return fmt.Errorf("get tag: %w", err)
	}
	if tag == nil {
		// Use the sentinel so handleError maps this to HTTP 404 instead of
		// falling through to the default 500 branch.
		return ErrNotFound
	}
	return s.store.RemoveTag(ctx, mediaID, tag.ID)
}
