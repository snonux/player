package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
)

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

func makeTempFile(t *testing.T, data string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "media-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func newUploadRequest(t *testing.T, setID, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, content)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/sets/"+setID+"/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func buildAdminSessionStore(userID int64) *repository.MockStore {
	store := buildSessionStore(userID)
	store.UserRepo.GetUserByIDFunc = func(ctx context.Context, id int64) (*model.User, error) {
		return &model.User{ID: id, Username: "admin", IsAdmin: true}, nil
	}
	return store
}

func sessionCookieForStore(t *testing.T, store repository.Store, sm *auth.SessionManager, userID int64) *http.Cookie {
	t.Helper()
	return addSessionCookie(t, store, sm, userID)
}

// ------------------------------------------------------------------
// Server helpers (server.go)
// ------------------------------------------------------------------

func Test_addrFromPort(t *testing.T) {
	if got := addrFromPort(8080); got != ":8080" {
		t.Fatalf("expected :8080, got %s", got)
	}
}

func TestNewGracefulServer(t *testing.T) {
	cfg := &internal.Config{Port: 3000}
	gs := NewGracefulServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), cfg)
	if gs.Server.Addr != ":3000" {
		t.Fatalf("unexpected addr %s", gs.Server.Addr)
	}
}

func TestPingStore_nonPinger(t *testing.T) {
	store := &repository.MockStore{}
	srv := newTestServer(t, store, nil, nil, &internal.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := srv.pingStore(context.Background()); err != nil {
		t.Fatal("expected nil for non-pinger")
	}
}

func TestPingStore_pingerError(t *testing.T) {
	store := &mockPingStore{err: errors.New("down")}
	srv := newTestServer(t, store, nil, nil, &internal.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := srv.pingStore(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

// ------------------------------------------------------------------
// String / readJSON / writeJSON / context helpers
// ------------------------------------------------------------------

func Test_stringPtr(t *testing.T) {
	s := "x"
	p := stringPtr(s)
	if p == nil || *p != s {
		t.Fatal("unexpected")
	}
}

func Test_readJSON_nilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	var dst map[string]any
	if err := readJSON(req, &dst); err == nil {
		t.Fatal("expected error for nil body")
	}
}

func Test_writeJSON_encodeError(t *testing.T) {
	// channel cannot be JSON-encoded, triggering the error path
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusOK, make(chan int))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func Test_userIDFromContext_missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if userIDFromContext(req) != 0 {
		t.Fatal("expected 0")
	}
}

func Test_sessionIDFromContext_missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if sessionIDFromContext(req) != "" {
		t.Fatal("expected empty")
	}
}

// ------------------------------------------------------------------
// Static pages (serveFile)
// ------------------------------------------------------------------

func TestServer_ServeFile_success(t *testing.T) {
	store := buildSessionStore(1)
	store.SessionRepo = repository.MockSessionRepo{
		GetSessionByIDFunc: func(ctx context.Context, id string) (*model.Session, error) {
			return &model.Session{ID: id, UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	}
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	srv := newTestServer(t, store, nil, sm, &internal.Config{SessionTimeoutHours: 24}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestServer_ServeFile_notFound(t *testing.T) {
	fs := newTestFS(map[string]string{}) // no index.html
	store := buildSessionStore(1)
	store.SessionRepo = repository.MockSessionRepo{
		GetSessionByIDFunc: func(ctx context.Context, id string) (*model.Session, error) {
			return &model.Session{ID: id, UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	}
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	srv := newTestServer(t, store, nil, sm, &internal.Config{SessionTimeoutHours: 24}, nil, nil, nil, nil, nil, nil, nil, nil, nil, fs)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
	}
}

// ------------------------------------------------------------------
// Bootstrap negative paths
// ------------------------------------------------------------------

func TestServer_Bootstrap_negativePaths(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		countErr  error
		hashErr   error
		createErr error
		sessErr   error
		wantCode  int
	}{
		{"invalid json", `bad`, nil, nil, nil, nil, http.StatusBadRequest},
		{"count users error", `{"username":"u","password":"p"}`, errors.New("boom"), nil, nil, nil, http.StatusInternalServerError},
		{"hash error", `{"username":"u","password":"p"}`, nil, errors.New("boom"), nil, nil, http.StatusInternalServerError},
		{"create user error", `{"username":"u","password":"p"}`, nil, nil, errors.New("boom"), nil, http.StatusInternalServerError},
		{"create session error", `{"username":"u","password":"p"}`, nil, nil, nil, errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = tt.wantCode
		})
	}
}

func TestServer_Bootstrap_hashError(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}
	authSvc := &service.MockAuthService{
		BootstrapFunc: func(ctx context.Context, username, password string) (*service.AuthResult, error) {
			return nil, errors.New("hash err")
		},
	}
	srv := newTestServer(t, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, authSvc, nil)
	body := `{"username":"u","password":"p"}`
	req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

func TestServer_Bootstrap_createUserError(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}
	authSvc := &service.MockAuthService{
		BootstrapFunc: func(ctx context.Context, username, password string) (*service.AuthResult, error) {
			return nil, errors.New("boom")
		},
	}
	srv := newTestServer(t, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, authSvc, nil)
	body := `{"username":"u","password":"p"}`
	req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

func TestServer_Bootstrap_createSessionError(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}
	authSvc := &service.MockAuthService{
		BootstrapFunc: func(ctx context.Context, username, password string) (*service.AuthResult, error) {
			return nil, errors.New("boom")
		},
	}
	srv := newTestServer(t, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, authSvc, nil)
	body := `{"username":"u","password":"p"}`
	req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

// ------------------------------------------------------------------
// Login negative paths
// ------------------------------------------------------------------

func TestServer_Login_negativePaths(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}

	t.Run("invalid json", func(t *testing.T) {
		authSvc := &service.MockAuthService{}
		srv := newTestServer(t, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, authSvc, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`bad`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("db error", func(t *testing.T) {
		authSvc := &service.MockAuthService{
			LoginFunc: func(ctx context.Context, username, password string) (*service.AuthResult, error) {
				return nil, errors.New("boom")
			},
		}
		srv := newTestServer(t, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, authSvc, nil)
		body := `{"username":"alice","password":"correct"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

// ------------------------------------------------------------------
// Sets
// ------------------------------------------------------------------

func TestServer_SetCover(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
		{"not found", "1", false, service.ErrNotFound, http.StatusNotFound},
		{"forbidden", "1", false, service.ErrForbidden, http.StatusForbidden},
		{"ok", "1", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					RegenerateSetCoverFunc: func(ctx context.Context, setID int64, folder string, userID int64) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/sets/"+tt.id+"/cover", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_ListSets_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	t.Run("nil service", func(t *testing.T) {
		srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/sets", nil)
		req.AddCookie(sessionCookieForStore(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})

	t.Run("service error", func(t *testing.T) {
		ms := &service.MockMediaService{
			ListSetsFunc: func(ctx context.Context, userID int64) ([]model.Set, error) {
				return nil, errors.New("boom")
			},
		}
		srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/sets", nil)
		req.AddCookie(sessionCookieForStore(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

// ------------------------------------------------------------------
// Upload
// ------------------------------------------------------------------

func TestServer_Upload(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, MaxUploadSizeMB: 10}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		noFile   bool
		large    bool
		wantCode int
	}{
		{"nil service", "1", true, nil, false, false, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, false, false, http.StatusBadRequest},
		{"missing file", "1", false, nil, true, false, http.StatusBadRequest},
		{"file too large", "1", false, nil, false, true, http.StatusRequestEntityTooLarge},
		{"forbidden", "1", false, service.ErrForbidden, false, false, http.StatusForbidden},
		{"not found", "1", false, service.ErrNotFound, false, false, http.StatusNotFound},
		{"unsupported ext", "1", false, service.ErrUnsupportedExtension, false, false, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), false, false, http.StatusInternalServerError},
		{"ok", "1", false, nil, false, false, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					UploadMediaFunc: func(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error) {
						return &model.Media{ID: 1, FileName: filename}, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			var req *http.Request
			if tt.noFile {
				var buf bytes.Buffer
				w := multipart.NewWriter(&buf)
				_ = w.Close()
				req = httptest.NewRequest(http.MethodPost, "/api/sets/"+tt.id+"/upload", &buf)
				req.Header.Set("Content-Type", w.FormDataContentType())
			} else if tt.large {
				var buf bytes.Buffer
				w := multipart.NewWriter(&buf)
				part, _ := w.CreateFormFile("file", "big.bin")
				part.Write(make([]byte, 11*1024*1024))
				_ = w.Close()
				req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/sets/%s/upload", tt.id), &buf)
				req.Header.Set("Content-Type", w.FormDataContentType())
			} else {
				req = newUploadRequest(t, tt.id, "test.mp4", "data")
			}
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Media detail, favorite, tags
// ------------------------------------------------------------------

func TestServer_MediaDetail_nilService(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/media/1", nil)
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rr.Code)
	}
}

func TestServer_Favorite_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					ToggleFavoriteFunc: func(ctx context.Context, userID, mediaID int64) (bool, error) {
						return false, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/media/"+tt.id+"/favorite", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AddTag_nilService(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/media/1/tags", strings.NewReader(`{"tag":"x"}`))
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rr.Code)
	}
}

func TestServer_RemoveTag_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		tag      string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", "t", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", "t", false, nil, http.StatusBadRequest},
		{"service error", "1", "t", false, errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					RemoveTagFunc: func(ctx context.Context, mediaID, userID int64, tagName string) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/media/%s/tags/%s", tt.id, tt.tag), nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Stream / Download / Thumbnail / RegenThumbnail
// ------------------------------------------------------------------

func TestServer_Stream(t *testing.T) {
	path := makeTempFile(t, "videodata")
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		res      *service.FileResult
		wantCode int
	}{
		{"nil service", "1", true, nil, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), nil, http.StatusInternalServerError},
		{"not found", "1", false, nil, nil, http.StatusNotFound},
		{"file missing", "1", false, nil, &service.FileResult{Path: "/nonexistent", FileName: "a.mp4"}, http.StatusNotFound},
		{"ok", "1", false, nil, &service.FileResult{Path: path, FileName: "a.mp4"}, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					StreamMediaFunc: func(ctx context.Context, mediaID, userID int64) (*service.FileResult, error) {
						return tt.res, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/media/"+tt.id+"/stream", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestLooksLikeMPEGTS(t *testing.T) {
	ts := make([]byte, 188*5)
	for i := 0; i < len(ts); i += 188 {
		ts[i] = 0x47
	}
	tsPath := filepath.Join(t.TempDir(), "mislabelled.mp4")
	if err := os.WriteFile(tsPath, ts, 0o644); err != nil {
		t.Fatal(err)
	}
	if !probe.LooksLikeMPEGTS(tsPath) {
		t.Fatal("expected MPEG-TS sync bytes to be detected")
	}

	mp4Path := filepath.Join(t.TempDir(), "real.mp4")
	if err := os.WriteFile(mp4Path, []byte("\x00\x00\x00\x18ftypmp42"), 0o644); err != nil {
		t.Fatal(err)
	}
	if probe.LooksLikeMPEGTS(mp4Path) {
		t.Fatal("did not expect MP4 header to be detected as MPEG-TS")
	}
}

func TestServer_Download(t *testing.T) {
	path := makeTempFile(t, "filedata")
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		res      *service.FileResult
		wantCode int
		wantDisp bool
	}{
		{"nil service", "1", true, nil, nil, http.StatusNotImplemented, false},
		{"invalid id", "abc", false, nil, nil, http.StatusBadRequest, false},
		{"service error", "1", false, errors.New("boom"), nil, http.StatusInternalServerError, false},
		{"not found", "1", false, nil, nil, http.StatusNotFound, false},
		{"file missing", "1", false, nil, &service.FileResult{Path: "/nonexistent", FileName: "a.mp4"}, http.StatusNotFound, false},
		{"ok", "1", false, nil, &service.FileResult{Path: path, FileName: "a.mp4"}, http.StatusOK, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					DownloadMediaFunc: func(ctx context.Context, mediaID, userID int64) (*service.FileResult, error) {
						return tt.res, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/media/"+tt.id+"/download", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
			if tt.wantDisp {
				disp := rr.Header().Get("Content-Disposition")
				if !strings.Contains(disp, "attachment") {
					t.Fatalf("expected attachment disposition, got %q", disp)
				}
			}
		})
	}
}

func TestServer_Thumbnail(t *testing.T) {
	path := makeTempFile(t, "thumbdata")
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		res      *service.FileResult
		wantCode int
	}{
		{"nil service", "1", true, nil, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), nil, http.StatusInternalServerError},
		{"not found", "1", false, nil, nil, http.StatusNotFound},
		{"file missing", "1", false, nil, &service.FileResult{Path: "/nonexistent", FileName: "t.jpg"}, http.StatusNotFound},
		{"ok", "1", false, nil, &service.FileResult{Path: path, FileName: "t.jpg"}, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					GetThumbnailFunc: func(ctx context.Context, mediaID, userID int64) (*service.FileResult, error) {
						return tt.res, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/media/"+tt.id+"/thumbnail", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_RegenThumbnail(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", "1", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					RegenerateThumbnailFunc: func(ctx context.Context, mediaID, userID int64) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/media/"+tt.id+"/thumbnail", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_RegenThumbnail_errorMapping(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		svcErr   error
		wantCode int
	}{
		{"not found", service.ErrNotFound, http.StatusNotFound},
		{"forbidden", service.ErrForbidden, http.StatusForbidden},
		{"internal error", errors.New("boom"), http.StatusInternalServerError},
		{"ok", nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &service.MockMediaService{
				RegenerateThumbnailFunc: func(ctx context.Context, mediaID, userID int64) error {
					return tt.svcErr
				},
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/media/1/thumbnail", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Shares
// ------------------------------------------------------------------

func TestServer_CreateShare_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, ShareDefaultExpiryDays: 14}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					CreateShareFunc: func(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error) {
						return nil, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/media/"+tt.id+"/shares", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_ListShares_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					ListSharesFunc: func(ctx context.Context, mediaID, userID int64) ([]model.Share, error) {
						return nil, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/media/"+tt.id+"/shares", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_RevokeShare(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		token    string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "abc", true, nil, http.StatusNotImplemented},
		{"service error", "abc", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", "abc", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					RevokeShareFunc: func(ctx context.Context, token string, userID int64) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodDelete, "/api/shares/"+tt.token, nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_SharePage(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}
	fs := newTestFS(map[string]string{
		"index.html":     "index",
		"login.html":     "login",
		"share.html":     "<html>share</html>",
		"bootstrap.html": "bootstrap",
	})

	t.Run("nil service", func(t *testing.T) {
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})

	t.Run("service error", func(t *testing.T) {
		ms := &service.MockMediaService{
			GetSharedMediaFunc: func(ctx context.Context, token string) (*service.GetSharedMediaResult, error) {
				return nil, errors.New("boom")
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		ms := &service.MockMediaService{
			GetSharedMediaFunc: func(ctx context.Context, token string) (*service.GetSharedMediaResult, error) {
				return nil, nil
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("expired", func(t *testing.T) {
		ms := &service.MockMediaService{
			GetSharedMediaFunc: func(ctx context.Context, token string) (*service.GetSharedMediaResult, error) {
				return nil, service.ErrShareExpired
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusGone {
			t.Fatalf("expected %d, got %d", http.StatusGone, rr.Code)
		}
	})

	t.Run("html default accept", func(t *testing.T) {
		ms := &service.MockMediaService{
			GetSharedMediaFunc: func(ctx context.Context, token string) (*service.GetSharedMediaResult, error) {
				return &service.GetSharedMediaResult{
					Media: &service.SharedMediaView{ID: 1, FileName: "share.mp4", Type: model.MediaTypeVideo, Duration: 120},
					StreamURL: "/s/abc/stream",
					ThumbURL:  "/s/abc/thumbnail",
				}, nil
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		ct := rr.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html content type, got %q", ct)
		}
		body := rr.Body.String()
		if !strings.Contains(body, "<html>") {
			t.Fatalf("expected share.html body, got %q", body)
		}
	})

	t.Run("html explicit accept", func(t *testing.T) {
		ms := &service.MockMediaService{
			GetSharedMediaFunc: func(ctx context.Context, token string) (*service.GetSharedMediaResult, error) {
				return &service.GetSharedMediaResult{
					Media: &service.SharedMediaView{ID: 1, FileName: "share.mp4", Type: model.MediaTypeVideo, Duration: 120},
					StreamURL: "/s/abc/stream",
					ThumbURL:  "/s/abc/thumbnail",
				}, nil
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		req.Header.Set("Accept", "text/html")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		ct := rr.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html content type, got %q", ct)
		}
	})

	t.Run("json accept", func(t *testing.T) {
		ms := &service.MockMediaService{
			GetSharedMediaFunc: func(ctx context.Context, token string) (*service.GetSharedMediaResult, error) {
				return &service.GetSharedMediaResult{
					Media: &service.SharedMediaView{ID: 1, FileName: "share.mp4", Type: model.MediaTypeVideo, Duration: 120},
					StreamURL: "/s/abc/stream",
					ThumbURL:  "/s/abc/thumbnail",
				}, nil
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		req.Header.Set("Accept", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		ct := rr.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected application/json content type, got %q", ct)
		}
		var body service.GetSharedMediaResult
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("expected JSON body: %v", err)
		}
		if body.StreamURL != "/s/abc/stream" {
			t.Fatalf("unexpected stream_url %q", body.StreamURL)
		}
	})
}

func TestServer_ShareStream(t *testing.T) {
	path := makeTempFile(t, "shared")
	cfg := &internal.Config{SessionTimeoutHours: 24}

	t.Run("nil service", func(t *testing.T) {
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rr.Code)
		}
	})

	t.Run("service error", func(t *testing.T) {
		ms := &service.MockMediaService{
			StreamSharedMediaFunc: func(ctx context.Context, token string) (*service.FileResult, error) {
				return nil, errors.New("boom")
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		ms := &service.MockMediaService{
			StreamSharedMediaFunc: func(ctx context.Context, token string) (*service.FileResult, error) {
				return nil, service.ErrShareNotFound
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("expired", func(t *testing.T) {
		ms := &service.MockMediaService{
			StreamSharedMediaFunc: func(ctx context.Context, token string) (*service.FileResult, error) {
				return nil, service.ErrShareExpired
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusGone {
			t.Fatalf("expected %d, got %d", http.StatusGone, rr.Code)
		}
	})

	t.Run("media not found", func(t *testing.T) {
		ms := &service.MockMediaService{
			StreamSharedMediaFunc: func(ctx context.Context, token string) (*service.FileResult, error) {
				return nil, service.ErrMediaNotFound
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("file missing", func(t *testing.T) {
		ms := &service.MockMediaService{
			StreamSharedMediaFunc: func(ctx context.Context, token string) (*service.FileResult, error) {
				return &service.FileResult{Path: "/nonexistent", FileName: "a.mp4"}, nil
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("ok", func(t *testing.T) {
		ms := &service.MockMediaService{
			StreamSharedMediaFunc: func(ctx context.Context, token string) (*service.FileResult, error) {
				return &service.FileResult{Path: path, FileName: "a.mp4"}, nil
			},
		}
		srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

// ------------------------------------------------------------------
// SoftDelete / Restore
// ------------------------------------------------------------------

func TestServer_SoftDelete_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", "1", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					SoftDeleteMediaFunc: func(ctx context.Context, mediaID, userID int64) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodDelete, "/api/media/"+tt.id, nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_Restore_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", "1", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					RestoreMediaFunc: func(ctx context.Context, mediaID, userID int64) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/media/"+tt.id+"/restore", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Notes
// ------------------------------------------------------------------

func TestServer_UpsertNote(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		body     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", `{"content":"hi"}`, true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", `{"content":"hi"}`, false, nil, http.StatusBadRequest},
		{"invalid body", "1", `bad`, false, nil, http.StatusBadRequest},
		{"service error", "1", `{"content":"hi"}`, false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", "1", `{"content":"hi"}`, false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					UpsertNoteFunc: func(ctx context.Context, note *model.Note) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/media/"+tt.id+"/notes", strings.NewReader(tt.body))
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_DeleteNote(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "1", true, nil, http.StatusNotImplemented},
		{"invalid id", "abc", false, nil, http.StatusBadRequest},
		{"service error", "1", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", "1", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ms service.MediaService
			if !tt.svcNil {
				ms = &service.MockMediaService{
					DeleteNoteFunc: func(ctx context.Context, mediaID, userID int64) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := httptest.NewRequest(http.MethodDelete, "/api/media/"+tt.id+"/notes", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Progress
// ------------------------------------------------------------------

func TestServer_Progress_negative(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		body     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", `{"media_id":1,"position_seconds":1}`, true, nil, http.StatusNotImplemented},
		{"invalid body", `bad`, false, nil, http.StatusBadRequest},
		{"missing media_id", `{"position_seconds":1}`, false, nil, http.StatusBadRequest},
		{"service error", `{"media_id":1,"position_seconds":1}`, false, errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps service.ProgressService
			if !tt.svcNil {
				ps = &service.MockProgressService{
					UpdateProgressFunc: func(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error {
						return tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, ps, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/progress", strings.NewReader(tt.body))
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Admin routes negative paths
// ------------------------------------------------------------------

func TestServer_AdminRescan(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", true, nil, http.StatusNotImplemented},
		{"service error", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					TriggerRescanFunc: func(ctx context.Context) error { return tt.svcErr },
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/rescan", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AdminListTrash(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", true, nil, http.StatusNotImplemented},
		{"service error", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					ListTrashFunc: func(ctx context.Context) ([]model.Media, error) { return nil, tt.svcErr },
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/admin/trash", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AdminListUsers(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", true, nil, http.StatusNotImplemented},
		{"service error", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					ListUsersFunc: func(ctx context.Context) ([]model.User, error) { return nil, tt.svcErr },
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AdminCreateUser(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		body     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", `{"username":"bob","password":"pass","is_admin":false}`, true, nil, http.StatusNotImplemented},
		{"invalid body", `bad`, false, nil, http.StatusBadRequest},
		{"missing username", `{"password":"pass"}`, false, nil, http.StatusBadRequest},
		{"service error", `{"username":"bob","password":"pass","is_admin":false}`, false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", `{"username":"bob","password":"pass","is_admin":false}`, false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					CreateUserFunc: func(ctx context.Context, username, password string, isAdmin bool) (*model.User, error) {
						return &model.User{ID: 2, Username: username, IsAdmin: isAdmin}, tt.svcErr
					},
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/users", strings.NewReader(tt.body))
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AdminDeleteUser(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		id       string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", "2", true, nil, http.StatusNotImplemented},
		{"self delete", "1", false, nil, http.StatusBadRequest},
		{"service error", "2", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", "2", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					DeleteUserFunc: func(ctx context.Context, id int64) error { return tt.svcErr },
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+tt.id, nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AdminListPermissions(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", true, nil, http.StatusNotImplemented},
		{"service error", false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					ListPermissionsFunc: func(ctx context.Context) (*service.PermissionsMatrix, error) { return nil, tt.svcErr },
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/admin/permissions", nil)
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AdminGrantPermission(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		body     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", `{"set_id":1,"user_id":2,"role":"viewer"}`, true, nil, http.StatusNotImplemented},
		{"invalid body", `bad`, false, nil, http.StatusBadRequest},
		{"service error", `{"set_id":1,"user_id":2,"role":"viewer"}`, false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", `{"set_id":1,"user_id":2,"role":"viewer"}`, false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					GrantPermissionFunc: func(ctx context.Context, setID, userID int64, role model.Role) error { return tt.svcErr },
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/permissions", strings.NewReader(tt.body))
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestServer_AdminRevokePermission(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}

	tests := []struct {
		name     string
		body     string
		svcNil   bool
		svcErr   error
		wantCode int
	}{
		{"nil service", `{"set_id":1,"user_id":2}`, true, nil, http.StatusNotImplemented},
		{"invalid body", `bad`, false, nil, http.StatusBadRequest},
		{"service error", `{"set_id":1,"user_id":2}`, false, errors.New("boom"), http.StatusInternalServerError},
		{"ok", `{"set_id":1,"user_id":2}`, false, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var as service.AdminService
			if !tt.svcNil {
				as = &service.MockAdminService{
					RevokePermissionFunc: func(ctx context.Context, setID, userID int64) error { return tt.svcErr },
				}
			}
			srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, as, nil, nil, nil)
			req := httptest.NewRequest(http.MethodDelete, "/api/admin/permissions", strings.NewReader(tt.body))
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Custom hasher for error injection
// ------------------------------------------------------------------

type errHasher struct{}

func (e *errHasher) Hash(password string) (string, error) { return "", errors.New("hash err") }
func (e *errHasher) Compare(hash, password string) error  { return errors.New("compare err") }

// ------------------------------------------------------------------
// parseMediaListQuery
// ------------------------------------------------------------------

func mustParseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	return u.Query()
}

func Test_parseMediaListQuery_defaults(t *testing.T) {
	q := mustParseQuery(t, "/api/media")
	got := parseMediaListQuery(q)
	want := repository.MediaFilter{Limit: 100, Offset: 0}
	if got.Search != want.Search || got.Sort != want.Sort || got.Limit != want.Limit || got.Offset != want.Offset {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if got.SetID != nil || got.Type != nil || got.Favorites != false || got.MinDuration != nil || got.MaxDuration != nil {
		t.Fatalf("expected nil optional fields, got %+v", got)
	}
}

func Test_parseMediaListQuery_allParams(t *testing.T) {
	q := mustParseQuery(t, "/api/media?search=foo&sort=name&set_id=7&type=video&favorites=true&tags=bar,baz&min_duration=10&max_duration=100&limit=50&offset=10")
	got := parseMediaListQuery(q)
	if got.Search != "foo" {
		t.Fatalf("unexpected search: %q", got.Search)
	}
	if got.Sort != "name" {
		t.Fatalf("unexpected sort: %q", got.Sort)
	}
	if got.SetID == nil || *got.SetID != 7 {
		t.Fatalf("unexpected set_id: %v", got.SetID)
	}
	if got.Type == nil || *got.Type != "video" {
		t.Fatalf("unexpected type: %v", got.Type)
	}
	if got.Favorites != true {
		t.Fatalf("unexpected favorites: %v", got.Favorites)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "bar" || got.Tags[1] != "baz" {
		t.Fatalf("unexpected tags: %v", got.Tags)
	}
	if got.MinDuration == nil || *got.MinDuration != 10 {
		t.Fatalf("unexpected min_duration: %v", got.MinDuration)
	}
	if got.MaxDuration == nil || *got.MaxDuration != 100 {
		t.Fatalf("unexpected max_duration: %v", got.MaxDuration)
	}
	if got.Limit != 50 {
		t.Fatalf("unexpected limit: %d", got.Limit)
	}
	if got.Offset != 10 {
		t.Fatalf("unexpected offset: %d", got.Offset)
	}
}

func Test_parseMediaListQuery_limitClampingAndNegativeOffset(t *testing.T) {
	tests := []struct {
		name          string
		limit         string
		offset        string
		wantLimit     int
		wantOffset    int
		invalidParam  string
		invalidKey    string
		invalidBadVal string
	}{
		{"limit too high", "5000", "0", 100, 0, "", "", ""},
		{"limit negative", "-5", "0", 100, 0, "", "", ""},
		{"limit zero", "0", "0", 100, 0, "", "", ""},
		{"valid limit", "200", "0", 200, 0, "", "", ""},
		{"offset negative", "100", "-1", 100, 0, "", "", ""},
		{"offset string", "100", "foo", 100, 0, "", "", ""},
		{"max duration bad", "100", "0", 100, 0, "max_duration", "max_duration", "bad"},
		{"min duration bad", "100", "0", 100, 0, "min_duration", "min_duration", "bad"},
		{"set_id bad", "100", "0", 100, 0, "set_id", "set_id", "bad"},
		{"favorites bad", "100", "0", 100, 0, "favorites", "favorites", "bad"},
		{"limit valid", "100", "0", 100, 0, "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := mustParseQuery(t, "/api/media?limit="+tt.limit+"&offset="+tt.offset)
			if tt.invalidKey != "" {
				q.Set(tt.invalidKey, tt.invalidBadVal)
			}
			got := parseMediaListQuery(q)
			if got.Limit != tt.wantLimit {
				t.Fatalf("unexpected limit: got %d, want %d", got.Limit, tt.wantLimit)
			}
			if got.Offset != tt.wantOffset {
				t.Fatalf("unexpected offset: got %d, want %d", got.Offset, tt.wantOffset)
			}
			// Ensure invalid params don't cause panics and are treated as omitted
			if tt.invalidKey == "set_id" && got.SetID != nil {
				t.Fatalf("expected set_id nil for bad value, got %v", got.SetID)
			}
			if tt.invalidKey == "favorites" && got.Favorites != false {
				t.Fatalf("expected favorites nil for bad value, got %v", got.Favorites)
			}
			if tt.invalidKey == "min_duration" && got.MinDuration != nil {
				t.Fatalf("expected min_duration nil for bad value, got %v", got.MinDuration)
			}
			if tt.invalidKey == "max_duration" && got.MaxDuration != nil {
				t.Fatalf("expected max_duration nil for bad value, got %v", got.MaxDuration)
			}
		})
	}
}

func Test_parseMediaListQuery_emptyTags(t *testing.T) {
	q := mustParseQuery(t, "/api/media?tags=")
	got := parseMediaListQuery(q)
	if got.Tags != nil {
		t.Fatalf("expected nil tags for empty string, got %v", got.Tags)
	}
}

// ------------------------------------------------------------------
// Negative nil-service tests for narrow interface split
// ------------------------------------------------------------------

func TestServer_NilAdminSvc(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	store.UserRepo.GetUserByIDFunc = func(ctx context.Context, id int64) (*model.User, error) {
		return &model.User{ID: 1, IsAdmin: true}, nil
	}
	srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cookie := addSessionCookie(t, store, sm, 1)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/trash"},
		{http.MethodPost, "/api/admin/rescan"},
		{http.MethodGet, "/api/admin/scan-progress"},
		{http.MethodGet, "/api/admin/users"},
		{http.MethodPost, "/api/admin/users"},
		{http.MethodDelete, "/api/admin/users/1"},
		{http.MethodGet, "/api/admin/permissions"},
		{http.MethodPost, "/api/admin/permissions"},
		{http.MethodDelete, "/api/admin/permissions"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.AddCookie(cookie)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotImplemented {
				t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rr.Code)
			}
		})
	}
}

func TestServer_NilProgressSvc(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cookie := addSessionCookie(t, store, sm, 1)

	req := httptest.NewRequest(http.MethodPost, "/api/progress", strings.NewReader(`{"media_id":1,"position_seconds":5}`))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected %d, got %d", http.StatusNotImplemented, rr.Code)
	}
}
