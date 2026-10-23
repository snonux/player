package service

import (
	"context"

	"github.com/snonux/player/internal/model"
)

func (m *MockMediaService) ListBookmarks(ctx context.Context, mediaID, userID int64) ([]model.Bookmark, error) {
	if m.ListBookmarksFunc != nil {
		return m.ListBookmarksFunc(ctx, mediaID, userID)
	}
	return nil, nil
}

func (m *MockMediaService) CreateBookmark(ctx context.Context, mediaID, userID int64, position float64, label string) (*model.Bookmark, error) {
	if m.CreateBookmarkFunc != nil {
		return m.CreateBookmarkFunc(ctx, mediaID, userID, position, label)
	}
	return nil, nil
}

func (m *MockMediaService) DeleteBookmark(ctx context.Context, id, mediaID, userID int64) error {
	if m.DeleteBookmarkFunc != nil {
		return m.DeleteBookmarkFunc(ctx, id, mediaID, userID)
	}
	return nil
}
