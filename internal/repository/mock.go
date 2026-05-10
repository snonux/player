// Package repository provides data access abstractions.
package repository

import (
	"context"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// compile-time checks.
var (
	_ Store                       = (*MockStore)(nil)
	_ MediaServiceStore           = (*MockStore)(nil)
	_ AdminServiceStore           = (*MockStore)(nil)
	_ AuthServiceStore            = (*MockStore)(nil)
	_ ProgressServiceStore        = (*MockStore)(nil)
	_ GCStore                     = (*MockStore)(nil)
	_ ScannerStore                = (*MockStore)(nil)
	_ AccessHelperStore           = (*MockStore)(nil)
	_ BrowseServiceStore          = (*MockStore)(nil)
	_ WriteServiceStore           = (*MockStore)(nil)
	_ ShareServiceStore           = (*MockStore)(nil)
	_ TagServiceStore             = (*MockStore)(nil)
	_ FavoriteServiceStore        = (*MockStore)(nil)
	_ NoteServiceStore            = (*MockStore)(nil)
	_ TrashServiceStore           = (*MockStore)(nil)
	_ UserAdminServiceStore       = (*MockStore)(nil)
	_ PermissionAdminServiceStore = (*MockStore)(nil)
	_ PodcastRepo                 = (*MockStore)(nil)
)

// NewMockStore returns a MockStore with all no-op defaults.
func NewMockStore() *MockStore {
	return &MockStore{}
}

// MockStore is a hand-written fake for all repository interfaces.
// Each embedded struct provides default no-op / zero-value behavior;
// callers override individual func fields to inject test behavior.
type MockStore struct {
	UserRepo                MockUserRepo
	SetRepo                 MockSetRepo
	SetPermissionRepo       MockSetPermissionRepo
	MediaRepo               MockMediaRepo
	TagRepo                 MockTagRepo
	FavoriteRepo            MockFavoriteRepo
	PlaybackProgressRepo    MockPlaybackProgressRepo
	PlaybackAccumulatorRepo MockPlaybackAccumulatorRepo
	SessionRepo             MockSessionRepo
	ShareRepo               MockShareRepo
	NoteRepo                MockNoteRepo
	PodcastRepo             MockPodcastRepo
}

// CreateUser implements UserRepo.
func (m *MockStore) CreateUser(ctx context.Context, user *model.User) (int64, error) {
	return m.UserRepo.CreateUser(ctx, user)
}

// GetUserByID implements UserRepo.
func (m *MockStore) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return m.UserRepo.GetUserByID(ctx, id)
}

// GetUserByUsername implements UserRepo.
func (m *MockStore) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	return m.UserRepo.GetUserByUsername(ctx, username)
}

// ListUsers implements UserRepo.
func (m *MockStore) ListUsers(ctx context.Context) ([]model.User, error) {
	return m.UserRepo.ListUsers(ctx)
}

// DeleteUser implements UserRepo.
func (m *MockStore) DeleteUser(ctx context.Context, id int64) error {
	return m.UserRepo.DeleteUser(ctx, id)
}

// CountUsers implements UserRepo.
func (m *MockStore) CountUsers(ctx context.Context) (int, error) { return m.UserRepo.CountUsers(ctx) }

// CreateSet implements SetRepo.
func (m *MockStore) CreateSet(ctx context.Context, set *model.Set) (int64, error) {
	return m.SetRepo.CreateSet(ctx, set)
}

// GetSetByID implements SetRepo.
func (m *MockStore) GetSetByID(ctx context.Context, id int64) (*model.Set, error) {
	return m.SetRepo.GetSetByID(ctx, id)
}

// ListSets implements SetRepo.
func (m *MockStore) ListSets(ctx context.Context) ([]model.Set, error) {
	return m.SetRepo.ListSets(ctx)
}

// UpdateSet implements SetRepo.
func (m *MockStore) UpdateSet(ctx context.Context, set *model.Set) error {
	return m.SetRepo.UpdateSet(ctx, set)
}

// DeleteSet implements SetRepo.
func (m *MockStore) DeleteSet(ctx context.Context, id int64) error {
	return m.SetRepo.DeleteSet(ctx, id)
}

// GrantPermission implements SetPermissionRepo.
func (m *MockStore) GrantPermission(ctx context.Context, perm *model.SetPermission) error {
	return m.SetPermissionRepo.GrantPermission(ctx, perm)
}

// RevokePermission implements SetPermissionRepo.
func (m *MockStore) RevokePermission(ctx context.Context, setID, userID int64) error {
	return m.SetPermissionRepo.RevokePermission(ctx, setID, userID)
}

// GetPermission implements SetPermissionRepo.
func (m *MockStore) GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
	return m.SetPermissionRepo.GetPermission(ctx, setID, userID)
}

// ListPermissionsBySet implements SetPermissionRepo.
func (m *MockStore) ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error) {
	return m.SetPermissionRepo.ListPermissionsBySet(ctx, setID)
}

// ListPermissionsByUser implements SetPermissionRepo.
func (m *MockStore) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error) {
	return m.SetPermissionRepo.ListPermissionsByUser(ctx, userID)
}

// CreateMedia implements MediaRepo.
func (m *MockStore) CreateMedia(ctx context.Context, media *model.Media) (int64, error) {
	return m.MediaRepo.CreateMedia(ctx, media)
}

// GetMediaByID implements MediaRepo.
func (m *MockStore) GetMediaByID(ctx context.Context, id int64) (*model.Media, error) {
	return m.MediaRepo.GetMediaByID(ctx, id)
}

// UpdateMedia implements MediaRepo.
func (m *MockStore) UpdateMedia(ctx context.Context, media *model.Media) error {
	return m.MediaRepo.UpdateMedia(ctx, media)
}

