package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestShareService_GetSharedThumbnail(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	tests := []struct {
		name    string
		share   *model.Share
		media   *model.Media
		wantErr bool
	}{
		{
			name:    "ok",
			share:   &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)},
			media:   &model.Media{ID: 1, ThumbnailPath: "/tmp/thumb.jpg"},
			wantErr: false,
		},
		{
			name:    "missing media",
			share:   &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)},
			media:   nil,
			wantErr: true,
		},
		{
			name:    "missing thumbnail",
			share:   &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)},
			media:   &model.Media{ID: 1, ThumbnailPath: ""},
			wantErr: true,
		},
		{
			name:    "expired token",
			share:   &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(-time.Hour)},
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
				},
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return tt.media, nil
					},
				},
			}
			svc := NewShareService(store, newMockClock(), &accessHelper{store: store})
			_, err := svc.GetSharedThumbnail(ctx, "abc")
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

func TestShareService_ListMyShares(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	t.Run("ok", func(t *testing.T) {
		store := &repository.MockStore{
			ShareRepo: repository.MockShareRepo{
				ListSharesByUserFunc: func(ctx context.Context, userID int64) ([]model.Share, error) {
					return []model.Share{{Token: "abc", MediaID: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}, nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					return &model.Media{ID: 1, FileName: "a.mp4", Type: model.MediaTypeVideo}, nil
				},
			},
		}
		svc := NewShareService(store, newMockClock(), &accessHelper{store: store})
		res, err := svc.ListMyShares(ctx, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 share, got %d", len(res))
		}
	})

	t.Run("store error", func(t *testing.T) {
		store := &repository.MockStore{
			ShareRepo: repository.MockShareRepo{
				ListSharesByUserFunc: func(ctx context.Context, userID int64) ([]model.Share, error) {
					return nil, errors.New("boom")
				},
			},
		}
		svc := NewShareService(store, newMockClock(), &accessHelper{store: store})
		_, err := svc.ListMyShares(ctx, 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
