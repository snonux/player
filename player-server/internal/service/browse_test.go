package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// fakeResolver is a test double for thumb.Resolver that lets tests assert
// browseService delegates thumbnail lookup instead of touching the disk.
type fakeResolver struct {
	called    bool
	gotMedia  *model.Media
	resolved  *thumb.ResolvedFile
	resolvErr error
}

func (f *fakeResolver) Resolve(media *model.Media) (*thumb.ResolvedFile, error) {
	f.called = true
	f.gotMedia = media
	return f.resolved, f.resolvErr
}

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
			name: "single audio folder with cover is flattened",
			set:  &model.Set{ID: 1, RootPath: "audiobooks"},
			media: []model.Media{
				{ID: 1, SetID: 1, RelPath: "Book/Book.m4b", FileName: "Book.m4b", Type: model.MediaTypeAudio},
				{ID: 2, SetID: 1, RelPath: "Book/cover.jpg", FileName: "cover.jpg", Type: model.MediaTypeImage},
			},
			wantMedia:   1,
			wantFolders: 0,
		},
		{
			name: "audio folder with extra content is still enterable",
			set:  &model.Set{ID: 1, RootPath: "audiobooks"},
			media: []model.Media{
				{ID: 1, SetID: 1, RelPath: "Book/Book.m4b", FileName: "Book.m4b", Type: model.MediaTypeAudio},
				{ID: 2, SetID: 1, RelPath: "Book/bonus.mp3", FileName: "bonus.mp3", Type: model.MediaTypeAudio},
				{ID: 3, SetID: 1, RelPath: "Book/cover.jpg", FileName: "cover.jpg", Type: model.MediaTypeImage},
			},
			wantMedia:   0,
			wantFolders: 1,
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
			svc := NewBrowseService(store, clock.RealClock{}, tmpDir, &accessHelper{store: store}, nil)
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
				return []model.Media{{ID: 10, SetID: 1, RelPath: "Test Feed/downloaded.mp3", FileName: "downloaded.mp3"}}, nil
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
			ListFeedsBySetIDFunc: func(ctx context.Context, setID int64) ([]model.PodcastFeed, error) {
				return []model.PodcastFeed{{ID: 99, SetID: setID, Title: "Test Feed"}}, nil
			},
			ListEpisodesWithStatusFunc: func(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
				return []model.PodcastEpisodeWithStatus{
					{PodcastEpisode: model.PodcastEpisode{ID: 1, Title: "Downloaded", IsDownloaded: true}},
					{PodcastEpisode: model.PodcastEpisode{ID: 2, Title: "Undownloaded", IsDownloaded: false}},
				}, nil
			},
		},
	}
	browser := NewPodcastBrowseService(&store.PodcastRepo, t.TempDir())
	svc := NewBrowseService(store, clock.RealClock{}, t.TempDir(), &accessHelper{store: store}, browser)
	res, err := svc.BrowseSet(ctx, 1, 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Folders) != 1 || res.Folders[0].Name != "Test Feed" {
		t.Fatalf("expected podcast feed folder, got %+v", res.Folders)
	}

	res, err = svc.BrowseSet(ctx, 1, 1, "Test Feed")
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

// TestBrowseService_GetThumbnail_DelegatesToResolver verifies the service
// no longer calls os.Stat directly: GetThumbnail should hand off to the
// injected thumb.Resolver and translate ResolvedFile -> FileResult.
func TestBrowseService_GetThumbnail_DelegatesToResolver(t *testing.T) {
	ctx := context.Background()

	media := &model.Media{ID: 7, SetID: 1, FileName: "a.mp4", ThumbnailPath: "/anywhere/x.jpg"}
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return media, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
	}

	t.Run("resolver hit becomes FileResult", func(t *testing.T) {
		fake := &fakeResolver{resolved: &thumb.ResolvedFile{Path: "/anywhere/x.jpg", FileName: "x.jpg", FileSize: 42}}
		svc := NewBrowseServiceWithResolver(store, clock.RealClock{}, "/tmp/media", &accessHelper{store: store}, nil, fake)
		res, err := svc.GetThumbnail(ctx, 7, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !fake.called {
			t.Fatal("expected resolver to be invoked")
		}
		if fake.gotMedia == nil || fake.gotMedia.ID != 7 {
			t.Fatalf("resolver received wrong media: %+v", fake.gotMedia)
		}
		if res.Path != "/anywhere/x.jpg" || res.FileName != "x.jpg" || res.FileSize != 42 {
			t.Fatalf("unexpected FileResult: %+v", res)
		}
	})

	t.Run("resolver ErrNotFound maps to service ErrNotFound", func(t *testing.T) {
		fake := &fakeResolver{resolvErr: thumb.ErrNotFound}
		svc := NewBrowseServiceWithResolver(store, clock.RealClock{}, "/tmp/media", &accessHelper{store: store}, nil, fake)
		_, err := svc.GetThumbnail(ctx, 7, 1)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("other resolver errors propagate", func(t *testing.T) {
		boom := errors.New("disk on fire")
		fake := &fakeResolver{resolvErr: boom}
		svc := NewBrowseServiceWithResolver(store, clock.RealClock{}, "/tmp/media", &accessHelper{store: store}, nil, fake)
		_, err := svc.GetThumbnail(ctx, 7, 1)
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("expected non-not-found error, got %v", err)
		}
	})

	t.Run("nil resolver falls back to default", func(t *testing.T) {
		// NewBrowseServiceWithResolver(nil resolver) should still work and
		// not panic — the constructor swaps in the FS resolver.
		svc := NewBrowseServiceWithResolver(store, clock.RealClock{}, "/tmp/media", &accessHelper{store: store}, nil, nil)
		if svc == nil {
			t.Fatal("expected non-nil service")
		}
	})
}
