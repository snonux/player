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

func TestShareService_GetSharedMedia_ThumbnailURL(t *testing.T) {
	ctx := context.Background()
	now := newMockClock().T

	tests := []struct {
		name         string
		thumbnail    string
		wantHasThumb bool
		wantThumbURL string
	}{
		{
			name:         "with thumbnail",
			thumbnail:    "/tmp/thumb.jpg",
			wantHasThumb: true,
			wantThumbURL: "/s/abc/thumbnail",
		},
		{
			name:         "without thumbnail",
			thumbnail:    "",
			wantHasThumb: false,
			wantThumbURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				ShareRepo: repository.MockShareRepo{
					GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
						return &model.Share{Token: "abc", MediaID: 1, ExpiresAt: now.Add(time.Hour)}, nil
					},
				},
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return &model.Media{ID: 1, ThumbnailPath: tt.thumbnail}, nil
					},
				},
			}
			svc := NewShareService(store, newMockClock(), &accessHelper{store: store})
			got, err := svc.GetSharedMedia(ctx, "abc")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.HasThumb != tt.wantHasThumb {
				t.Fatalf("HasThumb = %v, want %v", got.HasThumb, tt.wantHasThumb)
			}
			if got.ThumbURL != tt.wantThumbURL {
				t.Fatalf("ThumbURL = %q, want %q", got.ThumbURL, tt.wantThumbURL)
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

func TestShareService_RevokeAfterMediaTrashed(t *testing.T) {
	for _, actor := range []string{"creator", "admin"} {
		t.Run(actor, func(t *testing.T) {
			ctx := context.Background()
			store, err := repository.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })

			clk := newMockClock()
			now := clk.Now()
			createUser := func(name string, admin bool) int64 {
				t.Helper()
				id, err := store.CreateUser(ctx, &model.User{
					Username: name, PasswordHash: "hash", IsAdmin: admin, CreatedAt: now,
				})
				if err != nil {
					t.Fatalf("create user %s: %v", name, err)
				}
				return id
			}
			creatorID := createUser("creator", false)
			otherID := createUser("other", false)
			adminID := createUser("admin", true)
			setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
			if err != nil {
				t.Fatalf("create set: %v", err)
			}
			for _, id := range []int64{creatorID, otherID} {
				if err := store.GrantPermission(ctx, &model.SetPermission{
					SetID: setID, UserID: id, Role: model.RoleViewer, CreatedAt: now,
				}); err != nil {
					t.Fatalf("grant permission: %v", err)
				}
			}
			mediaID, err := store.CreateMedia(ctx, &model.Media{
				SetID: setID, RelPath: "song.mp3", FileName: "song.mp3",
				AbsPath: "/set/song.mp3", Type: model.MediaTypeAudio, CreatedAt: now,
			})
			if err != nil {
				t.Fatalf("create media: %v", err)
			}
			svc := NewShareService(store, clk, NewAccessHelper(store))
			share, err := svc.CreateShare(ctx, creatorID, mediaID, now.Add(time.Hour), nil)
			if err != nil {
				t.Fatalf("create share: %v", err)
			}
			if err := store.SoftDeleteMedia(ctx, mediaID); err != nil {
				t.Fatalf("trash media: %v", err)
			}
			listed, err := svc.ListMyShares(ctx, creatorID)
			if err != nil || len(listed) != 1 || listed[0].Token != share.Token {
				t.Fatalf("trashed share list = %+v, %v", listed, err)
			}
			if err := svc.RevokeShare(ctx, share.Token, otherID); !errors.Is(err, ErrForbidden) {
				t.Fatalf("noncreator revoke = %v, want forbidden", err)
			}
			if got, err := store.GetShareByToken(ctx, share.Token); err != nil || got == nil {
				t.Fatalf("share after denied revoke = %+v, %v", got, err)
			}
			revokerID := creatorID
			if actor == "admin" {
				revokerID = adminID
			}
			if err := svc.RevokeShare(ctx, share.Token, revokerID); err != nil {
				t.Fatalf("revoke trashed share: %v", err)
			}
			if err := store.RestoreMedia(ctx, mediaID); err != nil {
				t.Fatalf("restore media: %v", err)
			}
			if _, err := svc.ValidateShareToken(ctx, share.Token); !errors.Is(err, ErrShareNotFound) {
				t.Fatalf("restored media share = %v, want not found", err)
			}
		})
	}
}
