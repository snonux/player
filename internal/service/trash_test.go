package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestTrashService_ListTrash(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		media    []model.Media
		storeErr error
		wantErr  bool
		wantLen  int
	}{
		{
			name:    "ok empty",
			media:   []model.Media{},
			wantLen: 0,
		},
		{
			name:    "ok with items",
			media:   []model.Media{{ID: 1, FileName: "a.mp4"}},
			wantLen: 1,
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
					ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
						return tt.media, tt.storeErr
					},
				},
			}
			svc := NewTrashService(store)
			res, err := svc.ListTrash(ctx)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res) != tt.wantLen {
				t.Fatalf("expected %d items, got %d", tt.wantLen, len(res))
			}
		})
	}
}
