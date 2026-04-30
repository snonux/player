package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/play/internal/clock"
	"codeberg.org/snonux/play/internal/model"
	"codeberg.org/snonux/play/internal/repository"
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			media:    &model.Media{ID: 1, FileName: "a.mp4"},
			tags:     []model.Tag{{ID: 1, Name: "rock"}},
			fav:      true,
			note:     &model.Note{MediaID: 1, UserID: 1, Content: "hello"},
			progress: &model.PlaybackProgress{UserID: 1, MediaID: 1, PositionSeconds: 42},
		},
		{
			name:    "not found",
			mediaID: 2,
			media:   nil,
			wantNil: true,
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
			media:   &model.Media{ID: 1},
			tagsErr: errors.New("boom"),
			wantErr: true,
		},
		{
			name:    "favorite error",
			mediaID: 1,
			media:   &model.Media{ID: 1},
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
	svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
		FavoriteRepo: repository.MockFavoriteRepo{
			ToggleFavoriteFunc: func(ctx context.Context, userID, mediaID int64) (bool, error) {
				return true, nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media")
	if err := svc.SoftDeleteMedia(ctx, 1, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMediaService_RestoreMedia(t *testing.T) {
	ctx := context.Background()
	var called bool
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			RestoreMediaFunc: func(ctx context.Context, id int64) error {
				called = true
				return nil
			},
		},
	}
	svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			filename:  "../../etc/passwd",
			wantErr:   false,
		},
		{
			name:      "path traversal dotdot rejected",
			setExists: true,
			filename:  "..",
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
				MediaRepo: repository.MockMediaRepo{
					CreateMediaFunc: func(ctx context.Context, media *model.Media) (int64, error) {
						return 1, tt.createErr
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), tmpDir)
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
	svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
			res, err := svc.ValidateShareToken(ctx, "abc")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantValid {
				if res == nil {
					t.Fatal("expected valid share")
				}
			} else {
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
		{
			name:    "media not found",
			mediaID: 2,
			media:   nil,
			share:   &model.Share{Token: "abc", MediaID: 2, ExpiresAt: now.Add(time.Hour)},
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
				ShareRepo: repository.MockShareRepo{
					GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
						return tt.share, nil
					},
					UseShareFunc: func(ctx context.Context, token string) error {
						return nil
					},
				},
			}
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
			svc := NewMediaService(store, newMockClock(), "/tmp/media")
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
