package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func newMockClock() *clock.MockClock {
	return &clock.MockClock{T: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestMediaService_ListSets(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		userID    int64
		user      *model.User
		userErr   error
		sets      []model.Set
		setsErr   error
		perms     []model.SetPermission
		permsErr  error
		wantCount int
		wantErr   bool
	}{
		{
			name:      "admin sees all sets",
			userID:    1,
			user:      &model.User{ID: 1, IsAdmin: true},
			sets:      []model.Set{{ID: 1}, {ID: 2}, {ID: 3}},
			wantCount: 3,
		},
		{
			name:   "non-admin with perms",
			userID: 2,
			user:   &model.User{ID: 2, IsAdmin: false},
			sets: []model.Set{
				{ID: 1, Permissions: []model.SetPermission{{SetID: 1, UserID: 2}}},
				{ID: 2},
			},
			perms:     []model.SetPermission{{SetID: 1, UserID: 2}},
			wantCount: 1,
		},
		{
			name:   "non-admin with set permissions",
			userID: 3,
			user:   &model.User{ID: 3, IsAdmin: false},
			sets: []model.Set{
				{ID: 1, Permissions: []model.SetPermission{}},
				{ID: 2, Permissions: []model.SetPermission{{SetID: 2, UserID: 3}}},
			},
			perms:     nil,
			wantCount: 1,
		},
		{
			name:    "user error",
			userID:  1,
			userErr: errors.New("boom"),
			wantErr: true,
		},
		{
			name:    "sets error",
			userID:  1,
			user:    &model.User{ID: 1, IsAdmin: true},
			setsErr: errors.New("boom"),
			wantErr: true,
		},
		{
			name:     "perms error",
			userID:   2,
			user:     &model.User{ID: 2, IsAdmin: false},
			setsErr:  nil,
			permsErr: errors.New("boom"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return tt.user, tt.userErr
					},
				},
				SetRepo: repository.MockSetRepo{
					ListSetsFunc: func(ctx context.Context) ([]model.Set, error) {
						return tt.sets, tt.setsErr
					},
				},
				SetPermissionRepo: repository.MockSetPermissionRepo{
					ListPermissionsByUserFunc: func(ctx context.Context, userID int64) ([]model.SetPermission, error) {
						return tt.perms, tt.permsErr
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			sets, err := svc.ListSets(ctx, tt.userID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(sets) != tt.wantCount {
				t.Fatalf("expected %d sets, got %d", tt.wantCount, len(sets))
			}
		})
	}
}

func TestMediaService_GetMediaDetail(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		mediaID     int64
		userID      int64
		media       *model.Media
		mediaErr    error
		tags        []model.Tag
		tagsErr     error
		fav         bool
		favErr      error
		note        *model.Note
		noteErr     error
		progress    *model.PlaybackProgress
		progressErr error
		wantErr     bool
		wantNil     bool
	}{
		{
			name:     "ok",
			mediaID:  1,
			userID:   1,
			media:    &model.Media{ID: 1, SetID: 1, FileName: "a.mp4"},
			tags:     []model.Tag{{ID: 1, Name: "rock"}},
			fav:      true,
			note:     &model.Note{MediaID: 1, UserID: 1, Content: "hello"},
			progress: &model.PlaybackProgress{UserID: 1, MediaID: 1, PositionSeconds: 42},
		},
		{
			name:    "not found",
			mediaID: 2,
			media:   nil,
			wantErr: true,
		},
		{
			name:     "media error",
			mediaID:  3,
			mediaErr: errors.New("boom"),
			wantErr:  true,
		},
		{
			name:    "tags error",
			mediaID: 1,
			media:   &model.Media{ID: 1, SetID: 1},
			tagsErr: errors.New("boom"),
			wantErr: true,
		},
		{
			name:    "favorite error",
			mediaID: 1,
			media:   &model.Media{ID: 1, SetID: 1},
			favErr:  errors.New("boom"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return tt.media, tt.mediaErr
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return &model.Set{ID: id}, nil
					},
				},
				TagRepo: repository.MockTagRepo{
					ListTagsByMediaFunc: func(ctx context.Context, mediaID int64) ([]model.Tag, error) {
						return tt.tags, tt.tagsErr
					},
				},
				FavoriteRepo: repository.MockFavoriteRepo{
					IsFavoriteFunc: func(ctx context.Context, userID, mediaID int64) (bool, error) {
						return tt.fav, tt.favErr
					},
				},
				NoteRepo: repository.MockNoteRepo{
					GetNoteFunc: func(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
						return tt.note, tt.noteErr
					},
				},
				PlaybackProgressRepo: repository.MockPlaybackProgressRepo{
					GetProgressFunc: func(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
						return tt.progress, tt.progressErr
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			detail, err := svc.GetMediaDetail(ctx, tt.mediaID, tt.userID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if detail != nil {
					t.Fatal("expected nil detail")
				}
				return
			}
			if detail == nil {
				t.Fatal("expected detail, got nil")
			}
			if detail.Media.ID != tt.mediaID {
				t.Fatalf("unexpected media id %d", detail.Media.ID)
			}
			if tt.progress != nil {
				if detail.Progress == nil {
					t.Fatal("expected progress in detail")
				}
				if detail.Progress.PositionSeconds != tt.progress.PositionSeconds {
					t.Fatalf("expected position %v, got %v", tt.progress.PositionSeconds, detail.Progress.PositionSeconds)
				}
				if detail.ResumeFrom() != tt.progress.PositionSeconds {
					t.Fatalf("expected ResumeFrom %v, got %v", tt.progress.PositionSeconds, detail.ResumeFrom())
				}
			} else if detail.Progress != nil {
				t.Fatal("unexpected progress in detail")
			}
		})
	}
}

func TestMediaService_StreamMedia_Access(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	tests := []struct {
		name    string
		mediaID int64
		userID  int64
		media   *model.Media
		user    *model.User
		set     *model.Set
		perm    *model.SetPermission
		wantErr error
	}{
		{
			name:    "admin access",
			mediaID: 1,
			userID:  1,
			media:   &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4", FileSizeBytes: 100},
			user:    &model.User{ID: 1, IsAdmin: true},
		},
		{
			name:    "viewer via explicit permission",
			mediaID: 1,
			userID:  2,
			media:   &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4"},
			user:    &model.User{ID: 2, IsAdmin: false},
			set:     &model.Set{ID: 1},
			perm:    &model.SetPermission{SetID: 1, UserID: 2, Role: model.RoleViewer},
		},
		{
			name:    "owner via set permissions",
			mediaID: 1,
			userID:  3,
			media:   &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4"},
			user:    &model.User{ID: 3, IsAdmin: false},
			set:     &model.Set{ID: 1, Permissions: []model.SetPermission{{SetID: 1, UserID: 3, Role: model.RoleOwner}}},
		},
		{
			name:    "unauthorized user",
			mediaID: 1,
			userID:  4,
			media:   &model.Media{ID: 1, SetID: 1},
			user:    &model.User{ID: 4, IsAdmin: false},
			set:     &model.Set{ID: 1, Permissions: []model.SetPermission{}},
			wantErr: ErrForbidden,
		},
		{
			name:    "unauthenticated user",
			mediaID: 1,
			userID:  0,
			media:   &model.Media{ID: 1, SetID: 1},
			set:     &model.Set{ID: 1},
			wantErr: ErrForbidden,
		},
		{
			name:    "media not found",
			mediaID: 99,
			userID:  1,
			media:   nil,
			wantErr: ErrNotFound,
		},
		{
			name:    "soft-deleted media",
			mediaID: 1,
			userID:  1,
			media:   &model.Media{ID: 1, SetID: 1, DeletedAt: &now, AbsPath: "/tmp/a.mp4", FileName: "a.mp4"},
			user:    &model.User{ID: 1, IsAdmin: true},
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						if id == tt.mediaID {
							return tt.media, nil
						}
						return nil, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return tt.user, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return tt.set, nil
					},
				},
				SetPermissionRepo: repository.MockSetPermissionRepo{
					GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
						return tt.perm, nil
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			res, err := svc.StreamMedia(ctx, tt.mediaID, tt.userID)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("expected result, got nil")
			}
		})
	}
}

