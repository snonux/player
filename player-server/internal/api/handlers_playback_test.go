package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

// newPlaybackTestServer creates a Server wired with a PlaybackHintsService for testing.
func newPlaybackTestServer(t *testing.T, store repository.Store, sm auth.SessionManager, hintSvc service.PlaybackHintsService) *Server {
	t.Helper()
	fs := newTestFS(map[string]string{
		"index.html": "index", "login.html": "login",
		"bootstrap.html": "bootstrap", "share.html": "share",
	})
	authSvc := &service.MockAuthService{
		CountUsersFunc:  func(context.Context) (int, error) { return 1, nil },
		GetUserByIDFunc: func(context.Context, int64) (*model.User, error) { return &model.User{ID: 1, IsAdmin: true}, nil },
	}
	// NewServer now returns (*Server, error); we pass a non-nil Config here,
	// so a failure points to a wiring bug in the test setup.
	srv, err := NewServer(ServerDeps{
		Store:          store,
		SessionManager: sm,
		Config:         &internal.Config{},
		Services: ServerServices{
			Media: MediaServices{
				PlaybackHints: hintSvc,
			},
			Auth: authSvc,
		},
		StaticFS:      fs,
		MediaStreamer: service.NewMediaStreamer(nil, ""),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// sessionForPlaybackTest creates a session cookie that resolves to userID 1.
func sessionForPlaybackTest(t *testing.T, store repository.Store, sm auth.SessionManager) *http.Cookie {
	t.Helper()
	return addSessionCookie(t, store, sm, 1)
}

// ------------------------------------------------------------------
// GET /api/v1/media/{id}/playback
// ------------------------------------------------------------------

func TestHandlePlaybackHints_Success(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)

	hint := &service.PlaybackHint{
		StreamURL:       "/api/v1/media/5/stream",
		Container:       "mp4",
		VideoCodec:      "h264",
		AudioCodec:      "aac",
		DurationSeconds: 300.0,
		FileSizeBytes:   1234567,
		Width:           1920,
		Height:          1080,
		Bitrate:         4000000,
		NeedsTranscode:  false,
	}
	hintSvc := &service.MockPlaybackHintsService{
		GetPlaybackHintFunc: func(_ context.Context, mediaID, userID int64) (*service.PlaybackHint, error) {
			if mediaID != 5 || userID != 1 {
				return nil, errors.New("unexpected args")
			}
			return hint, nil
		},
	}

	srv := newPlaybackTestServer(t, store, sm, hintSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/5/playback", nil)
	req.AddCookie(sessionForPlaybackTest(t, store, sm))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var got service.PlaybackHint
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Container != "mp4" {
		t.Errorf("Container: want mp4, got %s", got.Container)
	}
	if got.VideoCodec != "h264" {
		t.Errorf("VideoCodec: want h264, got %s", got.VideoCodec)
	}
	if got.AudioCodec != "aac" {
		t.Errorf("AudioCodec: want aac, got %s", got.AudioCodec)
	}
	if got.NeedsTranscode {
		t.Error("NeedsTranscode: want false, got true")
	}
	if got.DurationSeconds != 300.0 {
		t.Errorf("DurationSeconds: want 300, got %f", got.DurationSeconds)
	}
}

func TestHandlePlaybackHints_InvalidID(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	hintSvc := &service.MockPlaybackHintsService{}

	srv := newPlaybackTestServer(t, store, sm, hintSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/abc/playback", nil)
	req.AddCookie(sessionForPlaybackTest(t, store, sm))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", rr.Code)
	}
}

func TestHandlePlaybackHints_NotFound(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	hintSvc := &service.MockPlaybackHintsService{
		GetPlaybackHintFunc: func(_ context.Context, _, _ int64) (*service.PlaybackHint, error) {
			return nil, service.ErrNotFound
		},
	}

	srv := newPlaybackTestServer(t, store, sm, hintSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/99/playback", nil)
	req.AddCookie(sessionForPlaybackTest(t, store, sm))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandlePlaybackHints_Forbidden(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	hintSvc := &service.MockPlaybackHintsService{
		GetPlaybackHintFunc: func(_ context.Context, _, _ int64) (*service.PlaybackHint, error) {
			return nil, service.ErrForbidden
		},
	}

	srv := newPlaybackTestServer(t, store, sm, hintSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/3/playback", nil)
	req.AddCookie(sessionForPlaybackTest(t, store, sm))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestHandlePlaybackHints_NoService(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)

	// Passing nil PlaybackHintsService should yield 501.
	srv := newPlaybackTestServer(t, store, sm, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/1/playback", nil)
	req.AddCookie(sessionForPlaybackTest(t, store, sm))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rr.Code)
	}
}

func TestHandlePlaybackHints_LegacyPath(t *testing.T) {
	// The route is also available under /api/media/{id}/playback (handleBoth).
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)

	hint := &service.PlaybackHint{Container: "mkv", NeedsTranscode: true}
	hintSvc := &service.MockPlaybackHintsService{
		GetPlaybackHintFunc: func(_ context.Context, _, _ int64) (*service.PlaybackHint, error) {
			return hint, nil
		},
	}

	srv := newPlaybackTestServer(t, store, sm, hintSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/media/2/playback", nil)
	req.AddCookie(sessionForPlaybackTest(t, store, sm))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on legacy path, got %d: %s", rr.Code, rr.Body.String())
	}

	var got service.PlaybackHint
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.NeedsTranscode {
		t.Error("expected NeedsTranscode=true for mkv")
	}
}

func TestHandlePlaybackHints_MKVNeedsTranscode(t *testing.T) {
	// End-to-end: hintSvc returns a real hint for an mkv file with exotic codec.
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)

	hint := &service.PlaybackHint{
		StreamURL:      "/api/v1/media/10/stream",
		Container:      "mkv",
		VideoCodec:     "h264",
		AudioCodec:     "ac3",
		NeedsTranscode: true,
	}
	hintSvc := &service.MockPlaybackHintsService{
		GetPlaybackHintFunc: func(_ context.Context, _, _ int64) (*service.PlaybackHint, error) {
			return hint, nil
		},
	}

	srv := newPlaybackTestServer(t, store, sm, hintSvc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/10/playback", nil)
	req.AddCookie(sessionForPlaybackTest(t, store, sm))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var got service.PlaybackHint
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.NeedsTranscode {
		t.Error("expected NeedsTranscode=true for mkv/ac3")
	}
	if got.Container != "mkv" {
		t.Errorf("container: want mkv, got %s", got.Container)
	}
}
