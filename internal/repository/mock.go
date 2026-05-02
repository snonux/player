// Package repository provides data access abstractions.
package repository

import (
	"context"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// compile-time checks.
var (
	_ Store                = (*MockStore)(nil)
	_ MediaServiceStore    = (*MockStore)(nil)
	_ AdminServiceStore    = (*MockStore)(nil)
	_ AuthServiceStore     = (*MockStore)(nil)
	_ ProgressServiceStore = (*MockStore)(nil)
	_ GCStore              = (*MockStore)(nil)
	_ ScannerStore         = (*MockStore)(nil)
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
}

func (m *MockStore) CreateUser(ctx context.Context, user *model.User) (int64, error) {
	return m.UserRepo.CreateUser(ctx, user)
}
func (m *MockStore) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return m.UserRepo.GetUserByID(ctx, id)
}
func (m *MockStore) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	return m.UserRepo.GetUserByUsername(ctx, username)
}
func (m *MockStore) ListUsers(ctx context.Context) ([]model.User, error) {
	return m.UserRepo.ListUsers(ctx)
}
func (m *MockStore) DeleteUser(ctx context.Context, id int64) error {
	return m.UserRepo.DeleteUser(ctx, id)
}
func (m *MockStore) CountUsers(ctx context.Context) (int, error) { return m.UserRepo.CountUsers(ctx) }

func (m *MockStore) CreateSet(ctx context.Context, set *model.Set) (int64, error) {
	return m.SetRepo.CreateSet(ctx, set)
}
func (m *MockStore) GetSetByID(ctx context.Context, id int64) (*model.Set, error) {
	return m.SetRepo.GetSetByID(ctx, id)
}
func (m *MockStore) ListSets(ctx context.Context) ([]model.Set, error) {
	return m.SetRepo.ListSets(ctx)
}
func (m *MockStore) UpdateSet(ctx context.Context, set *model.Set) error {
	return m.SetRepo.UpdateSet(ctx, set)
}
func (m *MockStore) DeleteSet(ctx context.Context, id int64) error {
	return m.SetRepo.DeleteSet(ctx, id)
}

func (m *MockStore) GrantPermission(ctx context.Context, perm *model.SetPermission) error {
	return m.SetPermissionRepo.GrantPermission(ctx, perm)
}
func (m *MockStore) RevokePermission(ctx context.Context, setID, userID int64) error {
	return m.SetPermissionRepo.RevokePermission(ctx, setID, userID)
}
func (m *MockStore) GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
	return m.SetPermissionRepo.GetPermission(ctx, setID, userID)
}
func (m *MockStore) ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error) {
	return m.SetPermissionRepo.ListPermissionsBySet(ctx, setID)
}
func (m *MockStore) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error) {
	return m.SetPermissionRepo.ListPermissionsByUser(ctx, userID)
}

func (m *MockStore) CreateMedia(ctx context.Context, media *model.Media) (int64, error) {
	return m.MediaRepo.CreateMedia(ctx, media)
}
func (m *MockStore) GetMediaByID(ctx context.Context, id int64) (*model.Media, error) {
	return m.MediaRepo.GetMediaByID(ctx, id)
}
func (m *MockStore) UpdateMedia(ctx context.Context, media *model.Media) error {
	return m.MediaRepo.UpdateMedia(ctx, media)
}
func (m *MockStore) UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error {
	return m.MediaRepo.UpdateMediaThumbnail(ctx, id, thumbnailPath)
}
func (m *MockStore) SoftDeleteMedia(ctx context.Context, id int64) error {
	return m.MediaRepo.SoftDeleteMedia(ctx, id)
}
func (m *MockStore) RestoreMedia(ctx context.Context, id int64) error {
	return m.MediaRepo.RestoreMedia(ctx, id)
}
func (m *MockStore) HardDeleteMedia(ctx context.Context, id int64) error {
	return m.MediaRepo.HardDeleteMedia(ctx, id)
}
func (m *MockStore) ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error) {
	return m.MediaRepo.ListMedia(ctx, filter)
}
func (m *MockStore) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	return m.MediaRepo.ListDeletedMedia(ctx)
}
func (m *MockStore) IncrementPlayCount(ctx context.Context, id int64) error {
	return m.MediaRepo.IncrementPlayCount(ctx, id)
}