// UpdateMediaThumbnail implements MediaRepo.
func (m *MockStore) UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error {
	return m.MediaRepo.UpdateMediaThumbnail(ctx, id, thumbnailPath)
}

// SoftDeleteMedia implements MediaRepo.
func (m *MockStore) SoftDeleteMedia(ctx context.Context, id int64) error {
	return m.MediaRepo.SoftDeleteMedia(ctx, id)
}

// RestoreMedia implements MediaRepo.
func (m *MockStore) RestoreMedia(ctx context.Context, id int64) error {
	return m.MediaRepo.RestoreMedia(ctx, id)
}

// HardDeleteMedia implements MediaRepo.
func (m *MockStore) HardDeleteMedia(ctx context.Context, id int64) error {
	return m.MediaRepo.HardDeleteMedia(ctx, id)
}

// ListMedia implements MediaRepo.
func (m *MockStore) ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error) {
	return m.MediaRepo.ListMedia(ctx, filter)
}

// ListDeletedMedia implements MediaRepo.
func (m *MockStore) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	return m.MediaRepo.ListDeletedMedia(ctx)
}

// IncrementPlayCount implements MediaRepo.
func (m *MockStore) IncrementPlayCount(ctx context.Context, id int64) error {
	return m.MediaRepo.IncrementPlayCount(ctx, id)
}

// CreateTag implements TagRepo.
func (m *MockStore) CreateTag(ctx context.Context, name string) (int64, error) {
	return m.TagRepo.CreateTag(ctx, name)
}

// GetTagByID implements TagRepo.
func (m *MockStore) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	return m.TagRepo.GetTagByID(ctx, id)
}

// GetTagByName implements TagRepo.
func (m *MockStore) GetTagByName(ctx context.Context, name string) (*model.Tag, error) {
	return m.TagRepo.GetTagByName(ctx, name)
}

// ListTags implements TagRepo.
func (m *MockStore) ListTags(ctx context.Context) ([]model.Tag, error) {
	return m.TagRepo.ListTags(ctx)
}

// DeleteTag implements TagRepo.
func (m *MockStore) DeleteTag(ctx context.Context, id int64) error {
	return m.TagRepo.DeleteTag(ctx, id)
}

// AssignTag implements TagRepo.
func (m *MockStore) AssignTag(ctx context.Context, mediaID, tagID int64) error {
	return m.TagRepo.AssignTag(ctx, mediaID, tagID)
}

// RemoveTag implements TagRepo.
func (m *MockStore) RemoveTag(ctx context.Context, mediaID, tagID int64) error {
	return m.TagRepo.RemoveTag(ctx, mediaID, tagID)
}

// ListTagsByMedia implements TagRepo.
func (m *MockStore) ListTagsByMedia(ctx context.Context, mediaID int64) ([]model.Tag, error) {
	return m.TagRepo.ListTagsByMedia(ctx, mediaID)
}

// ToggleFavorite implements FavoriteRepo.
func (m *MockStore) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	return m.FavoriteRepo.ToggleFavorite(ctx, userID, mediaID)
}

// IsFavorite implements FavoriteRepo.
func (m *MockStore) IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	return m.FavoriteRepo.IsFavorite(ctx, userID, mediaID)
}

// ListFavoritesByUser implements FavoriteRepo.
func (m *MockStore) ListFavoritesByUser(ctx context.Context, userID int64) ([]model.Favorite, error) {
	return m.FavoriteRepo.ListFavoritesByUser(ctx, userID)
}

// UpsertProgress implements PlaybackProgressRepo.
func (m *MockStore) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	return m.PlaybackProgressRepo.UpsertProgress(ctx, progress)
}

// GetProgress implements PlaybackProgressRepo.
func (m *MockStore) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	return m.PlaybackProgressRepo.GetProgress(ctx, userID, mediaID)
}

// ListProgressByUser implements PlaybackProgressRepo.
func (m *MockStore) ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error) {
	return m.PlaybackProgressRepo.ListProgressByUser(ctx, userID)
}

// UpsertAccumulator implements PlaybackAccumulatorRepo.
func (m *MockStore) UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error {
	return m.PlaybackAccumulatorRepo.UpsertAccumulator(ctx, acc)
}

// GetAccumulator implements PlaybackAccumulatorRepo.
func (m *MockStore) GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
	return m.PlaybackAccumulatorRepo.GetAccumulator(ctx, sessionID, mediaID)
}

// CreateSession implements SessionRepo.
func (m *MockStore) CreateSession(ctx context.Context, session *model.Session) error {
	return m.SessionRepo.CreateSession(ctx, session)
}

// GetSessionByID implements SessionRepo.
func (m *MockStore) GetSessionByID(ctx context.Context, id string) (*model.Session, error) {
	return m.SessionRepo.GetSessionByID(ctx, id)
}

// DeleteSession implements SessionRepo.
func (m *MockStore) DeleteSession(ctx context.Context, id string) error {
	return m.SessionRepo.DeleteSession(ctx, id)
}

// DeleteExpiredSessions implements SessionRepo.
func (m *MockStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return m.SessionRepo.DeleteExpiredSessions(ctx, now)
}

// CreateShare implements ShareRepo.
func (m *MockStore) CreateShare(ctx context.Context, share *model.Share) error {
	return m.ShareRepo.CreateShare(ctx, share)
}

// GetShareByToken implements ShareRepo.
func (m *MockStore) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	return m.ShareRepo.GetShareByToken(ctx, token)
}

// ListSharesByMedia implements ShareRepo.
func (m *MockStore) ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error) {
	return m.ShareRepo.ListSharesByMedia(ctx, mediaID)
}

// ListSharesByUser implements ShareRepo.
func (m *MockStore) ListSharesByUser(ctx context.Context, userID int64) ([]model.Share, error) {
	return m.ShareRepo.ListSharesByUser(ctx, userID)
}