func TestMediaService_StreamMedia(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4", FileSizeBytes: 100}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: 1, IsAdmin: true}, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	res, err := svc.StreamMedia(ctx, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.FileName != "a.mp4" {
		t.Fatalf("unexpected filename %q", res.FileName)
	}
}

func TestMediaService_DownloadMedia_Access(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	tests := []struct {
		name    string
		mediaID int64
		userID  int64
		media   *model.Media
		user    *model.User
		set     *model.Set
		perm    *model.SetPermission
		wantErr error
	}{
		{
			name:    "admin access",
			mediaID: 1,
			userID:  1,
			media:   &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4", FileSizeBytes: 100},
			user:    &model.User{ID: 1, IsAdmin: true},
		},
		{
			name:    "viewer via explicit permission",
			mediaID: 1,
			userID:  2,
			media:   &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4"},
			user:    &model.User{ID: 2, IsAdmin: false},
			set:     &model.Set{ID: 1},
			perm:    &model.SetPermission{SetID: 1, UserID: 2, Role: model.RoleViewer},
		},
		{
			name:    "unauthorized user",
			mediaID: 1,
			userID:  4,
			media:   &model.Media{ID: 1, SetID: 1},
			user:    &model.User{ID: 4, IsAdmin: false},
			set:     &model.Set{ID: 1, Permissions: []model.SetPermission{}},
			wantErr: ErrForbidden,
		},
		{
			name:    "soft-deleted media",
			mediaID: 1,
			userID:  1,
			media:   &model.Media{ID: 1, SetID: 1, DeletedAt: &now, AbsPath: "/tmp/a.mp4", FileName: "a.mp4"},
			user:    &model.User{ID: 1, IsAdmin: true},
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						if id == tt.mediaID {
							return tt.media, nil
						}
						return nil, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return tt.user, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return tt.set, nil
					},
				},
				SetPermissionRepo: repository.MockSetPermissionRepo{
					GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
						return tt.perm, nil
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			res, err := svc.DownloadMedia(ctx, tt.mediaID, tt.userID)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("expected result, got nil")
			}
		})
	}
}

