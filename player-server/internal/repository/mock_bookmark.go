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

// MockRadioRepo is a hand-written radio bookmark repository fake.
type MockRadioRepo struct {
	UpsertRadioBookmarkFunc func(context.Context, *model.RadioBookmark) (*model.RadioBookmark, error)
	ListRadioBookmarksFunc  func(context.Context, int64) ([]model.RadioBookmark, error)
	GetRadioBookmarkFunc    func(context.Context, int64, string) (*model.RadioBookmark, error)
	DeleteRadioBookmarkFunc func(context.Context, int64, string) error
}

func (m *MockRadioRepo) UpsertRadioBookmark(ctx context.Context, b *model.RadioBookmark) (*model.RadioBookmark, error) {
	if m.UpsertRadioBookmarkFunc != nil {
		return m.UpsertRadioBookmarkFunc(ctx, b)
	}
	return b, nil
}

func (m *MockRadioRepo) ListRadioBookmarks(ctx context.Context, userID int64) ([]model.RadioBookmark, error) {
	if m.ListRadioBookmarksFunc != nil {
		return m.ListRadioBookmarksFunc(ctx, userID)
	}
	return nil, nil
}

func (m *MockRadioRepo) GetRadioBookmark(ctx context.Context, userID int64, uuid string) (*model.RadioBookmark, error) {
	if m.GetRadioBookmarkFunc != nil {
		return m.GetRadioBookmarkFunc(ctx, userID, uuid)
	}
	return nil, nil
}

func (m *MockRadioRepo) DeleteRadioBookmark(ctx context.Context, userID int64, uuid string) error {
	if m.DeleteRadioBookmarkFunc != nil {
		return m.DeleteRadioBookmarkFunc(ctx, userID, uuid)
	}
	return nil
}

func (m *MockStore) UpsertRadioBookmark(ctx context.Context, b *model.RadioBookmark) (*model.RadioBookmark, error) {
	return m.RadioRepo.UpsertRadioBookmark(ctx, b)
}

func (m *MockStore) ListRadioBookmarks(ctx context.Context, userID int64) ([]model.RadioBookmark, error) {
	return m.RadioRepo.ListRadioBookmarks(ctx, userID)
}

func (m *MockStore) GetRadioBookmark(ctx context.Context, userID int64, uuid string) (*model.RadioBookmark, error) {
	return m.RadioRepo.GetRadioBookmark(ctx, userID, uuid)
}

func (m *MockStore) DeleteRadioBookmark(ctx context.Context, userID int64, uuid string) error {
	return m.RadioRepo.DeleteRadioBookmark(ctx, userID, uuid)
}
