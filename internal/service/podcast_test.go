package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
	"codeberg.org/snonux/player/internal/repository"
)

func setupPodcastService(t *testing.T) (*podcastService, *repository.MockStore) {
	t.Helper()
	mediaRoot := t.TempDir()
	clk := &clock.MockClock{T: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := repository.NewMockStore()
	helper := &accessHelper{store: store}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewPodcastService(store, clk, mediaRoot, helper, nil, nil, &http.Client{Timeout: DefaultHTTPClientTimeout}, 60)
	svc.logger = logger
	return svc, store
}

func TestPodcastService_CustomHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	svc := NewPodcastServiceWithLogger(repository.NewMockStore(), &clock.MockClock{T: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, t.TempDir(), nil, nil, nil, custom, 60, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if svc.httpClient != custom {
		t.Fatal("expected injected httpClient to be stored")
	}
}

func TestPodcastService_NilHTTPClient_Defaults(t *testing.T) {
	store := repository.NewMockStore()
	clk := &clock.MockClock{T: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewPodcastServiceWithLogger(store, clk, t.TempDir(), nil, nil, nil, nil, 60, logger)
	if svc.httpClient == nil {
		t.Fatal("expected non-nil httpClient when nil passed to constructor")
	}
	if svc.httpClient.Timeout != DefaultHTTPClientTimeout {
		t.Fatalf("expected default timeout %v, got %v", DefaultHTTPClientTimeout, svc.httpClient.Timeout)
	}
}

func TestPodcastService_SubscribeFeed_Ok(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	var setID int64
	store.SetRepo = repository.MockSetRepo{
		ListSetsFunc: func(ctx context.Context) ([]model.Set, error) {
			return nil, nil
		},
		CreateSetFunc: func(ctx context.Context, set *model.Set) (int64, error) {
			if set.Name != "podcast" || set.RootPath != "podcast" {
				t.Fatalf("expected fixed podcast set, got name=%q root=%q", set.Name, set.RootPath)
			}
			setID++
			return setID, nil
		},
	}
	var feedID int64
	store.PodcastRepo = repository.MockPodcastRepo{
		CreateFeedFunc: func(ctx context.Context, feed *model.PodcastFeed) (int64, error) {
			feedID++
			return feedID, nil
		},
		UpdateFeedFunc:    func(ctx context.Context, feed *model.PodcastFeed) error { return nil },
		CreateEpisodeFunc: func(ctx context.Context, ep *model.PodcastEpisode) (int64, error) { return 1, nil },
	}

	svc.parseFeed = func(_ *http.Client, url string) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{
			Title:       "Test Feed",
			Description: "desc",
			ImageURL:    "http://example.com/cover.jpg",
			Episodes: []podcast.Episode{
				{GUID: "ep-1", Title: "Episode 1", EpisodeURL: "http://example.com/1.mp3"},
			},
		}, nil
	}
	coverCalled := false
	svc.downloadCover = func(c *http.Client, u, p string) error {
		coverCalled = true
		return nil
	}

	feed, err := svc.SubscribeFeed(ctx, "http://rss.example.com/feed.xml", "my-podcast", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if feed == nil {
		t.Fatal("expected feed, got nil")
	}
	if feed.Title != "Test Feed" {
		t.Errorf("title = %q, want Test Feed", feed.Title)
	}
	if feed.LastCheckedAt == nil {
		t.Error("expected LastCheckedAt set")
	}
	if !coverCalled {
		t.Error("expected cover download to be called")
	}
	if setID != 1 {
		t.Errorf("expected set created once, got %d", setID)
	}
	if feedID != 1 {
		t.Errorf("expected feed created once, got %d", feedID)
	}

	setPath := filepath.Join(svc.mediaRoot, "my-podcast")
	if _, err := os.Stat(setPath); !os.IsNotExist(err) {
		t.Error("custom set directory should not be created")
	}
	feedPath := filepath.Join(svc.mediaRoot, "podcast", "Test Feed")
	if _, err := os.Stat(feedPath); os.IsNotExist(err) {
		t.Error("expected feed directory under podcast set to exist on disk")
	}
}

func TestPodcastService_SubscribeFeed_NonAdmin(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: false}, nil
		},
	}

	_, err := svc.SubscribeFeed(ctx, "http://x", "name", 1)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestPodcastService_SubscribeFeed_NilUser(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return nil, nil
		},
	}

	_, err := svc.SubscribeFeed(ctx, "http://x", "name", 1)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestPodcastService_SubscribeFeed_UserError(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	boom := errors.New("boom")
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return nil, boom
		},
	}

	_, err := svc.SubscribeFeed(ctx, "http://x", "name", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}

