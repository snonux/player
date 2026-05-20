package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestWriteService_RestoreMedia(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		media        *model.Media
		deletedMedia []model.Media
		deletedErr   error
		storeErr     error
		wantErr      bool
		wantCode     error
	}{
		{
			name:  "ok",
			media: &model.Media{ID: 1, SetID: 1},
		},
		{
			name:         "soft deleted ok",
			deletedMedia: []model.Media{{ID: 1, SetID: 1}},
		},
		{
			name:       "deleted lookup error",
			deletedErr: errors.New("deleted lookup failed"),
			wantErr:    true,
		},
		{
			name:     "store error",
			media:    &model.Media{ID: 1, SetID: 1},
			storeErr: errors.New("boom"),
			wantErr:  true,
		},
		{
			name:     "not found",
			media:    nil,
			wantErr:  true,
			wantCode: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return tt.media, nil
					},
					ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
						return tt.deletedMedia, tt.deletedErr
					},
					RestoreMediaFunc: func(ctx context.Context, id int64) error {
						return tt.storeErr
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
				SetPermissionRepo: repository.MockSetPermissionRepo{
					GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
						return nil, nil
					},
				},
			}
			svc := NewWriteService(store, clock.RealClock{}, "/tmp/media", nil, nil, &accessHelper{store: store})
			err := svc.RestoreMedia(ctx, 1, 1)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.wantCode != nil {
					if !errors.Is(err, tt.wantCode) {
						t.Fatalf("expected error %v, got %v", tt.wantCode, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCopyFile_Success verifies that copyFile creates the destination with
// the exact contents of the source on a clean run (and returns no error).
func TestCopyFile_Success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	payload := []byte("hello copyFile world")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("dst contents mismatch: got %q want %q", got, payload)
	}
}

// TestCopyFile_MissingSource verifies the source-open error path propagates
// cleanly (no destination should be created or leaked).
func TestCopyFile_MissingSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "does-not-exist.bin")
	dst := filepath.Join(dir, "dst.bin")
	if err := copyFile(src, dst); err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected dst to not exist, stat err=%v", err)
	}
}
