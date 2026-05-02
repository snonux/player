package service

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestMockMediaService_Defaults(t *testing.T) {
	ctx := context.Background()
	m := &MockMediaService{}

	// Methods that return nil, nil or zero-value defaults
	m.ListSets(ctx, 1)
	m.GetMediaDetail(ctx, 1, 1)
	m.ListMedia(ctx, 1, repository.MediaFilter{})
	m.ToggleFavorite(ctx, 1, 1)
	m.AssignTag(ctx, 1, 1, "rock")
	m.RemoveTag(ctx, 1, 1, "rock")
	m.SoftDeleteMedia(ctx, 1, 1)
	m.RestoreMedia(ctx, 1, 1)
	m.RegenerateThumbnail(ctx, 1, 1)
	m.RegenerateSetCover(ctx, 1, "", 1)
	m.GetNote(ctx, 1, 1)
	m.UpsertNote(ctx, &model.Note{MediaID: 1, UserID: 1, Content: "hi"})
	m.DeleteNote(ctx, 1, 1)
	m.ListShares(ctx, 1, 1)
	m.RevokeShare(ctx, "abc", 1)
	m.ValidateShareToken(ctx, "abc")

	// Methods that return errors when not implemented
	if _, err := m.StreamMedia(ctx, 1, 1); err == nil {
		t.Fatal("expected error")
	}
	if _, err := m.DownloadMedia(ctx, 1, 1); err == nil {
		t.Fatal("expected error")
	}
	if _, err := m.GetThumbnail(ctx, 1, 1); err == nil {
		t.Fatal("expected error")
	}
	if _, err := m.UploadMedia(ctx, 1, 1, "x.mp3", strings.NewReader("x"), 1); err == nil {
		t.Fatal("expected error")
	}
	if _, err := m.CreateShare(ctx, 1, 1, time.Now()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := m.StreamSharedMedia(ctx, "abc"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMockMediaService_WithFuncs(t *testing.T) {
	ctx := context.Background()
	m := &MockMediaService{
		ListSetsFunc:            func(ctx context.Context, userID int64) ([]model.Set, error) { return nil, nil },
		GetMediaDetailFunc:      func(ctx context.Context, mediaID, userID int64) (*MediaDetail, error) { return nil, nil },
		ListMediaFunc:           func(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error) { return nil, nil },
		StreamMediaFunc:         func(ctx context.Context, mediaID, userID int64) (*FileResult, error) { return nil, nil },
		DownloadMediaFunc:       func(ctx context.Context, mediaID, userID int64) (*FileResult, error) { return nil, nil },
		GetThumbnailFunc:        func(ctx context.Context, mediaID, userID int64) (*FileResult, error) { return nil, nil },
		RegenerateThumbnailFunc: func(ctx context.Context, mediaID, userID int64) error { return nil },
		RegenerateSetCoverFunc:  func(ctx context.Context, setID int64, folder string, userID int64) error { return nil },
		ToggleFavoriteFunc:      func(ctx context.Context, userID, mediaID int64) (bool, error) { return false, nil },
		AssignTagFunc:           func(ctx context.Context, mediaID, userID int64, tagName string) error { return nil },
		RemoveTagFunc:           func(ctx context.Context, mediaID, userID int64, tagName string) error { return nil },
		SoftDeleteMediaFunc:     func(ctx context.Context, mediaID, userID int64) error { return nil },
		RestoreMediaFunc:        func(ctx context.Context, mediaID, userID int64) error { return nil },
		UploadMediaFunc:         func(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error) { return nil, nil },
		CreateShareFunc:         func(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error) { return nil, nil },
		ListSharesFunc:          func(ctx context.Context, mediaID, userID int64) ([]model.Share, error) { return nil, nil },
		RevokeShareFunc:         func(ctx context.Context, token string, userID int64) error { return nil },
		ValidateShareTokenFunc:  func(ctx context.Context, token string) (*model.Share, error) { return nil, nil },
		StreamSharedMediaFunc:   func(ctx context.Context, token string) (*FileResult, error) { return nil, nil },
		GetNoteFunc:             func(ctx context.Context, mediaID, userID int64) (*model.Note, error) { return nil, nil },
		UpsertNoteFunc:          func(ctx context.Context, note *model.Note) error { return nil },
		DeleteNoteFunc:          func(ctx context.Context, mediaID, userID int64) error { return nil },
	}

	m.ListSets(ctx, 1)
	m.GetMediaDetail(ctx, 1, 1)
	m.ListMedia(ctx, 1, repository.MediaFilter{})
	m.StreamMedia(ctx, 1, 1)
	m.DownloadMedia(ctx, 1, 1)
	m.GetThumbnail(ctx, 1, 1)
	m.RegenerateThumbnail(ctx, 1, 1)
	m.RegenerateSetCover(ctx, 1, "", 1)
	m.ToggleFavorite(ctx, 1, 1)
	m.AssignTag(ctx, 1, 1, "rock")
	m.RemoveTag(ctx, 1, 1, "rock")
	m.SoftDeleteMedia(ctx, 1, 1)
	m.RestoreMedia(ctx, 1, 1)
	m.UploadMedia(ctx, 1, 1, "x.mp3", strings.NewReader("x"), 1)
	m.CreateShare(ctx, 1, 1, time.Now())
	m.ListShares(ctx, 1, 1)
	m.RevokeShare(ctx, "abc", 1)
	m.ValidateShareToken(ctx, "abc")
	m.StreamSharedMedia(ctx, "abc")
	m.GetNote(ctx, 1, 1)
	m.UpsertNote(ctx, &model.Note{MediaID: 1, UserID: 1, Content: "hi"})
	m.DeleteNote(ctx, 1, 1)
}

func TestMockAdminService_Defaults(t *testing.T) {
	ctx := context.Background()
	m := &MockAdminService{}

	m.ListTrash(ctx)
	m.TriggerRescan(ctx)
	m.ListUsers(ctx)
	m.DeleteUser(ctx, 1)
	m.ListPermissions(ctx)
	m.GrantPermission(ctx, 1, 2, model.RoleViewer)
	m.RevokePermission(ctx, 1, 2)

	if _, err := m.CreateUser(ctx, "alice", "secret", false); err == nil {
		t.Fatal("expected error")
	}
}

func TestMockAdminService_WithFuncs(t *testing.T) {
	ctx := context.Background()
	m := &MockAdminService{
		ListTrashFunc:        func(ctx context.Context) ([]model.Media, error) { return nil, nil },
		TriggerRescanFunc:    func(ctx context.Context) error { return nil },
		ListUsersFunc:        func(ctx context.Context) ([]model.User, error) { return nil, nil },
		CreateUserFunc:       func(ctx context.Context, username, password string, isAdmin bool) (*model.User, error) { return nil, nil },
		DeleteUserFunc:       func(ctx context.Context, id int64) error { return nil },
		ListPermissionsFunc:  func(ctx context.Context) (*PermissionsMatrix, error) { return nil, nil },
		GrantPermissionFunc:  func(ctx context.Context, setID, userID int64, role model.Role) error { return nil },
		RevokePermissionFunc: func(ctx context.Context, setID, userID int64) error { return nil },
	}

	m.ListTrash(ctx)
	m.TriggerRescan(ctx)
	m.ListUsers(ctx)
	m.CreateUser(ctx, "alice", "secret", false)
	m.DeleteUser(ctx, 1)
	m.ListPermissions(ctx)
	m.GrantPermission(ctx, 1, 2, model.RoleViewer)
	m.RevokePermission(ctx, 1, 2)
}

func TestMockProgressService_Defaults(t *testing.T) {
	ctx := context.Background()
	m := &MockProgressService{}
	m.UpdateProgress(ctx, "sess", 1, 1, 10)
}

func TestMockProgressService_WithFunc(t *testing.T) {
	ctx := context.Background()
	m := &MockProgressService{
		UpdateProgressFunc: func(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error { return nil },
	}
	m.UpdateProgress(ctx, "sess", 1, 1, 10)
}