func TestPodcastService_SubscribeFeed_ParseError(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	var setCreated bool
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	store.SetRepo = repository.MockSetRepo{
		CreateSetFunc: func(ctx context.Context, set *model.Set) (int64, error) {
			setCreated = true
			return 1, nil
		},
	}
	svc.parseFeed = func(_ *http.Client, url string) (*podcast.ParsedFeed, error) {
		return nil, errors.New("parse fail")
	}

	_, err := svc.SubscribeFeed(ctx, "bad", "name", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if setCreated {
		t.Error("expected no set created when parse fails")
	}
	setPath := filepath.Join(svc.mediaRoot, "name")
	if _, err := os.Stat(setPath); !os.IsNotExist(err) {
		t.Error("expected no set directory when parse fails")
	}
}

func TestPodcastService_SubscribeFeed_CreateSetError(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	boom := errors.New("boom")
	store.SetRepo = repository.MockSetRepo{
		CreateSetFunc: func(ctx context.Context, set *model.Set) (int64, error) {
			return 0, boom
		},
	}
	svc.parseFeed = func(_ *http.Client, url string) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{Title: "T"}, nil
	}

	_, err := svc.SubscribeFeed(ctx, "http://x", "name", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
	setPath := filepath.Join(svc.mediaRoot, "podcast")
	if _, err := os.Stat(setPath); !os.IsNotExist(err) {
		t.Error("expected set directory to be cleaned up")
	}
}

func TestPodcastService_SubscribeFeed_GrantPermissionError(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	boom := errors.New("boom")
	var deletedSetID int64
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	store.SetRepo = repository.MockSetRepo{
		CreateSetFunc: func(ctx context.Context, set *model.Set) (int64, error) {
			return 42, nil
		},
		DeleteSetFunc: func(ctx context.Context, id int64) error {
			deletedSetID = id
			return nil
		},
	}
	store.SetPermissionRepo = repository.MockSetPermissionRepo{
		GrantPermissionFunc: func(ctx context.Context, perm *model.SetPermission) error {
			return boom
		},
	}
	svc.parseFeed = func(_ *http.Client, url string) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{Title: "T"}, nil
	}

	_, err := svc.SubscribeFeed(ctx, "http://x", "name", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
	if deletedSetID != 42 {
		t.Fatalf("expected set rollback (delete %d), got %d", 42, deletedSetID)
	}
	setPath := filepath.Join(svc.mediaRoot, "podcast")
	if _, err := os.Stat(setPath); !os.IsNotExist(err) {
		t.Error("expected set directory to be cleaned up on permission error")
	}
}

func TestPodcastService_SubscribeFeed_CreateFeedError(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	boom := errors.New("boom")
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	store.SetRepo = repository.MockSetRepo{
		CreateSetFunc: func(ctx context.Context, set *model.Set) (int64, error) {
			return 42, nil
		},
	}
	store.SetPermissionRepo = repository.MockSetPermissionRepo{
		GrantPermissionFunc: func(ctx context.Context, perm *model.SetPermission) error {
			return nil
		},
	}
	store.PodcastRepo = repository.MockPodcastRepo{
		CreateFeedFunc: func(ctx context.Context, feed *model.PodcastFeed) (int64, error) {
			return 0, boom
		},
	}
	svc.parseFeed = func(_ *http.Client, url string) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{Title: "T"}, nil
	}

	_, err := svc.SubscribeFeed(ctx, "http://x", "name", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
	setPath := filepath.Join(svc.mediaRoot, "podcast")
	if _, err := os.Stat(setPath); os.IsNotExist(err) {
		t.Error("expected podcast set directory to remain")
	}
}

func TestPodcastService_PodcastFolderName(t *testing.T) {
	tests := []struct {
		setName string
		title   string
		want    string
	}{
		{"my-podcast", "Some Title", "my-podcast"},
		{"", "Some Title", "Some Title"},
		{"", "", "feed-7"},
		{"../../etc", "", "------etc"},
	}

	for _, tt := range tests {
		name := podcastFolderName(tt.setName, tt.title, 7)
		if name != tt.want {
			t.Errorf("podcastFolderName(%q, %q) = %q, want %q", tt.setName, tt.title, name, tt.want)
		}
	}
}

func TestPodcastService_SanitizeSetName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"A/B", "A-B"},
		{"A\\\\B", "A--B"},
		{"A.B", "A-B"},
		{"  spaced  ", "spaced"},
		{"", ""},
		{"normal", "normal"},
	}

	for _, c := range cases {
		got := sanitizeSetName(c.in)
		if got != c.want {
			t.Errorf("sanitizeSetName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPodcastService_SanitizeFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"A/B", "A-B"},
		{"A:B", "A-B"},
		{"  spaced  ", "spaced"},
		{"", ""},
		{"normal", "normal"},
	}

	for _, c := range cases {
		got := sanitizeFilename(c.in)
		if got != c.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPodcastService_DownloadEpisode_NonAdmin(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: false}, nil
		},
	}
	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByIDFunc: func(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
			return &model.PodcastEpisode{ID: id, FeedID: 1}, nil
		},
		GetFeedByIDFunc: func(ctx context.Context, id int64) (*model.PodcastFeed, error) {
			return &model.PodcastFeed{ID: id, SetID: 1}, nil
		},
	}
	store.SetPermissionRepo = repository.MockSetPermissionRepo{
		GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
			return nil, nil
		},
	}

	_, err := svc.DownloadEpisode(ctx, 1, 1)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestPodcastService_DownloadEpisode_Success(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake mp3 content"))
	}))
	defer server.Close()
	svc.httpClient = server.Client()

	setDir := filepath.Join(svc.mediaRoot, "podcast-set")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatalf("mkdir set dir: %v", err)
	}

	pub := time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)
	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByIDFunc: func(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
			return &model.PodcastEpisode{ID: id, FeedID: 1, Title: "Ep 1", EpisodeURL: server.URL + "/ep1.mp3", PublishedAt: &pub}, nil
		},
		GetFeedByIDFunc: func(ctx context.Context, id int64) (*model.PodcastFeed, error) {
			return &model.PodcastFeed{ID: id, SetID: 1}, nil
		},
		UpdateEpisodeMediaFunc: func(ctx context.Context, episodeID, mediaID int64, fileName string) error {
			return nil
		},
	}
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	store.SetRepo = repository.MockSetRepo{
		GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
			return &model.Set{ID: id, RootPath: "podcast-set"}, nil
		},
	}
	store.MediaRepo = repository.MockMediaRepo{
		CreateMediaFunc: func(ctx context.Context, media *model.Media) (int64, error) {
			return 42, nil
		},
	}

	media, err := svc.DownloadEpisode(ctx, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if media == nil {
		t.Fatal("expected media, got nil")
	}
	if media.ID != 42 {
		t.Errorf("media.ID = %d, want 42", media.ID)
	}
	if media.FileSizeBytes != int64(len("fake mp3 content")) {
		t.Errorf("media.FileSizeBytes = %d, want %d", media.FileSizeBytes, len("fake mp3 content"))
	}
	if _, err := os.Stat(media.AbsPath); os.IsNotExist(err) {
		t.Error("expected downloaded file to exist on disk")
	}
}