// UseShare implements ShareRepo.
func (m *MockStore) UseShare(ctx context.Context, token string) error {
	return m.ShareRepo.UseShare(ctx, token)
}

// DeleteShare implements ShareRepo.
func (m *MockStore) DeleteShare(ctx context.Context, token string) error {
	return m.ShareRepo.DeleteShare(ctx, token)
}

// DeleteExpiredShares implements ShareRepo.
func (m *MockStore) DeleteExpiredShares(ctx context.Context, now time.Time) error {
	return m.ShareRepo.DeleteExpiredShares(ctx, now)
}

// UpsertNote implements NoteRepo.
func (m *MockStore) UpsertNote(ctx context.Context, note *model.Note) error {
	return m.NoteRepo.UpsertNote(ctx, note)
}

// GetNote implements NoteRepo.
func (m *MockStore) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	return m.NoteRepo.GetNote(ctx, mediaID, userID)
}

// DeleteNote implements NoteRepo.
func (m *MockStore) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	return m.NoteRepo.DeleteNote(ctx, mediaID, userID)
}

// MockUserRepo is a fake UserRepo.
type MockUserRepo struct {
	CreateUserFunc        func(ctx context.Context, user *model.User) (int64, error)
	GetUserByIDFunc       func(ctx context.Context, id int64) (*model.User, error)
	GetUserByUsernameFunc func(ctx context.Context, username string) (*model.User, error)
	ListUsersFunc         func(ctx context.Context) ([]model.User, error)
	DeleteUserFunc        func(ctx context.Context, id int64) error
	CountUsersFunc        func(ctx context.Context) (int, error)
}

// CreateUser calls CreateUserFunc or returns a default ID.
func (m *MockUserRepo) CreateUser(ctx context.Context, user *model.User) (int64, error) {
	if m.CreateUserFunc != nil {
		return m.CreateUserFunc(ctx, user)
	}
	return 1, nil
}

// GetUserByID calls GetUserByIDFunc or returns nil.
func (m *MockUserRepo) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	if m.GetUserByIDFunc != nil {
		return m.GetUserByIDFunc(ctx, id)
	}
	return nil, nil
}

// GetUserByUsername calls GetUserByUsernameFunc or returns nil.
func (m *MockUserRepo) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	if m.GetUserByUsernameFunc != nil {
		return m.GetUserByUsernameFunc(ctx, username)
	}
	return nil, nil
}

// ListUsers calls ListUsersFunc or returns nil.
func (m *MockUserRepo) ListUsers(ctx context.Context) ([]model.User, error) {
	if m.ListUsersFunc != nil {
		return m.ListUsersFunc(ctx)
	}
	return nil, nil
}

// DeleteUser calls DeleteUserFunc or returns nil.
func (m *MockUserRepo) DeleteUser(ctx context.Context, id int64) error {
	if m.DeleteUserFunc != nil {
		return m.DeleteUserFunc(ctx, id)
	}
	return nil
}

// CountUsers calls CountUsersFunc or returns zero.
func (m *MockUserRepo) CountUsers(ctx context.Context) (int, error) {
	if m.CountUsersFunc != nil {
		return m.CountUsersFunc(ctx)
	}
	return 0, nil
}

// MockSetRepo is a fake SetRepo.
type MockSetRepo struct {
	CreateSetFunc  func(ctx context.Context, set *model.Set) (int64, error)
	GetSetByIDFunc func(ctx context.Context, id int64) (*model.Set, error)
	ListSetsFunc   func(ctx context.Context) ([]model.Set, error)
	UpdateSetFunc  func(ctx context.Context, set *model.Set) error
	DeleteSetFunc  func(ctx context.Context, id int64) error
}

// CreateSet calls CreateSetFunc or returns a default ID.
func (m *MockSetRepo) CreateSet(ctx context.Context, set *model.Set) (int64, error) {
	if m.CreateSetFunc != nil {
		return m.CreateSetFunc(ctx, set)
	}
	return 1, nil
}

// GetSetByID calls GetSetByIDFunc or returns nil.
func (m *MockSetRepo) GetSetByID(ctx context.Context, id int64) (*model.Set, error) {
	if m.GetSetByIDFunc != nil {
		return m.GetSetByIDFunc(ctx, id)
	}
	return nil, nil
}

// ListSets calls ListSetsFunc or returns nil.
func (m *MockSetRepo) ListSets(ctx context.Context) ([]model.Set, error) {
	if m.ListSetsFunc != nil {
		return m.ListSetsFunc(ctx)
	}
	return nil, nil
}

// UpdateSet calls UpdateSetFunc or returns nil.
func (m *MockSetRepo) UpdateSet(ctx context.Context, set *model.Set) error {
	if m.UpdateSetFunc != nil {
		return m.UpdateSetFunc(ctx, set)
	}
	return nil
}

// DeleteSet calls DeleteSetFunc or returns nil.
func (m *MockSetRepo) DeleteSet(ctx context.Context, id int64) error {
	if m.DeleteSetFunc != nil {
		return m.DeleteSetFunc(ctx, id)
	}
	return nil
}

// MockSetPermissionRepo is a fake SetPermissionRepo.
type MockSetPermissionRepo struct {
	GrantPermissionFunc       func(ctx context.Context, perm *model.SetPermission) error
	RevokePermissionFunc      func(ctx context.Context, setID, userID int64) error
	GetPermissionFunc         func(ctx context.Context, setID, userID int64) (*model.SetPermission, error)
	ListPermissionsBySetFunc  func(ctx context.Context, setID int64) ([]model.SetPermission, error)
	ListPermissionsByUserFunc func(ctx context.Context, userID int64) ([]model.SetPermission, error)
}

