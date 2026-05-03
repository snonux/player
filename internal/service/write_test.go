package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestWriteService_RestoreMedia(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		media    *model.Media
		storeErr error
		wantErr  bool
		wantCode error
	}{
		{
			name:  "ok",
			media: &model.Media{ID: 1, SetID: 1},
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