func TestPodcastService_DownloadEpisode_HTTPError(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	svc.httpClient = server.Client()

	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByIDFunc: func(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
			return &model.PodcastEpisode{ID: id, FeedID: 1, Title: "Ep 1", EpisodeURL: server.URL + "/ep1.mp3"}, nil
		},
		GetFeedByIDFunc: func(ctx context.Context, id int64) (*model.PodcastFeed, error) {
			return &model.PodcastFeed{ID: id, SetID: 1}, nil
		},
	}
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	store.SetRepo = repository.MockSetRepo{
		GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
			return &model.Set{ID: id, RootPath: "podcast-set"}, nil
		},
	}

	_, err := svc.DownloadEpisode(ctx, 1, 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("expected status 404 in error, got: %v", err)
	}
}

func TestPodcastService_DownloadEpisode_EpisodeNotFound(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByIDFunc: func(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
			return nil, nil
		},
	}
	_, err := svc.DownloadEpisode(ctx, 1, 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPodcastService_DownloadEpisode_FeedNotFound(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByIDFunc: func(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
			return &model.PodcastEpisode{ID: id, FeedID: 1}, nil
		},
		GetFeedByIDFunc: func(ctx context.Context, id int64) (*model.PodcastFeed, error) {
			return nil, nil
		},
	}
	_, err := svc.DownloadEpisode(ctx, 1, 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPodcastService_DownloadEpisode_SetNotFound(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByIDFunc: func(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
			return &model.PodcastEpisode{ID: id, FeedID: 1}, nil
		},
		GetFeedByIDFunc: func(ctx context.Context, id int64) (*model.PodcastFeed, error) {
			return &model.PodcastFeed{ID: id, SetID: 1}, nil
		},
	}
	store.UserRepo = repository.MockUserRepo{
		GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		},
	}
	store.SetRepo = repository.MockSetRepo{
		GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
			return nil, nil
		},
	}
	_, err := svc.DownloadEpisode(ctx, 1, 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPodcastService_UpsertFeedEpisodes(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	var created []model.PodcastEpisode
	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByGUIDFunc: func(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error) {
			return nil, nil
		},
		CreateEpisodeFunc: func(ctx context.Context, ep *model.PodcastEpisode) (int64, error) {
			created = append(created, *ep)
			return int64(len(created)), nil
		},
	}

	feed := &model.PodcastFeed{ID: 7}
	parsed := &podcast.ParsedFeed{
		Episodes: []podcast.Episode{
			{GUID: "g1", Title: "One"},
			{GUID: "g2", Title: "Two"},
		},
	}

	err := svc.upsertFeedEpisodes(ctx, feed, parsed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 episodes created, got %d", len(created))
	}
	if created[0].FeedID != 7 || created[0].GUID != "g1" {
		t.Errorf("episode 0 mismatch: %+v", created[0])
	}
	if created[1].GUID != "g2" {
		t.Errorf("episode 1 mismatch: %+v", created[1])
	}
}

