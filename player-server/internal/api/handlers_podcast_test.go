package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
	"codeberg.org/snonux/player/internal/thumb"
)

// newPodcastTestServer is like newTestServer but accepts a real podcast service.
func newPodcastTestServer(t *testing.T, store repository.Store, hasher auth.Hasher, sm auth.SessionManager, cfg *internal.Config,
	browseSvc service.MediaBrowseService,
	writeSvc service.MediaWriteService,
	shareSvc service.MediaShareService,
	tagSvc service.MediaTagService,
	favSvc service.MediaFavoriteService,
	noteSvc service.MediaNoteService,
	adminSvc service.AdminService,
	progressSvc service.ProgressService,
	authSvc service.AuthService,
	podcastSvc service.PodcastEpisodeService,
	fs http.FileSystem,
) *Server {
	t.Helper()
	if fs == nil {
		fs = newTestFS(map[string]string{
			"index.html":     "index",
			"login.html":     "login",
			"bootstrap.html": "bootstrap",
			"share.html":     "share",
		})
	}
	if authSvc == nil {
		authSvc = &service.MockAuthService{
			CountUsersFunc:  func(context.Context) (int, error) { return 1, nil },
			GetUserByIDFunc: func(context.Context, int64) (*model.User, error) { return &model.User{ID: 1, IsAdmin: true}, nil },
		}
	}
	return NewServer(ServerDeps{
		Store:          store,
		Hasher:         hasher,
		SessionManager: sm,
		Config:         cfg,
		Services: ServerServices{
			Browse:   browseSvc,
			Write:    writeSvc,
			Share:    shareSvc,
			Tag:      tagSvc,
			Favorite: favSvc,
			Note:     noteSvc,
			Admin:    adminSvc,
			Progress: progressSvc,
			Auth:     authSvc,
			Podcast:  podcastSvc,
		},
		StaticFS: fs,
	})
}

// setupPodcastE2E creates a full server with a real SQLite store and real services.
func setupPodcastE2E(t *testing.T) (srv *Server, store repository.Store, sm auth.SessionManager, adminID int64, cleanup func()) {
	t.Helper()

	dbStore, err := repository.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	clk := &clock.MockClock{T: now}

	adminID, err = dbStore.CreateUser(ctx, &model.User{
		Username:     "admin",
		PasswordHash: "hashed",
		IsAdmin:      true,
		CreatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	hasher := &staticHasher{fixed: "hashed"}
	sm = auth.NewSessionManager(dbStore, clk, time.Hour)
	authSvc := service.NewAuthService(dbStore, clk, hasher, sm, auth.NewTokenManager())

	mediaRoot := t.TempDir()
	helper := service.NewAccessHelper(dbStore)
	prober := &probe.MockProber{}
	thumbGen := &thumb.MockGenerator{}

	mediaSvc := service.NewMediaService(dbStore, clk, mediaRoot, thumbGen, prober)
	podcastSvc := service.NewPodcastService(dbStore, clk, mediaRoot, helper, prober, thumbGen, &http.Client{Timeout: service.DefaultHTTPClientTimeout}, 60)

	cfg := &internal.Config{
		SessionTimeoutHours: 24,
		MaxUploadSizeMB:     10,
		MediaRoot:           mediaRoot,
	}

	srv = newPodcastTestServer(t, dbStore, hasher, sm, cfg, mediaSvc, mediaSvc, mediaSvc, mediaSvc, mediaSvc, mediaSvc, nil, nil, authSvc, podcastSvc, nil)

	cleanup = func() {
		dbStore.Close()
	}

	return srv, dbStore, sm, adminID, cleanup
}

func TestPodcastE2E_FullFlow(t *testing.T) {
	srv, store, sm, adminID, cleanup := setupPodcastE2E(t)
	defer cleanup()

	// Audio server serves dummy MP3 bytes.
	audioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("dummy audio data"))
	}))
	defer audioServer.Close()

	rssBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel>
    <title>Test Podcast</title>
    <description>A test podcast</description>
    <item>
      <title>Episode 1</title>
      <guid>ep-1</guid>
      <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
      <enclosure url="%s/audio.mp3" length="1234" type="audio/mpeg"/>
      <itunes:duration>00:05:00</itunes:duration>
    </item>
  </channel>