// GrantPermission calls GrantPermissionFunc or returns nil.
func (m *MockSetPermissionRepo) GrantPermission(ctx context.Context, perm *model.SetPermission) error {
	if m.GrantPermissionFunc != nil {
		return m.GrantPermissionFunc(ctx, perm)
	}
	return nil
}

// RevokePermission calls RevokePermissionFunc or returns nil.
func (m *MockSetPermissionRepo) RevokePermission(ctx context.Context, setID, userID int64) error {
	if m.RevokePermissionFunc != nil {
		return m.RevokePermissionFunc(ctx, setID, userID)
	}
	return nil
}

// GetPermission calls GetPermissionFunc or returns nil.
func (m *MockSetPermissionRepo) GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
	if m.GetPermissionFunc != nil {
		return m.GetPermissionFunc(ctx, setID, userID)
	}
	return nil, nil
}

// ListPermissionsBySet calls ListPermissionsBySetFunc or returns nil.
func (m *MockSetPermissionRepo) ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error) {
	if m.ListPermissionsBySetFunc != nil {
		return m.ListPermissionsBySetFunc(ctx, setID)
	}
	return nil, nil
}

// ListPermissionsByUser calls ListPermissionsByUserFunc or returns nil.
func (m *MockSetPermissionRepo) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error) {
	if m.ListPermissionsByUserFunc != nil {
		return m.ListPermissionsByUserFunc(ctx, userID)
	}
	return nil, nil
}

// MockMediaRepo is a fake MediaRepo.
type MockMediaRepo struct {
	CreateMediaFunc          func(ctx context.Context, media *model.Media) (int64, error)
	GetMediaByIDFunc         func(ctx context.Context, id int64) (*model.Media, error)
	UpdateMediaFunc          func(ctx context.Context, media *model.Media) error
	UpdateMediaThumbnailFunc func(ctx context.Context, id int64, thumbnailPath string) error
	SoftDeleteMediaFunc      func(ctx context.Context, id int64) error
	RestoreMediaFunc         func(ctx context.Context, id int64) error
	HardDeleteMediaFunc      func(ctx context.Context, id int64) error
	ListMediaFunc            func(ctx context.Context, filter MediaFilter) ([]model.Media, error)
	ListDeletedMediaFunc     func(ctx context.Context) ([]model.Media, error)
	IncrementPlayCountFunc   func(ctx context.Context, id int64) error
}

// CreateMedia calls CreateMediaFunc or returns a default ID.
func (m *MockMediaRepo) CreateMedia(ctx context.Context, media *model.Media) (int64, error) {
	if m.CreateMediaFunc != nil {
		return m.CreateMediaFunc(ctx, media)
	}
	return 1, nil
}

// GetMediaByID calls GetMediaByIDFunc or returns nil.
func (m *MockMediaRepo) GetMediaByID(ctx context.Context, id int64) (*model.Media, error) {
	if m.GetMediaByIDFunc != nil {
		return m.GetMediaByIDFunc(ctx, id)
	}
	return nil, nil
}

// UpdateMedia calls UpdateMediaFunc or returns nil.
func (m *MockMediaRepo) UpdateMedia(ctx context.Context, media *model.Media) error {
	if m.UpdateMediaFunc != nil {
		return m.UpdateMediaFunc(ctx, media)
	}
	return nil
}

// UpdateMediaThumbnail calls UpdateMediaThumbnailFunc or returns nil.
func (m *MockMediaRepo) UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error {
	if m.UpdateMediaThumbnailFunc != nil {
		return m.UpdateMediaThumbnailFunc(ctx, id, thumbnailPath)
	}
	return nil
}

// SoftDeleteMedia calls SoftDeleteMediaFunc or returns nil.
func (m *MockMediaRepo) SoftDeleteMedia(ctx context.Context, id int64) error {
	if m.SoftDeleteMediaFunc != nil {
		return m.SoftDeleteMediaFunc(ctx, id)
	}
	return nil
}

// RestoreMedia calls RestoreMediaFunc or returns nil.
func (m *MockMediaRepo) RestoreMedia(ctx context.Context, id int64) error {
	if m.RestoreMediaFunc != nil {
		return m.RestoreMediaFunc(ctx, id)
	}
	return nil
}

// HardDeleteMedia calls HardDeleteMediaFunc or returns nil.
func (m *MockMediaRepo) HardDeleteMedia(ctx context.Context, id int64) error {
	if m.HardDeleteMediaFunc != nil {
		return m.HardDeleteMediaFunc(ctx, id)
	}
	return nil
}

// ListMedia calls ListMediaFunc or returns nil.
func (m *MockMediaRepo) ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error) {
	if m.ListMediaFunc != nil {
		return m.ListMediaFunc(ctx, filter)
	}
	return nil, nil
}

// ListDeletedMedia calls ListDeletedMediaFunc or returns nil.
func (m *MockMediaRepo) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	if m.ListDeletedMediaFunc != nil {
		return m.ListDeletedMediaFunc(ctx)
	}
	return nil, nil
}

// IncrementPlayCount calls IncrementPlayCountFunc or returns nil.
func (m *MockMediaRepo) IncrementPlayCount(ctx context.Context, id int64) error {
	if m.IncrementPlayCountFunc != nil {
		return m.IncrementPlayCountFunc(ctx, id)
	}
	return nil
}

// MockTagRepo is a fake TagRepo.
type MockTagRepo struct {
	CreateTagFunc       func(ctx context.Context, name string) (int64, error)
	GetTagByIDFunc      func(ctx context.Context, id int64) (*model.Tag, error)
	GetTagByNameFunc    func(ctx context.Context, name string) (*model.Tag, error)
	ListTagsFunc        func(ctx context.Context) ([]model.Tag, error)
	DeleteTagFunc       func(ctx context.Context, id int64) error
	AssignTagFunc       func(ctx context.Context, mediaID, tagID int64) error
	RemoveTagFunc       func(ctx context.Context, mediaID, tagID int64) error
	ListTagsByMediaFunc func(ctx context.Context, mediaID int64) ([]model.Tag, error)
}