func TestMediaService_ToggleFavorite(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
		SetRepo: repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				return &model.Set{ID: id}, nil
			},
		},
		FavoriteRepo: repository.MockFavoriteRepo{
			ToggleFavoriteFunc: func(ctx context.Context, userID, mediaID int64) (bool, error) {
				return true, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	fav, err := svc.ToggleFavorite(ctx, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fav {
		t.Fatal("expected favorite true")
	}
}

func TestMediaService_AssignTag(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		tagExists bool
		createErr error
		assignErr error
		wantErr   bool
	}{
		{
			name:      "existing tag",
			tagExists: true,
		},
		{
			name:      "new tag",
			tagExists: false,
		},
		{
			name:      "create error",
			tagExists: false,
			createErr: errors.New("boom"),
			wantErr:   true,
		},
		{
			name:      "assign error",
			tagExists: true,
			assignErr: errors.New("boom"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var created bool
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return &model.Media{ID: 1, SetID: 1}, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return &model.Set{ID: id}, nil
					},
				},
				TagRepo: repository.MockTagRepo{
					GetTagByNameFunc: func(ctx context.Context, name string) (*model.Tag, error) {
						if tt.tagExists {
							return &model.Tag{ID: 1, Name: name}, nil
						}
						return nil, nil
					},
					CreateTagFunc: func(ctx context.Context, name string) (int64, error) {
						created = true
						return 2, tt.createErr
					},
					AssignTagFunc: func(ctx context.Context, mediaID, tagID int64) error {
						return tt.assignErr
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			err := svc.AssignTag(ctx, 1, 1, "rock")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.tagExists && !created {
				t.Fatal("expected tag creation")
			}
		})
	}
}

func TestMediaService_RemoveTag(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		tagExists bool
		removeErr error
		wantErr   bool
	}{
		{
			name:      "ok",
			tagExists: true,
		},
		{
			name:      "tag not found",
			tagExists: false,
			wantErr:   true,
		},
		{
			name:      "remove error",
			tagExists: true,
			removeErr: errors.New("boom"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return &model.Media{ID: 1, SetID: 1}, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return &model.Set{ID: id}, nil
					},
				},
				TagRepo: repository.MockTagRepo{
					GetTagByNameFunc: func(ctx context.Context, name string) (*model.Tag, error) {
						if tt.tagExists {
							return &model.Tag{ID: 1, Name: name}, nil
						}
						return nil, nil
					},
					RemoveTagFunc: func(ctx context.Context, mediaID, tagID int64) error {
						return tt.removeErr
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			err := svc.RemoveTag(ctx, 1, 1, "rock")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestMediaService_SoftDeleteMedia(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1}, nil
			},
			SoftDeleteMediaFunc: func(ctx context.Context, id int64) error {
				return nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: 1, IsAdmin: true}, nil
			},
		},
		SetRepo: repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				return &model.Set{ID: id}, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	if err := svc.SoftDeleteMedia(ctx, 1, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMediaService_RestoreMedia(t *testing.T) {
	ctx := context.Background()
	var called bool
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1}, nil
			},
			RestoreMediaFunc: func(ctx context.Context, id int64) error {
				called = true
				return nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: 1, IsAdmin: true}, nil
			},
		},
		SetRepo: repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				return &model.Set{ID: id}, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	if err := svc.RestoreMedia(ctx, 1, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected restore called")
	}
}

func TestMediaService_UploadMedia(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		setExists bool
		setErr    error
		createErr error
		filename  string
		wantErr   bool
	}{
		{
			name:      "ok",
			setExists: true,
			filename:  "song.mp3",
		},
		{
			name:      "duplicate filename",
			setExists: true,
			filename:  "song.mp3",
		},
		{
			name:      "set not found",
			setExists: false,
			wantErr:   true,
		},
		{
			name:      "set error",
			setExists: true,
			setErr:    errors.New("boom"),
			wantErr:   true,
		},
		{
			name:      "path traversal sanitized",
			setExists: true,
			filename:  "../../etc/passwd.mp3",
			wantErr:   false,
		},
		{
			name:      "path traversal dotdot rejected",
			setExists: true,
			filename:  "..",
			wantErr:   true,
		},
		{
			name:      "unsupported extension",
			setExists: true,
			filename:  "document.txt",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						if !tt.setExists {
							return nil, nil
						}
						return &model.Set{ID: 1, RootPath: "music"}, tt.setErr
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetPermissionRepo: repository.MockSetPermissionRepo{
					GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
						return &model.SetPermission{SetID: setID, UserID: userID, Role: model.RoleOwner}, nil
					},
				},
				MediaRepo: repository.MockMediaRepo{
					CreateMediaFunc: func(ctx context.Context, media *model.Media) (int64, error) {
						return 1, tt.createErr
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), tmpDir, nil, nil)
			data := strings.NewReader("hello world")
			media, err := svc.UploadMedia(ctx, 1, 1, tt.filename, data, 11)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if media == nil {
				t.Fatal("expected media, got nil")
			}
		})
	}
}

func TestMediaService_CreateShare(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: 1, IsAdmin: true}, nil
			},
		},
		ShareRepo: repository.MockShareRepo{
			CreateShareFunc: func(ctx context.Context, share *model.Share) error {
				return nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	share, err := svc.CreateShare(ctx, 1, 1, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if share == nil {
		t.Fatal("expected share, got nil")
	}
	if share.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestMediaService_ValidateShareToken(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	tests := []struct {
		name      string
		share     *model.Share
		wantValid bool
	}{
		{
			name:      "valid",
			share:     &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)},
			wantValid: true,
		},
		{
			name:      "expired",
			share:     &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(-time.Hour)},
			wantValid: false,
		},
		{
			name:      "max uses reached",
			share:     &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour), MaxUses: intPtr(5), UsedCount: 5},
			wantValid: false,
		},
		{
			name:      "not found",
			share:     nil,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				ShareRepo: repository.MockShareRepo{
					GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
						return tt.share, nil
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			res, err := svc.ValidateShareToken(ctx, "abc")
			if tt.wantValid {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if res == nil {
					t.Fatal("expected valid share")
				}
			} else {
				if err == nil {
					t.Fatal("expected error for invalid share")
				}
				if res != nil {
					t.Fatal("expected nil share")
				}
			}
		})
	}
}