func TestPodcastService_UpsertFeedEpisodes_SkipsExisting(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	created := 0
	store.PodcastRepo = repository.MockPodcastRepo{
		GetEpisodeByGUIDFunc: func(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error) {
			if guid == "g1" {
				return &model.PodcastEpisode{GUID: "g1"}, nil
			}
			return nil, nil
		},
		CreateEpisodeFunc: func(ctx context.Context, ep *model.PodcastEpisode) (int64, error) {
			created++
			return 1, nil
		},
	}

	feed := &model.PodcastFeed{ID: 7}
	parsed := &podcast.ParsedFeed{
		Episodes: []podcast.Episode{
			{GUID: "g1", Title: "One"},
			{GUID: "g2", Title: "Two"},
		},
	}

	_ = svc.upsertFeedEpisodes(ctx, feed, parsed)
	if created != 1 {
		t.Fatalf("expected 1 new episode created, got %d", created)
	}
}

func TestPodcastService_UpdateFeedFromParsed(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	var updated *model.PodcastFeed
	store.PodcastRepo = repository.MockPodcastRepo{
		UpdateFeedFunc: func(ctx context.Context, feed *model.PodcastFeed) error {
			updated = feed
			return nil
		},
	}

	feed := &model.PodcastFeed{ID: 1}
	parsed := &podcast.ParsedFeed{
		Title:       "New Title",
		Description: "New Desc",
		ImageURL:    "http://img",
	}

	err := svc.updateFeedFromParsed(ctx, feed, parsed, "etag-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated == nil {
		t.Fatal("expected feed updated")
	}
	if updated.Title != "New Title" {
		t.Errorf("title = %q", updated.Title)
	}
	if updated.LastETag != "etag-123" {
		t.Errorf("etag = %q", updated.LastETag)
	}
	if updated.LastCheckedAt == nil {
		t.Fatal("expected LastCheckedAt")
	}
}

func TestPodcastService_InsertPodcastEpisodes(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	var created []model.PodcastEpisode
	store.PodcastRepo = repository.MockPodcastRepo{
		CreateEpisodeFunc: func(ctx context.Context, ep *model.PodcastEpisode) (int64, error) {
			created = append(created, *ep)
			return int64(len(created)), nil
		},
	}

	parsed := &podcast.ParsedFeed{
		Episodes: []podcast.Episode{
			{GUID: "g1", Title: "One"},
		},
	}
	svc.insertPodcastEpisodes(ctx, parsed, 99)
	if len(created) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(created))
	}
	if created[0].FeedID != 99 || created[0].GUID != "g1" {
		t.Errorf("episode mismatch: %+v", created[0])
	}
}

