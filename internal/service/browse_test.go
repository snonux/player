package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestBrowseService_BrowseSet(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		set         *model.Set
		setErr      error
		media       []model.Media
		mediaErr    error
		wantErr     bool
		wantMedia   int
		wantFolders int
	}{
		{
			name:        "ok empty set",
			set:         &model.Set{ID: 1, RootPath: "music"},
			media:       nil,
			wantMedia:   0,
			wantFolders: 0,
		},
		{
			name:        "ok with flat media",
			set:         &model.Set{ID: 1, RootPath: "music"},
			media:       []model.Media{{ID: 1, SetID: 1, RelPath: "song.mp3", FileName: "song.mp3"}},
			wantMedia:   1,
			wantFolders: 0,
		},
		{
			name:     "list media error",
			set:      &model.Set{ID: 1, RootPath: "music"},
			mediaErr: errors.New("boom"),
			wantErr:  true,
		},
		{
			name:    "set not found",
			set:     nil,
			media:   []model.Media{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					ListMediaFunc: func(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
						return tt.media, tt.mediaErr
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return tt.set, tt.setErr
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetPermissionRepo: repository.MockSetPermissionRepo{
					GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
						return nil, nil
					},
				},
			}
			svc := NewBrowseService(store, clock.RealClock{}, tmpDir, &accessHelper{store: store})
			res, err := svc.BrowseSet(ctx, 1, 1, "")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res.Media) != tt.wantMedia {
				t.Fatalf("expected %d media, got %d", tt.wantMedia, len(res.Media))
			}
			if len(res.Folders) != tt.wantFolders {
				t.Fatalf("expected %d folders, got %d", tt.wantFolders, len(res.Folders))
			}
		})
	}
}

func TestBrowseService_BrowseSet_PodcastEpisodes(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			ListMediaFunc: func(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
				return []model.Media{{ID: 10, SetID: 1, RelPath: "downloaded.mp3", FileName: "downloaded.mp3"}}, nil
			},
		},
		SetRepo: repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				return &model.Set{ID: id, RootPath: "podcast", IsPodcast: true}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
		SetPermissionRepo: repository.MockSetPermissionRepo{
			GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
				return nil, nil
			},
		},
		PodcastRepo: repository.MockPodcastRepo{
			GetFeedBySetIDFunc: func(ctx context.Context, setID int64) (*model.PodcastFeed, error) {
				return &model.PodcastFeed{ID: 99, SetID: setID}, nil
			},
			ListEpisodesWithStatusFunc: func(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
				return []model.PodcastEpisodeWithStatus{
					{PodcastEpisode: model.PodcastEpisode{ID: 1, Title: "Downloaded", IsDownloaded: true}},
					{PodcastEpisode: model.PodcastEpisode{ID: 2, Title: "Undownloaded", IsDownloaded: false}},
				}, nil
			},
		},
	}
	svc := NewBrowseService(store, clock.RealClock{}, t.TempDir(), &accessHelper{store: store})
	res, err := svc.BrowseSet(ctx, 1, 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Media) != 1 {
		t.Fatalf("expected downloaded media card, got %d media items", len(res.Media))
	}
	if len(res.Episodes) != 1 {
		t.Fatalf("expected only undownloaded episode, got %+v", res.Episodes)
	}
	if res.Episodes[0].ID != 2 {
		t.Fatalf("expected undownloaded episode ID 2, got %+v", res.Episodes[0])
	}
}