func TestMediaService_StreamSharedMedia(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	tests := []struct {
		name    string
		mediaID int64
		media   *model.Media
		share   *model.Share
		wantErr bool
	}{
		{
			name:    "ok",
			mediaID: 1,
			media:   &model.Media{ID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4"},
			share:   &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return tt.media, nil
					},
				},
				ShareRepo: repository.MockShareRepo{
					GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
						return tt.share, nil
					},
					UseShareFunc: func(ctx context.Context, token string) error {
						return nil
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			res, err := svc.StreamSharedMedia(ctx, "abc")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.FileName != "a.mp4" {
				t.Fatalf("unexpected filename %q", res.FileName)
			}
		})
	}
}

func TestMediaService_UploadMedia_ProbeAndThumbnail(t *testing.T) {
	ctx := context.Background()
	makeStore := func() *repository.MockStore {
		return &repository.MockStore{
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: 1, RootPath: "music"}, nil
				},
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: true}, nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				CreateMediaFunc: func(ctx context.Context, media *model.Media) (int64, error) {
					return 42, nil
				},
				UpdateMediaFunc: func(ctx context.Context, media *model.Media) error {
					return nil
				},
				HardDeleteMediaFunc: func(ctx context.Context, id int64) error {
					return nil
				},
			},
		}
	}

	t.Run("success video with probe and thumbnail", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := makeStore()
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 120, Codec: "h264", Resolution: "1920x1080", Bitrate: 5000}, nil
		}}
		thumbGen := &mockThumbGenerator{GenerateFunc: func(ctx context.Context, inputPath, outputPath string, duration float64) error {
			_ = os.WriteFile(outputPath, []byte("thumb"), 0o644)
			return nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, thumbGen, prober)
		data := strings.NewReader("fake video data")
		media, err := svc.UploadMedia(ctx, 1, 1, "video.mp4", data, 16)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if media.ID != 42 {
			t.Fatalf("unexpected id %d", media.ID)
		}
		if media.Duration != 120 {
			t.Fatalf("unexpected duration %f", media.Duration)
		}
		if media.Codec != "h264" {
			t.Fatalf("unexpected codec %s", media.Codec)
		}
		if media.ThumbnailPath == "" {
			t.Fatal("expected thumbnail path")
		}
	})

	t.Run("probe failure cleans up", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := makeStore()
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return nil, errors.New("probe failed")
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, nil, prober)
		data := strings.NewReader("fake")
		_, err := svc.UploadMedia(ctx, 1, 1, "song.mp3", data, 4)
		if err == nil {
			t.Fatal("expected error")
		}
		// verify temp file is removed
		files, _ := os.ReadDir(filepath.Join(tmpDir, "music"))
		for _, e := range files {
			if e.Name() != ".thumbnails" {
				t.Fatalf("expected cleanup, found %s", e.Name())
			}
		}
	})

	t.Run("thumbnail failure cleans up", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := makeStore()
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 120}, nil
		}}
		thumbGen := &mockThumbGenerator{GenerateFunc: func(ctx context.Context, inputPath, outputPath string, duration float64) error {
			return errors.New("thumbnail failed")
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, thumbGen, prober)
		data := strings.NewReader("fake video data")
		_, err := svc.UploadMedia(ctx, 1, 1, "video.mp4", data, 16)
		if err == nil {
			t.Fatal("expected error")
		}
		files, _ := os.ReadDir(filepath.Join(tmpDir, "music"))
		for _, e := range files {
			if e.Name() != ".thumbnails" {
				t.Fatalf("expected cleanup, found %s", e.Name())
			}
		}
	})

	t.Run("audio skips thumbnail but probes", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := makeStore()
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 300, Bitrate: 320}, nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, nil, prober)
		data := strings.NewReader("fake audio data")
		media, err := svc.UploadMedia(ctx, 1, 1, "song.mp3", data, 16)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if media.ThumbnailPath != "" {
			t.Fatal("expected no thumbnail path for audio")
		}
		if media.Duration != 300 {
			t.Fatalf("unexpected duration %f", media.Duration)
		}
	})

	t.Run("update media failure cleans up", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := makeStore()
		store.MediaRepo.UpdateMediaFunc = func(ctx context.Context, media *model.Media) error {
			return errors.New("update failed")
		}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 120}, nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, nil, prober)
		data := strings.NewReader("fake audio data")
		_, err := svc.UploadMedia(ctx, 1, 1, "song.mp3", data, 16)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMediaService_Notes(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		method    string
		wantErr   bool
		noteErr   error
		upsertErr error
		deleteErr error
	}{
		{name: "get note", method: "get"},
		{name: "get note error", method: "get", wantErr: true, noteErr: errors.New("boom")},
		{name: "upsert note", method: "upsert"},
		{name: "upsert error", method: "upsert", wantErr: true, upsertErr: errors.New("boom")},
		{name: "delete note", method: "delete"},
		{name: "delete error", method: "delete", wantErr: true, deleteErr: errors.New("boom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return &model.Media{ID: 1, SetID: 1}, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return &model.Set{ID: id}, nil
					},
				},
				NoteRepo: repository.MockNoteRepo{
					GetNoteFunc: func(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
						return &model.Note{MediaID: mediaID, UserID: userID, Content: "hello"}, tt.noteErr
					},
					UpsertNoteFunc: func(ctx context.Context, note *model.Note) error {
						return tt.upsertErr
					},
					DeleteNoteFunc: func(ctx context.Context, mediaID, userID int64) error {
						return tt.deleteErr
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			var err error
			switch tt.method {
			case "get":
				_, err = svc.GetNote(ctx, 1, 1)
			case "upsert":
				err = svc.UpsertNote(ctx, &model.Note{MediaID: 1, UserID: 1, Content: "hi"})
			case "delete":
				err = svc.DeleteNote(ctx, 1, 1)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}

// mockThumbGenerator is a test fake for thumb.Generator.
type mockThumbGenerator struct {
	GenerateFunc func(ctx context.Context, inputPath, outputPath string, duration float64) error
}

func (m *mockThumbGenerator) Generate(ctx context.Context, inputPath, outputPath string, duration float64) error {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, inputPath, outputPath, duration)
	}
	return nil
}

// mockProber is a test fake for probe.Prober.
type mockProber struct {
	ProbeFunc func(ctx context.Context, path string) (*model.Metadata, error)
}

func (m *mockProber) Probe(ctx context.Context, path string) (*model.Metadata, error) {
	if m.ProbeFunc != nil {
		return m.ProbeFunc(ctx, path)
	}
	return &model.Metadata{}, nil
}

func TestMediaService_ViewerCannotMutate(t *testing.T) {
	ctx := context.Background()

	makeViewerStore := func(mediaID, setID int64) *repository.MockStore {
		return &repository.MockStore{
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					if id == mediaID {
						return &model.Media{ID: mediaID, SetID: setID}, nil
					}
					return nil, nil
				},
				SoftDeleteMediaFunc: func(ctx context.Context, id int64) error { return nil },
				RestoreMediaFunc:    func(ctx context.Context, id int64) error { return nil },
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: id, Permissions: []model.SetPermission{{SetID: setID, UserID: 2, Role: model.RoleViewer}}}, nil
				},
			},
		}
	}

	t.Run("viewer cannot soft delete", func(t *testing.T) {
		store := makeViewerStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.SoftDeleteMedia(ctx, 1, 2)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("viewer cannot restore", func(t *testing.T) {
		store := makeViewerStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RestoreMedia(ctx, 1, 2)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("viewer cannot upload", func(t *testing.T) {
		store := &repository.MockStore{
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: 1, RootPath: "music", Permissions: []model.SetPermission{{SetID: 1, UserID: 2, Role: model.RoleViewer}}}, nil
				},
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), t.TempDir(), nil, nil)
		_, err := svc.UploadMedia(ctx, 1, 2, "song.mp3", strings.NewReader("data"), 4)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("viewer cannot regenerate thumbnail", func(t *testing.T) {
		store := makeViewerStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateThumbnail(ctx, 1, 2)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("viewer cannot regenerate set cover", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: 1, Permissions: []model.SetPermission{{SetID: 1, UserID: 2, Role: model.RoleViewer}}}, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateSetCover(ctx, 1, "", 2)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})
}

func TestMediaService_UnauthorizedAccessDenied(t *testing.T) {
	ctx := context.Background()

	makeUnauthorizedStore := func(mediaID, setID int64) *repository.MockStore {
		return &repository.MockStore{
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					if id == mediaID {
						return &model.Media{ID: mediaID, SetID: setID}, nil
					}
					return nil, nil
				},
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: id, Permissions: []model.SetPermission{}}, nil
				},
			},
			SetPermissionRepo: repository.MockSetPermissionRepo{
				GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
					return nil, nil
				},
			},
		}
	}

	t.Run("unauthorized cannot get detail", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		_, err := svc.GetMediaDetail(ctx, 1, 9)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot list media in set", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetPermissionRepo: repository.MockSetPermissionRepo{
				ListPermissionsByUserFunc: func(ctx context.Context, userID int64) ([]model.SetPermission, error) {
					return nil, nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				ListMediaFunc: func(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
					return nil, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		res, err := svc.ListMedia(ctx, 9, repository.MediaFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 0 {
			t.Fatalf("expected empty list, got %d", len(res))
		}
	})

	t.Run("unauthorized cannot favorite", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		_, err := svc.ToggleFavorite(ctx, 9, 1)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot assign tag", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.AssignTag(ctx, 1, 9, "rock")
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot remove tag", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RemoveTag(ctx, 1, 9, "rock")
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot get note", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		_, err := svc.GetNote(ctx, 1, 9)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot upsert note", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.UpsertNote(ctx, &model.Note{MediaID: 1, UserID: 9, Content: "hello"})
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot delete note", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.DeleteNote(ctx, 1, 9)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot create share", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		_, err := svc.CreateShare(ctx, 9, 1, time.Now().Add(time.Hour))
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unauthorized cannot list shares", func(t *testing.T) {
		store := makeUnauthorizedStore(1, 1)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		_, err := svc.ListShares(ctx, 1, 9)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("owner can soft delete", func(t *testing.T) {
		store := &repository.MockStore{
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					return &model.Media{ID: 1, SetID: 1}, nil
				},
				SoftDeleteMediaFunc: func(ctx context.Context, id int64) error { return nil },
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: 1, Permissions: []model.SetPermission{{SetID: 1, UserID: 2, Role: model.RoleOwner}}}, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.SoftDeleteMedia(ctx, 1, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("owner can restore", func(t *testing.T) {
		store := &repository.MockStore{
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					return &model.Media{ID: 1, SetID: 1}, nil
				},
				RestoreMediaFunc: func(ctx context.Context, id int64) error { return nil },
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: 1, Permissions: []model.SetPermission{{SetID: 1, UserID: 2, Role: model.RoleOwner}}}, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RestoreMedia(ctx, 1, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("owner can upload", func(t *testing.T) {
		tmpDir := t.TempDir()
		store := &repository.MockStore{
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: 1, RootPath: "music", Permissions: []model.SetPermission{{SetID: 1, UserID: 2, Role: model.RoleOwner}}}, nil
				},
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				CreateMediaFunc: func(ctx context.Context, media *model.Media) (int64, error) {
					return 1, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), tmpDir, nil, nil)
		media, err := svc.UploadMedia(ctx, 1, 2, "song.mp3", strings.NewReader("data"), 4)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if media == nil {
			t.Fatal("expected media")
		}
	})
}

func TestMediaService_ListMedia_AdminAndUserFiltering(t *testing.T) {
	ctx := context.Background()

	t.Run("admin sees all", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: true}, nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				ListMediaFunc: func(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
					return []model.Media{{ID: 1}, {ID: 2}}, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		res, err := svc.ListMedia(ctx, 1, repository.MediaFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 2 {
			t.Fatalf("expected 2, got %d", len(res))
		}
	})

	t.Run("user sees only allowed sets", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetPermissionRepo: repository.MockSetPermissionRepo{
				ListPermissionsByUserFunc: func(ctx context.Context, userID int64) ([]model.SetPermission, error) {
					return []model.SetPermission{{SetID: 3, UserID: userID, Role: model.RoleViewer}}, nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				ListMediaFunc: func(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
					if len(filter.AllowedSetIDs) == 1 && filter.AllowedSetIDs[0] == 3 {
						return []model.Media{{ID: 5}}, nil
					}
					return nil, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		res, err := svc.ListMedia(ctx, 2, repository.MediaFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 1 || res[0].ID != 5 {
			t.Fatalf("unexpected result: %+v", res)
		}
	})
}

func TestMediaService_RegenerateThumbnail(t *testing.T) {
	ctx := context.Background()
	makeStore := func(media *model.Media) *repository.MockStore {
		return &repository.MockStore{
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					return media, nil
				},
				UpdateMediaFunc: func(ctx context.Context, m *model.Media) error {
					return nil
				},
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					if id == 1 {
						return &model.User{ID: 1, IsAdmin: true}, nil
					}
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: id}, nil
				},
			},
			SetPermissionRepo: repository.MockSetPermissionRepo{
				GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
					return nil, nil
				},
			},
		}
	}

	t.Run("admin can regenerate thumbnail", func(t *testing.T) {
		tmpDir := t.TempDir()
		mediaPath := filepath.Join(tmpDir, "video.mp4")
		_ = os.WriteFile(mediaPath, []byte("fake"), 0o644)
		media := &model.Media{ID: 1, SetID: 1, AbsPath: mediaPath, Type: model.MediaTypeVideo}
		store := makeStore(media)
		thumbGen := &mockThumbGenerator{}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 60}, nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, thumbGen, prober)
		if err := svc.RegenerateThumbnail(ctx, 1, 1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("owner can regenerate thumbnail", func(t *testing.T) {
		tmpDir := t.TempDir()
		mediaPath := filepath.Join(tmpDir, "video.mp4")
		_ = os.WriteFile(mediaPath, []byte("fake"), 0o644)
		media := &model.Media{ID: 1, SetID: 1, AbsPath: mediaPath, Type: model.MediaTypeVideo}
		store := makeStore(media)
		store.SetRepo = repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				return &model.Set{ID: id, Permissions: []model.SetPermission{{SetID: id, UserID: 2, Role: model.RoleOwner}}}, nil
			},
		}
		thumbGen := &mockThumbGenerator{}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 60}, nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, thumbGen, prober)
		if err := svc.RegenerateThumbnail(ctx, 1, 2); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("viewer cannot regenerate thumbnail", func(t *testing.T) {
		media := &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/video.mp4", Type: model.MediaTypeVideo}
		store := makeStore(media)
		store.SetRepo = repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				return &model.Set{ID: id, Permissions: []model.SetPermission{{SetID: id, UserID: 2, Role: model.RoleViewer}}}, nil
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateThumbnail(ctx, 1, 2)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		store := makeStore(nil)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateThumbnail(ctx, 1, 1)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("audio file rejected", func(t *testing.T) {
		media := &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/song.mp3", Type: model.MediaTypeAudio}
		store := makeStore(media)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateThumbnail(ctx, 1, 1)
		if err == nil {
			t.Fatal("expected error for audio file")
		}
	})

	t.Run("probe failure", func(t *testing.T) {
		media := &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/video.mp4", Type: model.MediaTypeVideo}
		store := makeStore(media)
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return nil, errors.New("probe err")
		}}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, prober)
		err := svc.RegenerateThumbnail(ctx, 1, 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("thumb generation failure", func(t *testing.T) {
		media := &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/video.mp4", Type: model.MediaTypeVideo}
		store := makeStore(media)
		thumbGen := &mockThumbGenerator{GenerateFunc: func(ctx context.Context, inputPath, outputPath string, duration float64) error {
			return errors.New("thumb err")
		}}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 60}, nil
		}}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", thumbGen, prober)
		err := svc.RegenerateThumbnail(ctx, 1, 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("update media failure", func(t *testing.T) {
		media := &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/video.mp4", Type: model.MediaTypeVideo}
		store := makeStore(media)
		store.MediaRepo.UpdateMediaFunc = func(ctx context.Context, m *model.Media) error {
			return errors.New("update err")
		}
		thumbGen := &mockThumbGenerator{}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 60}, nil
		}}
		svc := NewMediaService(store, newMockClock(), "/tmp/media", thumbGen, prober)
		err := svc.RegenerateThumbnail(ctx, 1, 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMediaService_RegenerateSetCover(t *testing.T) {
	ctx := context.Background()
	makeStore := func(setID int64, media []model.Media, set *model.Set) *repository.MockStore {
		return &repository.MockStore{
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					if set != nil && set.ID == id {
						return set, nil
					}
					return nil, nil
				},
				UpdateSetFunc: func(ctx context.Context, s *model.Set) error {
					return nil
				},
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					if id == 1 {
						return &model.User{ID: 1, IsAdmin: true}, nil
					}
					return &model.User{ID: id, IsAdmin: false}, nil
				},
			},
			SetPermissionRepo: repository.MockSetPermissionRepo{
				GetPermissionFunc: func(ctx context.Context, sid, uid int64) (*model.SetPermission, error) {
					return nil, nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				ListMediaFunc: func(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
					return media, nil
				},
			},
		}
	}

	t.Run("admin can regenerate cover", func(t *testing.T) {
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "video.mp4")
		_ = os.WriteFile(videoPath, []byte("fake"), 0o644)
		set := &model.Set{ID: 1, RootPath: "music"}
		media := []model.Media{{ID: 1, SetID: 1, AbsPath: videoPath, Type: model.MediaTypeVideo}}
		store := makeStore(1, media, set)
		thumbGen := &mockThumbGenerator{}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 60}, nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, thumbGen, prober)
		if err := svc.RegenerateSetCover(ctx, 1, "", 1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("owner can regenerate cover", func(t *testing.T) {
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "video.mp4")
		_ = os.WriteFile(videoPath, []byte("fake"), 0o644)
		set := &model.Set{ID: 1, RootPath: "music", Permissions: []model.SetPermission{{SetID: 1, UserID: 2, Role: model.RoleOwner}}}
		media := []model.Media{{ID: 1, SetID: 1, AbsPath: videoPath, Type: model.MediaTypeVideo}}
		store := makeStore(1, media, set)
		thumbGen := &mockThumbGenerator{}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 60}, nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, thumbGen, prober)
		if err := svc.RegenerateSetCover(ctx, 1, "", 2); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("viewer cannot regenerate cover", func(t *testing.T) {
		set := &model.Set{ID: 1, RootPath: "music", Permissions: []model.SetPermission{{SetID: 1, UserID: 2, Role: model.RoleViewer}}}
		store := makeStore(1, nil, set)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateSetCover(ctx, 1, "", 2)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("set not found", func(t *testing.T) {
		store := makeStore(1, nil, nil)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateSetCover(ctx, 1, "", 1)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("no video files", func(t *testing.T) {
		set := &model.Set{ID: 1, RootPath: "music"}
		media := []model.Media{{ID: 1, SetID: 1, AbsPath: "/tmp/song.mp3", Type: model.MediaTypeAudio}}
		store := makeStore(1, media, set)
		svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
		err := svc.RegenerateSetCover(ctx, 1, "", 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("probe failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "video.mp4")
		_ = os.WriteFile(videoPath, []byte("fake"), 0o644)
		set := &model.Set{ID: 1, RootPath: "music"}
		media := []model.Media{{ID: 1, SetID: 1, AbsPath: videoPath, Type: model.MediaTypeVideo}}
		store := makeStore(1, media, set)
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return nil, errors.New("probe err")
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, nil, prober)
		err := svc.RegenerateSetCover(ctx, 1, "", 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("thumb generation failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "video.mp4")
		_ = os.WriteFile(videoPath, []byte("fake"), 0o644)
		set := &model.Set{ID: 1, RootPath: "music"}
		media := []model.Media{{ID: 1, SetID: 1, AbsPath: videoPath, Type: model.MediaTypeVideo}}
		store := makeStore(1, media, set)
		thumbGen := &mockThumbGenerator{GenerateFunc: func(ctx context.Context, inputPath, outputPath string, duration float64) error {
			return errors.New("thumb err")
		}}
		prober := &mockProber{ProbeFunc: func(ctx context.Context, path string) (*model.Metadata, error) {
			return &model.Metadata{Duration: 60}, nil
		}}
		svc := NewMediaService(store, newMockClock(), tmpDir, thumbGen, prober)
		err := svc.RegenerateSetCover(ctx, 1, "", 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMediaService_FolderThumbnailFallback(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	thumbPath := filepath.Join(tmpDir, "thumb.jpg")
	if err := os.WriteFile(thumbPath, []byte("thumb"), 0o644); err != nil {
		t.Fatalf("write thumb: %v", err)
	}

	set := &model.Set{ID: 1, RootPath: "library"}
	media := []model.Media{
		{ID: 1, SetID: 1, RelPath: "Rock/a.mp4", ThumbnailPath: thumbPath, Type: model.MediaTypeVideo},
		{ID: 2, SetID: 1, RelPath: "Rock/b.mp4", Type: model.MediaTypeVideo},
	}
	store := &repository.MockStore{
		SetRepo: repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				if id == set.ID {
					return set, nil
				}
				return nil, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
		MediaRepo: repository.MockMediaRepo{
			ListMediaFunc: func(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
				return media, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), tmpDir, nil, nil)

	t.Run("browse marks folder as having thumbnail", func(t *testing.T) {
		res, err := svc.BrowseSet(ctx, 1, 1, "")
		if err != nil {
			t.Fatalf("browse set: %v", err)
		}
		if len(res.Folders) != 1 {
			t.Fatalf("expected one folder, got %d", len(res.Folders))
		}
		if res.Folders[0].Name != "Rock" || !res.Folders[0].HasCover {
			t.Fatalf("unexpected folder: %+v", res.Folders[0])
		}
	})

	t.Run("cover endpoint falls back to media thumbnail", func(t *testing.T) {
		fr, err := svc.GetSetCover(ctx, 1, "Rock", 1)
		if err != nil {
			t.Fatalf("get set cover: %v", err)
		}
		if fr.Path != thumbPath {
			t.Fatalf("expected fallback thumb %q, got %q", thumbPath, fr.Path)
		}
	})
}

func TestMediaService_GetThumbnail(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	thumbPath := filepath.Join(tmpDir, "thumb.jpg")
	_ = os.WriteFile(thumbPath, []byte("thumb"), 0o644)

	tests := []struct {
		name     string
		media    *model.Media
		wantPath string
		wantErr  bool
	}{
		{
			name:     "ok",
			media:    &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4", ThumbnailPath: thumbPath},
			wantPath: thumbPath,
		},
		{
			name:    "no thumbnail path",
			media:   &model.Media{ID: 1, SetID: 1, AbsPath: "/tmp/a.mp4", FileName: "a.mp4", ThumbnailPath: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return tt.media, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: 1, IsAdmin: true}, nil
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			res, err := svc.GetThumbnail(ctx, 1, 1)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Path != tt.wantPath {
				t.Fatalf("unexpected path %q", res.Path)
			}
		})
	}
}

func TestMediaService_RevokeShare(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	tests := []struct {
		name    string
		share   *model.Share
		media   *model.Media
		wantErr bool
	}{
		{
			name:  "ok",
			share: &model.Share{Token: "abc", MediaID: 1, CreatedBy: 1, ExpiresAt: now.Add(time.Hour)},
			media: &model.Media{ID: 1, SetID: 1},
		},
		{
			name:    "share not found",
			share:   nil,
			wantErr: true,
		},
		{
			name:    "access denied",
			share:   &model.Share{Token: "abc", MediaID: 1, CreatedBy: 1, ExpiresAt: now.Add(time.Hour)},
			media:   &model.Media{ID: 1, SetID: 1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				ShareRepo: repository.MockShareRepo{
					GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
						return tt.share, nil
					},
					DeleteShareFunc: func(ctx context.Context, token string) error {
						return nil
					},
				},
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return tt.media, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						if tt.name == "access denied" {
							return &model.User{ID: id, IsAdmin: false}, nil
						}
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return &model.Set{ID: id}, nil
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
			err := svc.RevokeShare(ctx, "abc", 1)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestMediaService_ListShares(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
		ShareRepo: repository.MockShareRepo{
			ListSharesByMediaFunc: func(ctx context.Context, mediaID int64) ([]model.Share, error) {
				return []model.Share{{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)}}, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	shares, err := svc.ListShares(ctx, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("expected 1 share, got %d", len(shares))
	}
}

func TestMediaService_StreamSharedMedia_MissingMedia(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	store := &repository.MockStore{
		ShareRepo: repository.MockShareRepo{
			GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
				return &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)}, nil
			},
			UseShareFunc: func(ctx context.Context, token string) error { return nil },
		},
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return nil, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	_, err := svc.StreamSharedMedia(ctx, "abc")
	if !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("expected ErrMediaNotFound, got %v", err)
	}
}

func TestMediaService_GuessMediaType_Edge(t *testing.T) {
	if got := guessMediaType("unknown.xyz"); got != model.MediaTypeVideo {
		t.Fatalf("expected video for unknown ext, got %v", got)
	}
}

func TestMediaService_CreateShare_StoreError(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: 1, IsAdmin: true}, nil
			},
		},
		ShareRepo: repository.MockShareRepo{
			CreateShareFunc: func(ctx context.Context, share *model.Share) error {
				return errors.New("db error")
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media", nil, nil)
	_, err := svc.CreateShare(ctx, 1, 1, now.Add(time.Hour))
	if err == nil {
		t.Fatal("expected error")
	}
}