// CreateTag calls CreateTagFunc or returns a default ID.
func (m *MockTagRepo) CreateTag(ctx context.Context, name string) (int64, error) {
	if m.CreateTagFunc != nil {
		return m.CreateTagFunc(ctx, name)
	}
	return 1, nil
}

// GetTagByID calls GetTagByIDFunc or returns nil.
func (m *MockTagRepo) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	if m.GetTagByIDFunc != nil {
		return m.GetTagByIDFunc(ctx, id)
	}
	return nil, nil
}

// GetTagByName calls GetTagByNameFunc or returns nil.
func (m *MockTagRepo) GetTagByName(ctx context.Context, name string) (*model.Tag, error) {
	if m.GetTagByNameFunc != nil {
		return m.GetTagByNameFunc(ctx, name)
	}
	return nil, nil
}

// ListTags calls ListTagsFunc or returns nil.
func (m *MockTagRepo) ListTags(ctx context.Context) ([]model.Tag, error) {
	if m.ListTagsFunc != nil {
		return m.ListTagsFunc(ctx)
	}
	return nil, nil
}

// DeleteTag calls DeleteTagFunc or returns nil.
func (m *MockTagRepo) DeleteTag(ctx context.Context, id int64) error {
	if m.DeleteTagFunc != nil {
		return m.DeleteTagFunc(ctx, id)
	}
	return nil
}

// AssignTag calls AssignTagFunc or returns nil.
func (m *MockTagRepo) AssignTag(ctx context.Context, mediaID, tagID int64) error {
	if m.AssignTagFunc != nil {
		return m.AssignTagFunc(ctx, mediaID, tagID)
	}
	return nil
}

// RemoveTag calls RemoveTagFunc or returns nil.
func (m *MockTagRepo) RemoveTag(ctx context.Context, mediaID, tagID int64) error {
	if m.RemoveTagFunc != nil {
		return m.RemoveTagFunc(ctx, mediaID, tagID)
	}
	return nil
}

// ListTagsByMedia calls ListTagsByMediaFunc or returns nil.
func (m *MockTagRepo) ListTagsByMedia(ctx context.Context, mediaID int64) ([]model.Tag, error) {
	if m.ListTagsByMediaFunc != nil {
		return m.ListTagsByMediaFunc(ctx, mediaID)
	}
	return nil, nil
}

// MockFavoriteRepo is a fake FavoriteRepo.
type MockFavoriteRepo struct {
	ToggleFavoriteFunc      func(ctx context.Context, userID, mediaID int64) (bool, error)
	IsFavoriteFunc          func(ctx context.Context, userID, mediaID int64) (bool, error)
	ListFavoritesByUserFunc func(ctx context.Context, userID int64) ([]model.Favorite, error)
}

// ToggleFavorite calls ToggleFavoriteFunc or returns false.
func (m *MockFavoriteRepo) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	if m.ToggleFavoriteFunc != nil {
		return m.ToggleFavoriteFunc(ctx, userID, mediaID)
	}
	return false, nil
}

// IsFavorite calls IsFavoriteFunc or returns false.
func (m *MockFavoriteRepo) IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	if m.IsFavoriteFunc != nil {
		return m.IsFavoriteFunc(ctx, userID, mediaID)
	}
	return false, nil
}

// ListFavoritesByUser calls ListFavoritesByUserFunc or returns nil.
func (m *MockFavoriteRepo) ListFavoritesByUser(ctx context.Context, userID int64) ([]model.Favorite, error) {
	if m.ListFavoritesByUserFunc != nil {
		return m.ListFavoritesByUserFunc(ctx, userID)
	}
	return nil, nil
}

// MockPlaybackProgressRepo is a fake PlaybackProgressRepo.
type MockPlaybackProgressRepo struct {
	UpsertProgressFunc     func(ctx context.Context, progress *model.PlaybackProgress) error
	GetProgressFunc        func(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error)
	ListProgressByUserFunc func(ctx context.Context, userID int64) ([]model.PlaybackProgress, error)
}

// UpsertProgress calls UpsertProgressFunc or returns nil.
func (m *MockPlaybackProgressRepo) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	if m.UpsertProgressFunc != nil {
		return m.UpsertProgressFunc(ctx, progress)
	}
	return nil
}

// GetProgress calls GetProgressFunc or returns nil.
func (m *MockPlaybackProgressRepo) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	if m.GetProgressFunc != nil {
		return m.GetProgressFunc(ctx, userID, mediaID)
	}
	return nil, nil
}

// ListProgressByUser calls ListProgressByUserFunc or returns nil.
func (m *MockPlaybackProgressRepo) ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error) {
	if m.ListProgressByUserFunc != nil {
		return m.ListProgressByUserFunc(ctx, userID)
	}
	return nil, nil
}

// MockPlaybackAccumulatorRepo is a fake PlaybackAccumulatorRepo.
type MockPlaybackAccumulatorRepo struct {
	UpsertAccumulatorFunc func(ctx context.Context, acc *model.PlaybackAccumulator) error
	GetAccumulatorFunc    func(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error)
}

// UpsertAccumulator calls UpsertAccumulatorFunc or returns nil.
func (m *MockPlaybackAccumulatorRepo) UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error {
	if m.UpsertAccumulatorFunc != nil {
		return m.UpsertAccumulatorFunc(ctx, acc)
	}
	return nil
}

