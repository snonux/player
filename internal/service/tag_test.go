package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestTagService_AssignTag(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		tagExists  bool
		createErr  error
		assignErr  error
		wantErr    bool
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
						return 2, tt.createErr
					},
					AssignTagFunc: func(ctx context.Context, mediaID, tagID int64) error {
						return tt.assignErr
					},
				},
			}
			svc := NewTagService(store, &accessHelper{store: store})
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
		})
	}
}