func (m *MockStore) CreateTag(ctx context.Context, name string) (int64, error) {
	return m.TagRepo.CreateTag(ctx, name)
}
func (m *MockStore) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	return m.TagRepo.GetTagByID(ctx, id)
}
func (m *MockStore) GetTagByName(ctx context.Context, name string) (*model.Tag, error) {
	return m.TagRepo.GetTagByName(ctx, name)
}
func (m *MockStore) ListTags(ctx context.Context) ([]model.Tag, error) {
	return m.TagRepo.ListTags(ctx)
}
func (m *MockStore) DeleteTag(ctx context.Context, id int64) error {
	return m.TagRepo.DeleteTag(ctx, id)
}
func (m *MockStore) AssignTag(ctx context.Context, mediaID, tagID int64) error {
	return m.TagRepo.AssignTag(ctx, mediaID, tagID)
}
func (m *MockStore) RemoveTag(ctx context.Context, mediaID, tagID int64) error {
	return m.TagRepo.RemoveTag(ctx, mediaID, tagID)
}
func (m *MockStore) ListTagsByMedia(ctx context.Context, mediaID int64) ([]model.Tag, error) {
	return m.TagRepo.ListTagsByMedia(ctx, mediaID)
}

func (m *MockStore) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	return m.FavoriteRepo.ToggleFavorite(ctx, userID, mediaID)
}
func (m *MockStore) IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	return m.FavoriteRepo.IsFavorite(ctx, userID, mediaID)
}
func (m *MockStore) ListFavoritesByUser(ctx context.Context, userID int64) ([]model.Favorite, error) {
	return m.FavoriteRepo.ListFavoritesByUser(ctx, userID)
}

func (m *MockStore) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	return m.PlaybackProgressRepo.UpsertProgress(ctx, progress)
}
func (m *MockStore) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	return m.PlaybackProgressRepo.GetProgress(ctx, userID, mediaID)
}
func (m *MockStore) ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error) {
	return m.PlaybackProgressRepo.ListProgressByUser(ctx, userID)
}

func (m *MockStore) UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error {
	return m.PlaybackAccumulatorRepo.UpsertAccumulator(ctx, acc)
}
func (m *MockStore) GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
	return m.PlaybackAccumulatorRepo.GetAccumulator(ctx, sessionID, mediaID)
}

func (m *MockStore) CreateSession(ctx context.Context, session *model.Session) error {
	return m.SessionRepo.CreateSession(ctx, session)
}
func (m *MockStore) GetSessionByID(ctx context.Context, id string) (*model.Session, error) {
	return m.SessionRepo.GetSessionByID(ctx, id)
}
func (m *MockStore) DeleteSession(ctx context.Context, id string) error {
	return m.SessionRepo.DeleteSession(ctx, id)
}
func (m *MockStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return m.SessionRepo.DeleteExpiredSessions(ctx, now)
}

func (m *MockStore) CreateShare(ctx context.Context, share *model.Share) error {
	return m.ShareRepo.CreateShare(ctx, share)
}
func (m *MockStore) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	return m.ShareRepo.GetShareByToken(ctx, token)
}
func (m *MockStore) ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error) {
	return m.ShareRepo.ListSharesByMedia(ctx, mediaID)
}
func (m *MockStore) ListSharesByUser(ctx context.Context, userID int64) ([]model.Share, error) {
	return m.ShareRepo.ListSharesByUser(ctx, userID)
}
func (m *MockStore) UseShare(ctx context.Context, token string) error {
	return m.ShareRepo.UseShare(ctx, token)
}
func (m *MockStore) DeleteShare(ctx context.Context, token string) error {
	return m.ShareRepo.DeleteShare(ctx, token)
}
func (m *MockStore) DeleteExpiredShares(ctx context.Context, now time.Time) error {
	return m.ShareRepo.DeleteExpiredShares(ctx, now)
}

func (m *MockStore) UpsertNote(ctx context.Context, note *model.Note) error {
	return m.NoteRepo.UpsertNote(ctx, note)
}
func (m *MockStore) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	return m.NoteRepo.GetNote(ctx, mediaID, userID)
}
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

func (m *MockUserRepo) CreateUser(ctx context.Context, user *model.User) (int64, error) {
	if m.CreateUserFunc != nil {
		return m.CreateUserFunc(ctx, user)
	}
	return 1, nil
}
func (m *MockUserRepo) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	if m.GetUserByIDFunc != nil {
		return m.GetUserByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *MockUserRepo) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	if m.GetUserByUsernameFunc != nil {
		return m.GetUserByUsernameFunc(ctx, username)
	}
	return nil, nil
}
func (m *MockUserRepo) ListUsers(ctx context.Context) ([]model.User, error) {
	if m.ListUsersFunc != nil {
		return m.ListUsersFunc(ctx)
	}
	return nil, nil
}
func (m *MockUserRepo) DeleteUser(ctx context.Context, id int64) error {
	if m.DeleteUserFunc != nil {
		return m.DeleteUserFunc(ctx, id)
	}
	return nil
}
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