func TestPodcastService_CheckFeeds_EmptyList(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	store.PodcastRepo = repository.MockPodcastRepo{
		ListFeedsNeedingCheckFunc: func(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
			return []model.PodcastFeed{}, nil
		},
	}

	err := svc.CheckFeeds(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPodcastService_CheckFeeds_Concurrent_Ok(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	var checked []int64
	var mu sync.Mutex

	callOrder := make(chan int64, 3)
	svc.parseFeedReader = func(r io.Reader) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{Title: "T"}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "etag-"+r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<rss><channel><title>X</title></channel></rss>`))
		select {
		case callOrder <- 1:
		default:
		}
	}))
	defer server.Close()

	store.PodcastRepo = repository.MockPodcastRepo{
		ListFeedsNeedingCheckFunc: func(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
			return []model.PodcastFeed{
				{ID: 1, FeedURL: server.URL + "/1.xml"},
				{ID: 2, FeedURL: server.URL + "/2.xml"},
				{ID: 3, FeedURL: server.URL + "/3.xml"},
			}, nil
		},
		UpdateFeedFunc: func(ctx context.Context, feed *model.PodcastFeed) error {
			mu.Lock()
			checked = append(checked, feed.ID)
			mu.Unlock()
			return nil
		},
	}

	svc.httpClient = server.Client()

	err := svc.CheckFeeds(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	if len(checked) != 3 {
		t.Fatalf("expected 3 feeds checked, got %d", len(checked))
	}
	mu.Unlock()

	// Verify we processed 3 requests concurrently by reading from channel.
	processed := 0
	done := time.After(100 * time.Millisecond)
	for {
		select {
		case <-callOrder:
			processed++
			if processed == 3 {
				return
			}
		case <-done:
			t.Fatalf("expected 3 feed checks, got %d", processed)
		}
	}
}

func TestPodcastService_CheckFeeds_AllFeedsFail(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store.PodcastRepo = repository.MockPodcastRepo{
		ListFeedsNeedingCheckFunc: func(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
			return []model.PodcastFeed{
				{ID: 1, FeedURL: server.URL + "/1.xml"},
				{ID: 2, FeedURL: server.URL + "/2.xml"},
			}, nil
		},
		UpdateFeedFunc: func(ctx context.Context, feed *model.PodcastFeed) error { return nil },
	}

	svc.httpClient = server.Client()

	err := svc.CheckFeeds(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type panicRoundTripper struct{}

func (panicRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	panic("transport boom")
}

func TestPodcastService_CheckFeeds_FeedPanicRecovered(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	store.PodcastRepo = repository.MockPodcastRepo{
		ListFeedsNeedingCheckFunc: func(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
			return []model.PodcastFeed{{ID: 1, FeedURL: "http://example.test/feed.xml"}}, nil
		},
	}
	svc.httpClient = &http.Client{Transport: panicRoundTripper{}}

	if err := svc.CheckFeeds(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPodcastService_CheckFeeds_ListError(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)
	boom := errors.New("boom")
	store.PodcastRepo = repository.MockPodcastRepo{
		ListFeedsNeedingCheckFunc: func(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
			return nil, boom
		},
	}

	err := svc.CheckFeeds(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}

func TestPodcastService_CheckFeeds_FeedError_Continues(t *testing.T) {
	ctx := context.Background()
	svc, store := setupPodcastService(t)

	var mu sync.Mutex
	updates := make(map[int64]*model.PodcastFeed)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "bad") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<rss><channel><title>X</title></channel></rss>`))
	}))
	defer server.Close()

	store.PodcastRepo = repository.MockPodcastRepo{
		ListFeedsNeedingCheckFunc: func(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
			return []model.PodcastFeed{
				{ID: 1, FeedURL: server.URL + "/bad.xml"},
				{ID: 2, FeedURL: server.URL + "/ok.xml"},
			}, nil
		},
		UpdateFeedFunc: func(ctx context.Context, feed *model.PodcastFeed) error {
			mu.Lock()
			updates[feed.ID] = feed
			mu.Unlock()
			return nil
		},
	}

	svc.httpClient = server.Client()

	err := svc.CheckFeeds(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if updates[1].ConsecutiveFailures != 1 || updates[1].NextCheckAt == nil {
		t.Fatalf("expected feed 1 to have failure backoff, got %+v", updates[1])
	}
	if updates[2].ConsecutiveFailures != 0 || updates[2].NextCheckAt != nil {
		t.Fatalf("expected feed 2 to have no failures, got %+v", updates[2])
	}
	mu.Unlock()
}