// GetAccumulator calls GetAccumulatorFunc or returns nil.
func (m *MockPlaybackAccumulatorRepo) GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
	if m.GetAccumulatorFunc != nil {
		return m.GetAccumulatorFunc(ctx, sessionID, mediaID)
	}
	return nil, nil
}

// MockSessionRepo is a fake SessionRepo.
type MockSessionRepo struct {
	CreateSessionFunc         func(ctx context.Context, session *model.Session) error
	GetSessionByIDFunc        func(ctx context.Context, id string) (*model.Session, error)
	DeleteSessionFunc         func(ctx context.Context, id string) error
	DeleteExpiredSessionsFunc func(ctx context.Context, now time.Time) error
}

// CreateSession calls CreateSessionFunc or returns nil.
func (m *MockSessionRepo) CreateSession(ctx context.Context, session *model.Session) error {
	if m.CreateSessionFunc != nil {
		return m.CreateSessionFunc(ctx, session)
	}
	return nil
}

// GetSessionByID calls GetSessionByIDFunc or returns nil.
func (m *MockSessionRepo) GetSessionByID(ctx context.Context, id string) (*model.Session, error) {
	if m.GetSessionByIDFunc != nil {
		return m.GetSessionByIDFunc(ctx, id)
	}
	return nil, nil
}

// DeleteSession calls DeleteSessionFunc or returns nil.
func (m *MockSessionRepo) DeleteSession(ctx context.Context, id string) error {
	if m.DeleteSessionFunc != nil {
		return m.DeleteSessionFunc(ctx, id)
	}
	return nil
}

// DeleteExpiredSessions calls DeleteExpiredSessionsFunc or returns nil.
func (m *MockSessionRepo) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	if m.DeleteExpiredSessionsFunc != nil {
		return m.DeleteExpiredSessionsFunc(ctx, now)
	}
	return nil
}

// MockShareRepo is a fake ShareRepo.
type MockShareRepo struct {
	CreateShareFunc         func(ctx context.Context, share *model.Share) error
	GetShareByTokenFunc     func(ctx context.Context, token string) (*model.Share, error)
	ListSharesByMediaFunc   func(ctx context.Context, mediaID int64) ([]model.Share, error)
	ListSharesByUserFunc    func(ctx context.Context, userID int64) ([]model.Share, error)
	UseShareFunc            func(ctx context.Context, token string) error
	DeleteShareFunc         func(ctx context.Context, token string) error
	DeleteExpiredSharesFunc func(ctx context.Context, now time.Time) error
}

// CreateShare calls CreateShareFunc or returns nil.
func (m *MockShareRepo) CreateShare(ctx context.Context, share *model.Share) error {
	if m.CreateShareFunc != nil {
		return m.CreateShareFunc(ctx, share)
	}
	return nil
}

// GetShareByToken calls GetShareByTokenFunc or returns nil.
func (m *MockShareRepo) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	if m.GetShareByTokenFunc != nil {
		return m.GetShareByTokenFunc(ctx, token)
	}
	return nil, nil
}

// ListSharesByMedia calls ListSharesByMediaFunc or returns nil.
func (m *MockShareRepo) ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error) {
	if m.ListSharesByMediaFunc != nil {
		return m.ListSharesByMediaFunc(ctx, mediaID)
	}
	return nil, nil
}

// ListSharesByUser calls ListSharesByUserFunc or returns nil.
func (m *MockShareRepo) ListSharesByUser(ctx context.Context, userID int64) ([]model.Share, error) {
	if m.ListSharesByUserFunc != nil {
		return m.ListSharesByUserFunc(ctx, userID)
	}
	return nil, nil
}

// UseShare calls UseShareFunc or returns nil.
func (m *MockShareRepo) UseShare(ctx context.Context, token string) error {
	if m.UseShareFunc != nil {
		return m.UseShareFunc(ctx, token)
	}
	return nil
}

// DeleteShare calls DeleteShareFunc or returns nil.
func (m *MockShareRepo) DeleteShare(ctx context.Context, token string) error {
	if m.DeleteShareFunc != nil {
		return m.DeleteShareFunc(ctx, token)
	}
	return nil
}

// DeleteExpiredShares calls DeleteExpiredSharesFunc or returns nil.
func (m *MockShareRepo) DeleteExpiredShares(ctx context.Context, now time.Time) error {
	if m.DeleteExpiredSharesFunc != nil {
		return m.DeleteExpiredSharesFunc(ctx, now)
	}
	return nil
}

// MockNoteRepo is a fake NoteRepo.
type MockNoteRepo struct {
	UpsertNoteFunc func(ctx context.Context, note *model.Note) error
	GetNoteFunc    func(ctx context.Context, mediaID, userID int64) (*model.Note, error)
	DeleteNoteFunc func(ctx context.Context, mediaID, userID int64) error
}

// UpsertNote calls UpsertNoteFunc or returns nil.
func (m *MockNoteRepo) UpsertNote(ctx context.Context, note *model.Note) error {
	if m.UpsertNoteFunc != nil {
		return m.UpsertNoteFunc(ctx, note)
	}
	return nil
}

// GetNote calls GetNoteFunc or returns nil.
func (m *MockNoteRepo) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	if m.GetNoteFunc != nil {
		return m.GetNoteFunc(ctx, mediaID, userID)
	}
	return nil, nil
}

// DeleteNote calls DeleteNoteFunc or returns nil.
func (m *MockNoteRepo) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	if m.DeleteNoteFunc != nil {
		return m.DeleteNoteFunc(ctx, mediaID, userID)
	}
	return nil
}

// CreateFeed implements PodcastRepo.
func (m *MockStore) CreateFeed(ctx context.Context, feed *model.PodcastFeed) (int64, error) {
	return m.PodcastRepo.CreateFeed(ctx, feed)
}