func (m *MockSetRepo) CreateSet(ctx context.Context, set *model.Set) (int64, error) {
	if m.CreateSetFunc != nil {
		return m.CreateSetFunc(ctx, set)
	}
	return 1, nil
}
func (m *MockSetRepo) GetSetByID(ctx context.Context, id int64) (*model.Set, error) {
	if m.GetSetByIDFunc != nil {
		return m.GetSetByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *MockSetRepo) ListSets(ctx context.Context) ([]model.Set, error) {
	if m.ListSetsFunc != nil {
		return m.ListSetsFunc(ctx)
	}
	return nil, nil
}
func (m *MockSetRepo) UpdateSet(ctx context.Context, set *model.Set) error {
	if m.UpdateSetFunc != nil {
		return m.UpdateSetFunc(ctx, set)
	}
	return nil
}
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

func (m *MockSetPermissionRepo) GrantPermission(ctx context.Context, perm *model.SetPermission) error {
	if m.GrantPermissionFunc != nil {
		return m.GrantPermissionFunc(ctx, perm)
	}
	return nil
}
func (m *MockSetPermissionRepo) RevokePermission(ctx context.Context, setID, userID int64) error {
	if m.RevokePermissionFunc != nil {
		return m.RevokePermissionFunc(ctx, setID, userID)
	}
	return nil
}
func (m *MockSetPermissionRepo) GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
	if m.GetPermissionFunc != nil {
		return m.GetPermissionFunc(ctx, setID, userID)
	}
	return nil, nil
}
func (m *MockSetPermissionRepo) ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error) {
	if m.ListPermissionsBySetFunc != nil {
		return m.ListPermissionsBySetFunc(ctx, setID)
	}
	return nil, nil
}
func (m *MockSetPermissionRepo) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error) {
	if m.ListPermissionsByUserFunc != nil {
		return m.ListPermissionsByUserFunc(ctx, userID)
	}
	return nil, nil
}

// MockMediaRepo is a fake MediaRepo.
type MockMediaRepo struct {
	CreateMediaFunc         func(ctx context.Context, media *model.Media) (int64, error)
	GetMediaByIDFunc        func(ctx context.Context, id int64) (*model.Media, error)
	UpdateMediaFunc         func(ctx context.Context, media *model.Media) error
	UpdateMediaThumbnailFunc func(ctx context.Context, id int64, thumbnailPath string) error
	SoftDeleteMediaFunc     func(ctx context.Context, id int64) error
	RestoreMediaFunc        func(ctx context.Context, id int64) error
	HardDeleteMediaFunc     func(ctx context.Context, id int64) error
	ListMediaFunc           func(ctx context.Context, filter MediaFilter) ([]model.Media, error)
	ListDeletedMediaFunc    func(ctx context.Context) ([]model.Media, error)
	IncrementPlayCountFunc func(ctx context.Context, id int64) error
}

func (m *MockMediaRepo) CreateMedia(ctx context.Context, media *model.Media) (int64, error) {
	if m.CreateMediaFunc != nil {
		return m.CreateMediaFunc(ctx, media)
	}
	return 1, nil
}
func (m *MockMediaRepo) GetMediaByID(ctx context.Context, id int64) (*model.Media, error) {
	if m.GetMediaByIDFunc != nil {
		return m.GetMediaByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *MockMediaRepo) UpdateMedia(ctx context.Context, media *model.Media) error {
	if m.UpdateMediaFunc != nil {
		return m.UpdateMediaFunc(ctx, media)
	}
	return nil
}
func (m *MockMediaRepo) UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error {
	if m.UpdateMediaThumbnailFunc != nil {
		return m.UpdateMediaThumbnailFunc(ctx, id, thumbnailPath)
	}
	return nil
}
func (m *MockMediaRepo) SoftDeleteMedia(ctx context.Context, id int64) error {
	if m.SoftDeleteMediaFunc != nil {
		return m.SoftDeleteMediaFunc(ctx, id)
	}
	return nil
}
func (m *MockMediaRepo) RestoreMedia(ctx context.Context, id int64) error {
	if m.RestoreMediaFunc != nil {
		return m.RestoreMediaFunc(ctx, id)
	}
	return nil
}
func (m *MockMediaRepo) HardDeleteMedia(ctx context.Context, id int64) error {
	if m.HardDeleteMediaFunc != nil {
		return m.HardDeleteMediaFunc(ctx, id)
	}
	return nil
}
func (m *MockMediaRepo) ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error) {
	if m.ListMediaFunc != nil {
		return m.ListMediaFunc(ctx, filter)
	}
	return nil, nil
}
func (m *MockMediaRepo) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	if m.ListDeletedMediaFunc != nil {
		return m.ListDeletedMediaFunc(ctx)
	}
	return nil, nil
}
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