</rss>`, audioServer.URL)

	rssServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rssBody))
	}))
	defer rssServer.Close()

	secondRSSBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Second Podcast</title>
    <description>Another test podcast</description>
    <item>
      <title>Second Episode</title>
      <guid>second-ep-1</guid>
      <enclosure url="%s/second.mp3" length="4321" type="audio/mpeg"/>
    </item>
  </channel>
</rss>`, audioServer.URL)
	secondRSSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(secondRSSBody))
	}))
	defer secondRSSServer.Close()

	badRSSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not a valid feed"))
	}))
	defer badRSSServer.Close()

	cookie := addSessionCookie(t, store, sm, adminID)

	var podcastSetID int64
	var episodeID int64
	var downloadedMediaID int64

	t.Run("subscribe podcast", func(t *testing.T) {
		body := fmt.Sprintf(`{"feed_url":"%s/rss.xml","set_name":"test-podcast"}`, rssServer.URL)
		req := httptest.NewRequest(http.MethodPost, "/api/podcasts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var feed model.PodcastFeed
		if err := json.Unmarshal(rr.Body.Bytes(), &feed); err != nil {
			t.Fatalf("unmarshal feed: %v", err)
		}
		if feed.ID <= 0 {
			t.Fatalf("expected feed id > 0, got %d", feed.ID)
		}
		podcastSetID = feed.SetID
	})

	t.Run("subscribe second podcast uses same set", func(t *testing.T) {
		body := fmt.Sprintf(`{"feed_url":"%s/rss.xml","set_name":"second-podcast"}`, secondRSSServer.URL)
		req := httptest.NewRequest(http.MethodPost, "/api/podcasts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var feed model.PodcastFeed
		if err := json.Unmarshal(rr.Body.Bytes(), &feed); err != nil {
			t.Fatalf("unmarshal feed: %v", err)
		}
		if feed.SetID != podcastSetID {
			t.Fatalf("expected set_id %d, got %d", podcastSetID, feed.SetID)
		}
	})

	t.Run("list podcasts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/podcasts", nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var feeds []model.PodcastFeed
		if err := json.Unmarshal(rr.Body.Bytes(), &feeds); err != nil {
			t.Fatalf("unmarshal feeds: %v", err)
		}

		if len(feeds) != 2 {
			t.Fatalf("expected 2 podcast feeds, got %d", len(feeds))
		}
		for _, feed := range feeds {
			if feed.SetID != podcastSetID {
				t.Fatalf("expected all feeds in set %d, got feed %+v", podcastSetID, feed)
			}
		}
	})

	t.Run("list episodes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/podcasts/%d/episodes", podcastSetID), nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var episodes []model.PodcastEpisodeWithStatus
		if err := json.Unmarshal(rr.Body.Bytes(), &episodes); err != nil {
			t.Fatalf("unmarshal episodes: %v", err)
		}
		if len(episodes) < 1 {
			t.Fatalf("expected at least 1 episode, got %d", len(episodes))
		}
		episodeID = episodes[0].ID
	})

	t.Run("download episode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/podcasts/episodes/%d/download", episodeID), nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var media model.Media
		if err := json.Unmarshal(rr.Body.Bytes(), &media); err != nil {
			t.Fatalf("unmarshal media: %v", err)
		}
		if media.ID <= 0 {
			t.Fatalf("expected media id > 0, got %d", media.ID)
		}
		downloadedMediaID = media.ID
	})

	t.Run("toggle complete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/podcasts/episodes/%d/complete", episodeID), nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rr.Code)
		}
	})

	t.Run("media list includes downloaded episode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/media", nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}

		var mediaList []model.Media
		if err := json.Unmarshal(rr.Body.Bytes(), &mediaList); err != nil {
			t.Fatalf("unmarshal media list: %v", err)
		}

		found := false
		for _, m := range mediaList {
			if m.ID == downloadedMediaID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected downloaded media %d in list", downloadedMediaID)
		}
	})

	// Error cases.
	t.Run("subscribe invalid url", func(t *testing.T) {
		body := fmt.Sprintf(`{"feed_url":"%s","set_name":"bad"}`, badRSSServer.URL)
		req := httptest.NewRequest(http.MethodPost, "/api/podcasts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("episodes non-existent set", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/podcasts/99999/episodes", nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("download non-existent episode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/podcasts/episodes/99999/download", nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
		}
	})
}