// UpdateFeed implements PodcastRepo.
func (m *MockStore) UpdateFeed(ctx context.Context, feed *model.PodcastFeed) error {
	return m.PodcastRepo.UpdateFeed(ctx, feed)
}

// DeleteFeed implements PodcastRepo.
func (m *MockStore) DeleteFeed(ctx context.Context, id int64) error {
	return m.PodcastRepo.DeleteFeed(ctx, id)
}

// GetFeedByID implements PodcastRepo.
func (m *MockStore) GetFeedByID(ctx context.Context, id int64) (*model.PodcastFeed, error) {
	return m.PodcastRepo.GetFeedByID(ctx, id)
}

// GetFeedBySetID implements PodcastRepo.
func (m *MockStore) GetFeedBySetID(ctx context.Context, setID int64) (*model.PodcastFeed, error) {
	return m.PodcastRepo.GetFeedBySetID(ctx, setID)
}

// ListFeedsBySetID implements PodcastRepo.
func (m *MockStore) ListFeedsBySetID(ctx context.Context, setID int64) ([]model.PodcastFeed, error) {
	return m.PodcastRepo.ListFeedsBySetID(ctx, setID)
}

// ListFeeds implements PodcastRepo.
func (m *MockStore) ListFeeds(ctx context.Context) ([]model.PodcastFeed, error) {
	return m.PodcastRepo.ListFeeds(ctx)
}

// ListFeedsNeedingCheck implements PodcastRepo.
func (m *MockStore) ListFeedsNeedingCheck(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
	return m.PodcastRepo.ListFeedsNeedingCheck(ctx, now, before)
}

// CreateEpisode implements PodcastRepo.
func (m *MockStore) CreateEpisode(ctx context.Context, episode *model.PodcastEpisode) (int64, error) {
	return m.PodcastRepo.CreateEpisode(ctx, episode)
}

// GetEpisodeByID implements PodcastRepo.
func (m *MockStore) GetEpisodeByID(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
	return m.PodcastRepo.GetEpisodeByID(ctx, id)
}

// GetEpisodeByGUID implements PodcastRepo.
func (m *MockStore) GetEpisodeByGUID(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error) {
	return m.PodcastRepo.GetEpisodeByGUID(ctx, feedID, guid)
}

// ListEpisodesByFeed implements PodcastRepo.
func (m *MockStore) ListEpisodesByFeed(ctx context.Context, feedID int64, limit, offset int) ([]model.PodcastEpisode, error) {
	return m.PodcastRepo.ListEpisodesByFeed(ctx, feedID, limit, offset)
}

// UpdateEpisodeMedia implements PodcastRepo.
func (m *MockStore) UpdateEpisodeMedia(ctx context.Context, episodeID, mediaID int64, fileName string) error {
	return m.PodcastRepo.UpdateEpisodeMedia(ctx, episodeID, mediaID, fileName)
}

// DeleteEpisodesByFeed implements PodcastRepo.
func (m *MockStore) DeleteEpisodesByFeed(ctx context.Context, feedID int64) error {
	return m.PodcastRepo.DeleteEpisodesByFeed(ctx, feedID)
}

// UpsertEpisodeProgress implements PodcastRepo.
func (m *MockStore) UpsertEpisodeProgress(ctx context.Context, status *model.PodcastStatus) error {
	return m.PodcastRepo.UpsertEpisodeProgress(ctx, status)
}

// GetEpisodeProgress implements PodcastRepo.
func (m *MockStore) GetEpisodeProgress(ctx context.Context, userID, episodeID int64) (*model.PodcastStatus, error) {
	return m.PodcastRepo.GetEpisodeProgress(ctx, userID, episodeID)
}

// ListEpisodesWithStatus implements PodcastRepo.
func (m *MockStore) ListEpisodesWithStatus(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
	return m.PodcastRepo.ListEpisodesWithStatus(ctx, userID, feedID, limit, offset)
}

// MockPodcastRepo is a fake PodcastRepo.
type MockPodcastRepo struct {
	CreateFeedFunc            func(ctx context.Context, feed *model.PodcastFeed) (int64, error)
	UpdateFeedFunc            func(ctx context.Context, feed *model.PodcastFeed) error
	DeleteFeedFunc            func(ctx context.Context, id int64) error
	GetFeedByIDFunc           func(ctx context.Context, id int64) (*model.PodcastFeed, error)
	GetFeedBySetIDFunc        func(ctx context.Context, setID int64) (*model.PodcastFeed, error)
	ListFeedsBySetIDFunc      func(ctx context.Context, setID int64) ([]model.PodcastFeed, error)
	ListFeedsFunc             func(ctx context.Context) ([]model.PodcastFeed, error)
	ListFeedsNeedingCheckFunc func(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error)

	CreateEpisodeFunc        func(ctx context.Context, episode *model.PodcastEpisode) (int64, error)
	GetEpisodeByIDFunc       func(ctx context.Context, id int64) (*model.PodcastEpisode, error)
	GetEpisodeByGUIDFunc     func(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error)
	ListEpisodesByFeedFunc   func(ctx context.Context, feedID int64, limit, offset int) ([]model.PodcastEpisode, error)
	UpdateEpisodeMediaFunc   func(ctx context.Context, episodeID, mediaID int64, fileName string) error
	DeleteEpisodesByFeedFunc func(ctx context.Context, feedID int64) error

	UpsertEpisodeProgressFunc  func(ctx context.Context, status *model.PodcastStatus) error
	GetEpisodeProgressFunc     func(ctx context.Context, userID, episodeID int64) (*model.PodcastStatus, error)
	ListEpisodesWithStatusFunc func(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error)
}

// CreateFeed calls CreateFeedFunc or returns a default ID.
func (m *MockPodcastRepo) CreateFeed(ctx context.Context, feed *model.PodcastFeed) (int64, error) {
	if m.CreateFeedFunc != nil {
		return m.CreateFeedFunc(ctx, feed)
	}
	return 1, nil
}

