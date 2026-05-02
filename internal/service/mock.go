package service

import (
	"context"
	"errors"
	"io"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

var (
	_ MediaBrowseService    = (*MockMediaService)(nil)
	_ MediaWriteService     = (*MockMediaService)(nil)
	_ MediaShareService     = (*MockMediaService)(nil)
	_ MediaTagService       = (*MockMediaService)(nil)
	_ MediaFavoriteService  = (*MockMediaService)(nil)
	_ MediaNoteService      = (*MockMediaService)(nil)
	_ MediaService          = (*MockMediaService)(nil)
	_ AuthService           = (*MockAuthService)(nil)
)

// MockMediaService is a fake MediaService for testing.
type MockMediaService struct {
	ListSetsFunc            func(ctx context.Context, userID int64) ([]model.Set, error)
	GetMediaDetailFunc      func(ctx context.Context, mediaID, userID int64) (*MediaDetail, error)
	ListMediaFunc           func(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error)
	StreamMediaFunc         func(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	DownloadMediaFunc       func(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	GetThumbnailFunc        func(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	RegenerateThumbnailFunc func(ctx context.Context, mediaID, userID int64) error
	RegenerateSetCoverFunc  func(ctx context.Context, setID int64, folder string, userID int64) error
	BrowseSetFunc           func(ctx context.Context, setID, userID int64, parent string) (*BrowseResult, error)
	GetSetCoverFunc         func(ctx context.Context, setID int64, folder string, userID int64) (*FileResult, error)
	ToggleFavoriteFunc      func(ctx context.Context, userID, mediaID int64) (bool, error)
	AssignTagFunc           func(ctx context.Context, mediaID, userID int64, tagName string) error
	RemoveTagFunc           func(ctx context.Context, mediaID, userID int64, tagName string) error
	SoftDeleteMediaFunc     func(ctx context.Context, mediaID, userID int64) error
	RestoreMediaFunc        func(ctx context.Context, mediaID, userID int64) error
	UploadMediaFunc         func(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error)
	CreateShareFunc         func(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error)
	ListSharesFunc          func(ctx context.Context, mediaID, userID int64) ([]model.Share, error)
	RevokeShareFunc         func(ctx context.Context, token string, userID int64) error
	ValidateShareTokenFunc  func(ctx context.Context, token string) (*model.Share, error)
	StreamSharedMediaFunc   func(ctx context.Context, token string) (*FileResult, error)
	GetSharedMediaFunc      func(ctx context.Context, token string) (*GetSharedMediaResult, error)
	GetNoteFunc             func(ctx context.Context, mediaID, userID int64) (*model.Note, error)
	UpsertNoteFunc          func(ctx context.Context, note *model.Note) error
	DeleteNoteFunc          func(ctx context.Context, mediaID, userID int64) error
}

func (m *MockMediaService) ListSets(ctx context.Context, userID int64) ([]model.Set, error) {
	if m.ListSetsFunc != nil {
		return m.ListSetsFunc(ctx, userID)
	}
	return nil, nil
}
func (m *MockMediaService) GetMediaDetail(ctx context.Context, mediaID, userID int64) (*MediaDetail, error) {
	if m.GetMediaDetailFunc != nil {
		return m.GetMediaDetailFunc(ctx, mediaID, userID)
	}
	return nil, nil
}
func (m *MockMediaService) ListMedia(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error) {
	if m.ListMediaFunc != nil {
		return m.ListMediaFunc(ctx, userID, filter)
	}
	return nil, nil
}
func (m *MockMediaService) StreamMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	if m.StreamMediaFunc != nil {
		return m.StreamMediaFunc(ctx, mediaID, userID)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) DownloadMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	if m.DownloadMediaFunc != nil {
		return m.DownloadMediaFunc(ctx, mediaID, userID)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) GetThumbnail(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	if m.GetThumbnailFunc != nil {
		return m.GetThumbnailFunc(ctx, mediaID, userID)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) RegenerateThumbnail(ctx context.Context, mediaID, userID int64) error {
	if m.RegenerateThumbnailFunc != nil {
		return m.RegenerateThumbnailFunc(ctx, mediaID, userID)
	}
	return nil
}
func (m *MockMediaService) RegenerateSetCover(ctx context.Context, setID int64, folder string, userID int64) error {
	if m.RegenerateSetCoverFunc != nil {
		return m.RegenerateSetCoverFunc(ctx, setID, folder, userID)
	}
	return nil
}
func (m *MockMediaService) BrowseSet(ctx context.Context, setID, userID int64, parent string) (*BrowseResult, error) {
	if m.BrowseSetFunc != nil {
		return m.BrowseSetFunc(ctx, setID, userID, parent)
	}
	return nil, nil
}
func (m *MockMediaService) GetSetCover(ctx context.Context, setID int64, folder string, userID int64) (*FileResult, error) {
	if m.GetSetCoverFunc != nil {
		return m.GetSetCoverFunc(ctx, setID, folder, userID)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	if m.ToggleFavoriteFunc != nil {
		return m.ToggleFavoriteFunc(ctx, userID, mediaID)
	}
	return false, nil
}
func (m *MockMediaService) AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error {
	if m.AssignTagFunc != nil {
		return m.AssignTagFunc(ctx, mediaID, userID, tagName)
	}
	return nil
}
func (m *MockMediaService) RemoveTag(ctx context.Context, mediaID, userID int64, tagName string) error {
	if m.RemoveTagFunc != nil {
		return m.RemoveTagFunc(ctx, mediaID, userID, tagName)
	}
	return nil
}
func (m *MockMediaService) SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error {
	if m.SoftDeleteMediaFunc != nil {
		return m.SoftDeleteMediaFunc(ctx, mediaID, userID)
	}
	return nil
}
func (m *MockMediaService) RestoreMedia(ctx context.Context, mediaID, userID int64) error {
	if m.RestoreMediaFunc != nil {
		return m.RestoreMediaFunc(ctx, mediaID, userID)
	}
	return nil
}
func (m *MockMediaService) UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error) {
	if m.UploadMediaFunc != nil {
		return m.UploadMediaFunc(ctx, setID, userID, filename, data, size)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) CreateShare(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error) {
	if m.CreateShareFunc != nil {
		return m.CreateShareFunc(ctx, userID, mediaID, expiresAt)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) ListShares(ctx context.Context, mediaID, userID int64) ([]model.Share, error) {
	if m.ListSharesFunc != nil {
		return m.ListSharesFunc(ctx, mediaID, userID)
	}
	return nil, nil
}
func (m *MockMediaService) RevokeShare(ctx context.Context, token string, userID int64) error {
	if m.RevokeShareFunc != nil {
		return m.RevokeShareFunc(ctx, token, userID)
	}
	return nil
}
func (m *MockMediaService) ValidateShareToken(ctx context.Context, token string) (*model.Share, error) {
	if m.ValidateShareTokenFunc != nil {
		return m.ValidateShareTokenFunc(ctx, token)
	}
	return nil, nil
}
func (m *MockMediaService) StreamSharedMedia(ctx context.Context, token string) (*FileResult, error) {
	if m.StreamSharedMediaFunc != nil {
		return m.StreamSharedMediaFunc(ctx, token)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) GetSharedMedia(ctx context.Context, token string) (*GetSharedMediaResult, error) {
	if m.GetSharedMediaFunc != nil {
		return m.GetSharedMediaFunc(ctx, token)
	}
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) GetSharedThumbnail(ctx context.Context, token string) (*FileResult, error) {
	return nil, errors.New("not implemented")
}
func (m *MockMediaService) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	if m.GetNoteFunc != nil {
		return m.GetNoteFunc(ctx, mediaID, userID)
	}
	return nil, nil
}
func (m *MockMediaService) UpsertNote(ctx context.Context, note *model.Note) error {
	if m.UpsertNoteFunc != nil {
		return m.UpsertNoteFunc(ctx, note)
	}
	return nil
}
func (m *MockMediaService) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	if m.DeleteNoteFunc != nil {
		return m.DeleteNoteFunc(ctx, mediaID, userID)
	}
	return nil
}

// MockAdminService is a fake AdminService for testing.
type MockAdminService struct {
	ListTrashFunc        func(ctx context.Context) ([]model.Media, error)
	TriggerRescanFunc    func(ctx context.Context) error
	ListUsersFunc        func(ctx context.Context) ([]model.User, error)
	CreateUserFunc       func(ctx context.Context, username, password string, isAdmin bool) (*model.User, error)
	DeleteUserFunc       func(ctx context.Context, id int64) error
	ListPermissionsFunc  func(ctx context.Context) (*PermissionsMatrix, error)
	GrantPermissionFunc  func(ctx context.Context, setID, userID int64, role model.Role) error
	RevokePermissionFunc func(ctx context.Context, setID, userID int64) error
}

func (m *MockAdminService) ListTrash(ctx context.Context) ([]model.Media, error) {
	if m.ListTrashFunc != nil {
		return m.ListTrashFunc(ctx)
	}
	return nil, nil
}
func (m *MockAdminService) TriggerRescan(ctx context.Context) error {
	if m.TriggerRescanFunc != nil {
		return m.TriggerRescanFunc(ctx)
	}
	return nil
}
func (m *MockAdminService) ListUsers(ctx context.Context) ([]model.User, error) {
	if m.ListUsersFunc != nil {
		return m.ListUsersFunc(ctx)
	}
	return nil, nil
}
func (m *MockAdminService) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*model.User, error) {
	if m.CreateUserFunc != nil {
		return m.CreateUserFunc(ctx, username, password, isAdmin)
	}
	return nil, errors.New("not implemented")
}
func (m *MockAdminService) DeleteUser(ctx context.Context, id int64) error {
	if m.DeleteUserFunc != nil {
		return m.DeleteUserFunc(ctx, id)
	}
	return nil
}
func (m *MockAdminService) ListPermissions(ctx context.Context) (*PermissionsMatrix, error) {
	if m.ListPermissionsFunc != nil {
		return m.ListPermissionsFunc(ctx)
	}
	return nil, nil
}
func (m *MockAdminService) GrantPermission(ctx context.Context, setID, userID int64, role model.Role) error {
	if m.GrantPermissionFunc != nil {
		return m.GrantPermissionFunc(ctx, setID, userID, role)
	}
	return nil
}
func (m *MockAdminService) RevokePermission(ctx context.Context, setID, userID int64) error {
	if m.RevokePermissionFunc != nil {
		return m.RevokePermissionFunc(ctx, setID, userID)
	}
	return nil
}

// MockAuthService is a fake AuthService for testing.
type MockAuthService struct {
	BootstrapFunc func(ctx context.Context, username, password string) (*AuthResult, error)
	LoginFunc     func(ctx context.Context, username, password string) (*AuthResult, error)
}

func (m *MockAuthService) Bootstrap(ctx context.Context, username, password string) (*AuthResult, error) {
	if m.BootstrapFunc != nil {
		return m.BootstrapFunc(ctx, username, password)
	}
	return nil, nil
}
func (m *MockAuthService) Login(ctx context.Context, username, password string) (*AuthResult, error) {
	if m.LoginFunc != nil {
		return m.LoginFunc(ctx, username, password)
	}
	return nil, nil
}

// MockProgressService is a fake ProgressService for testing.
type MockProgressService struct {
	UpdateProgressFunc func(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error
}

func (m *MockProgressService) UpdateProgress(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error {
	if m.UpdateProgressFunc != nil {
		return m.UpdateProgressFunc(ctx, sessionID, userID, mediaID, position)
	}
	return nil
}
