package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestFavService_ToggleFavorite(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		storeErr error
		wantErr  bool
	}{
		{
			name: "ok",
		},
		{
			name:     "store error",
			storeErr: errors.New("boom"),
			wantErr:  true,
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
				FavoriteRepo: repository.MockFavoriteRepo{
					ToggleFavoriteFunc: func(ctx context.Context, userID, mediaID int64) (bool, error) {
						return true, tt.storeErr
					},
				},
			}
			svc := NewFavService(store, &accessHelper{store: store})
			_, err := svc.ToggleFavorite(ctx, 1, 1)
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