// UpdateFeed calls UpdateFeedFunc or returns nil.
func (m *MockPodcastRepo) UpdateFeed(ctx context.Context, feed *model.PodcastFeed) error {
	if m.UpdateFeedFunc != nil {
		return m.UpdateFeedFunc(ctx, feed)
	}
	return nil
}

// DeleteFeed calls DeleteFeedFunc or returns nil.
func (m *MockPodcastRepo) DeleteFeed(ctx context.Context, id int64) error {
	if m.DeleteFeedFunc != nil {
		return m.DeleteFeedFunc(ctx, id)
	}
	return nil
}

// GetFeedByID calls GetFeedByIDFunc or returns nil.
func (m *MockPodcastRepo) GetFeedByID(ctx context.Context, id int64) (*model.PodcastFeed, error) {
	if m.GetFeedByIDFunc != nil {
		return m.GetFeedByIDFunc(ctx, id)
	}
	return nil, nil
}

// GetFeedBySetID calls GetFeedBySetIDFunc or returns nil.
func (m *MockPodcastRepo) GetFeedBySetID(ctx context.Context, setID int64) (*model.PodcastFeed, error) {
	if m.GetFeedBySetIDFunc != nil {
		return m.GetFeedBySetIDFunc(ctx, setID)
	}
	return nil, nil
}

// ListFeedsBySetID calls ListFeedsBySetIDFunc or returns nil.
func (m *MockPodcastRepo) ListFeedsBySetID(ctx context.Context, setID int64) ([]model.PodcastFeed, error) {
	if m.ListFeedsBySetIDFunc != nil {
		return m.ListFeedsBySetIDFunc(ctx, setID)
	}
	return nil, nil
}

// ListFeeds calls ListFeedsFunc or returns nil.
func (m *MockPodcastRepo) ListFeeds(ctx context.Context) ([]model.PodcastFeed, error) {
	if m.ListFeedsFunc != nil {
		return m.ListFeedsFunc(ctx)
	}
	return nil, nil
}

// ListFeedsNeedingCheck calls ListFeedsNeedingCheckFunc or returns nil.
func (m *MockPodcastRepo) ListFeedsNeedingCheck(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
	if m.ListFeedsNeedingCheckFunc != nil {
		return m.ListFeedsNeedingCheckFunc(ctx, now, before)
	}
	return nil, nil
}

// CreateEpisode calls CreateEpisodeFunc or returns a default ID.
func (m *MockPodcastRepo) CreateEpisode(ctx context.Context, episode *model.PodcastEpisode) (int64, error) {
	if m.CreateEpisodeFunc != nil {
		return m.CreateEpisodeFunc(ctx, episode)
	}
	return 1, nil
}

// GetEpisodeByID calls GetEpisodeByIDFunc or returns nil.
func (m *MockPodcastRepo) GetEpisodeByID(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
	if m.GetEpisodeByIDFunc != nil {
		return m.GetEpisodeByIDFunc(ctx, id)
	}
	return nil, nil
}

// GetEpisodeByGUID calls GetEpisodeByGUIDFunc or returns nil.
func (m *MockPodcastRepo) GetEpisodeByGUID(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error) {
	if m.GetEpisodeByGUIDFunc != nil {
		return m.GetEpisodeByGUIDFunc(ctx, feedID, guid)
	}
	return nil, nil
}

// ListEpisodesByFeed calls ListEpisodesByFeedFunc or returns nil.
func (m *MockPodcastRepo) ListEpisodesByFeed(ctx context.Context, feedID int64, limit, offset int) ([]model.PodcastEpisode, error) {
	if m.ListEpisodesByFeedFunc != nil {
		return m.ListEpisodesByFeedFunc(ctx, feedID, limit, offset)
	}
	return nil, nil
}

// UpdateEpisodeMedia calls UpdateEpisodeMediaFunc or returns nil.
func (m *MockPodcastRepo) UpdateEpisodeMedia(ctx context.Context, episodeID, mediaID int64, fileName string) error {
	if m.UpdateEpisodeMediaFunc != nil {
		return m.UpdateEpisodeMediaFunc(ctx, episodeID, mediaID, fileName)
	}
	return nil
}

// DeleteEpisodesByFeed calls DeleteEpisodesByFeedFunc or returns nil.
func (m *MockPodcastRepo) DeleteEpisodesByFeed(ctx context.Context, feedID int64) error {
	if m.DeleteEpisodesByFeedFunc != nil {
		return m.DeleteEpisodesByFeedFunc(ctx, feedID)
	}
	return nil
}

// UpsertEpisodeProgress calls UpsertEpisodeProgressFunc or returns nil.
func (m *MockPodcastRepo) UpsertEpisodeProgress(ctx context.Context, status *model.PodcastStatus) error {
	if m.UpsertEpisodeProgressFunc != nil {
		return m.UpsertEpisodeProgressFunc(ctx, status)
	}
	return nil
}

// GetEpisodeProgress calls GetEpisodeProgressFunc or returns nil.
func (m *MockPodcastRepo) GetEpisodeProgress(ctx context.Context, userID, episodeID int64) (*model.PodcastStatus, error) {
	if m.GetEpisodeProgressFunc != nil {
		return m.GetEpisodeProgressFunc(ctx, userID, episodeID)
	}
	return nil, nil
}

// ListEpisodesWithStatus calls ListEpisodesWithStatusFunc or returns nil.
func (m *MockPodcastRepo) ListEpisodesWithStatus(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
	if m.ListEpisodesWithStatusFunc != nil {
		return m.ListEpisodesWithStatusFunc(ctx, userID, feedID, limit, offset)
	}
	return nil, nil
}