func (m *MockTagRepo) CreateTag(ctx context.Context, name string) (int64, error) {
	if m.CreateTagFunc != nil {
		return m.CreateTagFunc(ctx, name)
	}
	return 1, nil
}
func (m *MockTagRepo) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	if m.GetTagByIDFunc != nil {
		return m.GetTagByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *MockTagRepo) GetTagByName(ctx context.Context, name string) (*model.Tag, error) {
	if m.GetTagByNameFunc != nil {
		return m.GetTagByNameFunc(ctx, name)
	}
	return nil, nil
}
func (m *MockTagRepo) ListTags(ctx context.Context) ([]model.Tag, error) {
	if m.ListTagsFunc != nil {
		return m.ListTagsFunc(ctx)
	}
	return nil, nil
}
func (m *MockTagRepo) DeleteTag(ctx context.Context, id int64) error {
	if m.DeleteTagFunc != nil {
		return m.DeleteTagFunc(ctx, id)
	}
	return nil
}
func (m *MockTagRepo) AssignTag(ctx context.Context, mediaID, tagID int64) error {
	if m.AssignTagFunc != nil {
		return m.AssignTagFunc(ctx, mediaID, tagID)
	}
	return nil
}
func (m *MockTagRepo) RemoveTag(ctx context.Context, mediaID, tagID int64) error {
	if m.RemoveTagFunc != nil {
		return m.RemoveTagFunc(ctx, mediaID, tagID)
	}
	return nil
}
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

func (m *MockFavoriteRepo) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	if m.ToggleFavoriteFunc != nil {
		return m.ToggleFavoriteFunc(ctx, userID, mediaID)
	}
	return false, nil
}
func (m *MockFavoriteRepo) IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	if m.IsFavoriteFunc != nil {
		return m.IsFavoriteFunc(ctx, userID, mediaID)
	}
	return false, nil
}
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

func (m *MockPlaybackProgressRepo) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	if m.UpsertProgressFunc != nil {
		return m.UpsertProgressFunc(ctx, progress)
	}
	return nil
}
func (m *MockPlaybackProgressRepo) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	if m.GetProgressFunc != nil {
		return m.GetProgressFunc(ctx, userID, mediaID)
	}
	return nil, nil
}
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

func (m *MockPlaybackAccumulatorRepo) UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error {
	if m.UpsertAccumulatorFunc != nil {
		return m.UpsertAccumulatorFunc(ctx, acc)
	}
	return nil
}
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

func (m *MockSessionRepo) CreateSession(ctx context.Context, session *model.Session) error {
	if m.CreateSessionFunc != nil {
		return m.CreateSessionFunc(ctx, session)
	}
	return nil
}
func (m *MockSessionRepo) GetSessionByID(ctx context.Context, id string) (*model.Session, error) {
	if m.GetSessionByIDFunc != nil {
		return m.GetSessionByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *MockSessionRepo) DeleteSession(ctx context.Context, id string) error {
	if m.DeleteSessionFunc != nil {
		return m.DeleteSessionFunc(ctx, id)
	}
	return nil
}
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

func (m *MockShareRepo) CreateShare(ctx context.Context, share *model.Share) error {
	if m.CreateShareFunc != nil {
		return m.CreateShareFunc(ctx, share)
	}
	return nil
}
func (m *MockShareRepo) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	if m.GetShareByTokenFunc != nil {
		return m.GetShareByTokenFunc(ctx, token)
	}
	return nil, nil
}
func (m *MockShareRepo) ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error) {
	if m.ListSharesByMediaFunc != nil {
		return m.ListSharesByMediaFunc(ctx, mediaID)
	}
	return nil, nil
}
func (m *MockShareRepo) ListSharesByUser(ctx context.Context, userID int64) ([]model.Share, error) {
	if m.ListSharesByUserFunc != nil {
		return m.ListSharesByUserFunc(ctx, userID)
	}
	return nil, nil
}
func (m *MockShareRepo) UseShare(ctx context.Context, token string) error {
	if m.UseShareFunc != nil {
		return m.UseShareFunc(ctx, token)
	}
	return nil
}
func (m *MockShareRepo) DeleteShare(ctx context.Context, token string) error {
	if m.DeleteShareFunc != nil {
		return m.DeleteShareFunc(ctx, token)
	}
	return nil
}
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

func (m *MockNoteRepo) UpsertNote(ctx context.Context, note *model.Note) error {
	if m.UpsertNoteFunc != nil {
		return m.UpsertNoteFunc(ctx, note)
	}
	return nil
}
func (m *MockNoteRepo) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	if m.GetNoteFunc != nil {
		return m.GetNoteFunc(ctx, mediaID, userID)
	}
	return nil, nil
}
func (m *MockNoteRepo) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	if m.DeleteNoteFunc != nil {
		return m.DeleteNoteFunc(ctx, mediaID, userID)
	}
	return nil
}
