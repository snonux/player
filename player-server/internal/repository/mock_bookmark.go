package repository

import (
	"context"

	"github.com/snonux/player/internal/model"
)

// MockBookmarkRepo is a hand-written bookmark repository fake.
type MockBookmarkRepo struct {
	CreateBookmarkFunc func(context.Context, *model.Bookmark) (int64, error)
	ListBookmarksFunc  func(context.Context, int64, int64) ([]model.Bookmark, error)
	DeleteBookmarkFunc func(context.Context, int64, int64, int64) (bool, error)
}

func (m *MockBookmarkRepo) CreateBookmark(ctx context.Context, b *model.Bookmark) (int64, error) {
	if m.CreateBookmarkFunc != nil {
		return m.CreateBookmarkFunc(ctx, b)
	}
	return 0, nil
}

func (m *MockBookmarkRepo) ListBookmarks(ctx context.Context, mediaID, userID int64) ([]model.Bookmark, error) {
	if m.ListBookmarksFunc != nil {
		return m.ListBookmarksFunc(ctx, mediaID, userID)
	}
	return nil, nil
}

func (m *MockBookmarkRepo) DeleteBookmark(ctx context.Context, id, mediaID, userID int64) (bool, error) {
	if m.DeleteBookmarkFunc != nil {
		return m.DeleteBookmarkFunc(ctx, id, mediaID, userID)
	}
	return false, nil
}

func (m *MockStore) CreateBookmark(ctx context.Context, b *model.Bookmark) (int64, error) {
	return m.BookmarkRepo.CreateBookmark(ctx, b)
}

func (m *MockStore) ListBookmarks(ctx context.Context, mediaID, userID int64) ([]model.Bookmark, error) {
	return m.BookmarkRepo.ListBookmarks(ctx, mediaID, userID)
}

func (m *MockStore) DeleteBookmark(ctx context.Context, id, mediaID, userID int64) (bool, error) {
	return m.BookmarkRepo.DeleteBookmark(ctx, id, mediaID, userID)
}
