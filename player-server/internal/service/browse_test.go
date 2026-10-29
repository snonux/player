package service

import (
	"context"
	"errors"
	"testing"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
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

// Flattened single-file folders must keep the same name order on every
// request; playback resolves an edge and then reloads the target folder.
func TestBuildFoldersIn_DeterministicMediaOrder(t *testing.T) {
	media := []model.Media{
		{ID: 9, RelPath: "z.mp3", FileName: "z.mp3"},
		{ID: 8, RelPath: "same.mp3", FileName: "same.mp3"},
		{ID: 2, RelPath: "A/a.mp3", FileName: "a.mp3"},
		{ID: 3, RelPath: "C/same.mp3", FileName: "same.mp3"},
		{ID: 1, RelPath: "A.flac", FileName: "A.flac"},
		{ID: 4, RelPath: "B/same.mp3", FileName: "same.mp3"},
		{ID: 7, RelPath: "same.mp3", FileName: "same.mp3"},
	}
	want := []int64{1, 2, 4, 3, 7, 8, 9}
	for attempt := range 30 {
		rotation := attempt % len(media)
		input := append(append([]model.Media{}, media[rotation:]...), media[:rotation]...)
		folderMap, items := buildFolderMap(input, "")
		folders, got := buildFoldersIn(thumb.Layout{}, folderMap, input, items, "", "", "")
		if len(folders) != 0 || len(got) != len(want) {
			t.Fatalf("attempt %d: unexpected folders/media: %v/%v", attempt, folders, got)
		}
		for index, id := range want {
			if got[index].ID != id {
				t.Fatalf("attempt %d: position %d: got media %d, want %d", attempt, index, got[index].ID, id)
			}
		}
	}
}

func TestBrowseSetSortedRetainsFoldersAndFlattenedOrder(t *testing.T) {
	for _, facade := range []bool{false, true} {
		name := "browse service"
		if facade {
			name = "media service facade"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			calls := 0
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{ListMediaFunc: func(_ context.Context, f repository.MediaFilter) ([]model.Media, error) {
					calls++
					if f.Sort != "size_desc" || f.UserID != 7 {
						t.Fatalf("sort/user not forwarded: %+v", f)
					}
					// Flattened single-file directory must retain its repository rank.
					return []model.Media{{ID: 3, FileName: "z.mp4", RelPath: "single/z.mp4"}, {ID: 1, FileName: "a.mp4", RelPath: "a.mp4"}, {ID: 2, FileName: "one.mp4", RelPath: "folder/one.mp4"}, {ID: 4, FileName: "two.mp4", RelPath: "folder/two.mp4"}}, nil
				}},
				SetRepo:  repository.MockSetRepo{GetSetByIDFunc: func(context.Context, int64) (*model.Set, error) { return &model.Set{ID: 1, RootPath: "sort"}, nil }},
				UserRepo: repository.MockUserRepo{GetUserByIDFunc: func(context.Context, int64) (*model.User, error) { return &model.User{ID: 7, IsAdmin: true}, nil }},
			}
			var browse MediaBrowseService
			if facade {
				browse = NewMediaService(store, clock.RealClock{}, t.TempDir(), nil, nil)
			} else {
				browse = NewBrowseService(store, clock.RealClock{}, t.TempDir(), &accessHelper{store: store}, nil)
			}
			svc, ok := browse.(interface {
				BrowseSetSorted(context.Context, int64, int64, string, string) (*BrowseResult, error)
			})
			if !ok {
				t.Fatal("sorted browse capability is hidden by the service facade")
			}
			got, err := svc.BrowseSetSorted(ctx, 1, 7, "", "size_desc")
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("made %d media queries; want 1", calls)
			}
			if len(got.Folders) != 1 || got.Folders[0].Name != "folder" {
				t.Fatalf("folders = %+v", got.Folders)
			}
			if len(got.Media) != 2 || got.Media[0].ID != 3 || got.Media[1].ID != 1 {
				t.Fatalf("media = %+v", got.Media)
			}
		})
	}
}
