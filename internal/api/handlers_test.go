package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

type memFS struct {
	http.FileSystem
}

func newTestFS(files map[string]string) http.FileSystem {
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return http.FS(fsys)
}

func newTestServer(t *testing.T, store repository.Store, hasher auth.Hasher, sm *auth.SessionManager, cfg *internal.Config,
	mediaSvc service.MediaService, adminSvc service.AdminService, progressSvc service.ProgressService,
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
	return NewServer(store, hasher, sm, cfg, mediaSvc, adminSvc, progressSvc, fs)
}

func addSessionCookie(t *testing.T, store repository.Store, sm *auth.SessionManager, userID int64) *http.Cookie {
	t.Helper()
	repo := store.(repository.SessionRepo)
	now := time.Now()
	if sm == nil {
		// provide a default mock session; tests that need real validation should create their own.
		return &http.Cookie{Name: "session", Value: "abc123"}
	}
	id, err := sm.CreateSession(context.Background(), userID)
	if err != nil {
		// fallback for test when repo doesn't implement CreateSessionFunc
		id = "testsession"
		_ = repo.CreateSession(context.Background(), &model.Session{ID: id, UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now})
	}
	return &http.Cookie{Name: "session", Value: id}
}

func requireAuthCookie(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session" {
			return
		}
	}
	t.Fatal("expected session cookie")
}

// ------------------------------------------------------------------
// Middleware tests
// ------------------------------------------------------------------

func TestMiddleware_BootstrapRedirect(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		userCount int
		userErr   error
		wantCode  int
		wantLoc   string
	}{
		{"public bootstrap html", "/bootstrap.html", 0, nil, http.StatusOK, ""},
		{"public api bootstrap", "/api/bootstrap", 0, nil, http.StatusOK, ""},
		{"public login html", "/login.html", 0, nil, http.StatusOK, ""},
		{"public api login", "/api/login", 0, nil, http.StatusOK, ""},
		{"public healthz", "/healthz", 0, nil, http.StatusOK, ""},
		{"public readyz", "/readyz", 0, nil, http.StatusOK, ""},
		{"protected no users", "/", 0, nil, http.StatusTemporaryRedirect, "/bootstrap.html"},
		{"protected users exist", "/", 1, nil, http.StatusOK, ""},
		{"count error", "/", 0, errors.New("boom"), http.StatusInternalServerError, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				UserRepo: repository.MockUserRepo{
					CountUsersFunc: func(ctx context.Context) (int, error) {
						return tt.userCount, tt.userErr
					},
				},
			}
			mw := NewMiddleware(store, nil)
			handler := mw.BootstrapRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
			if tt.wantLoc != "" && rr.Header().Get("Location") != tt.wantLoc {
				t.Fatalf("expected location %q, got %q", tt.wantLoc, rr.Header().Get("Location"))
			}
		})
	}
}

func TestMiddleware_RequireSession(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		cookie   *http.Cookie
		session  *model.Session
		sessErr  error
		wantCode int
	}{
		{"no cookie", nil, nil, nil, http.StatusUnauthorized},
		{"empty cookie", &http.Cookie{Name: "session", Value: ""}, nil, nil, http.StatusUnauthorized},
		{"valid session", &http.Cookie{Name: "session", Value: "abc"}, &model.Session{ID: "abc", UserID: 1, ExpiresAt: now.Add(time.Hour)}, nil, http.StatusOK},
		{"expired session", &http.Cookie{Name: "session", Value: "old"}, &model.Session{ID: "old", UserID: 1, ExpiresAt: now.Add(-time.Hour)}, nil, http.StatusUnauthorized},
		{"db error", &http.Cookie{Name: "session", Value: "abc"}, nil, errors.New("boom"), http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var deleted string
			repo := repository.MockSessionRepo{
				GetSessionByIDFunc: func(ctx context.Context, id string) (*model.Session, error) {
					return tt.session, tt.sessErr
				},
				DeleteSessionFunc: func(ctx context.Context, id string) error {
					deleted = id
					return nil
				},
			}
			sm := auth.NewSessionManager(&repo, &clock.MockClock{T: now}, time.Hour)
			mw := NewMiddleware(nil, sm)

			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sess, ok := r.Context().Value(sessionCtxKey).(*model.Session)
				if !ok && tt.wantCode == http.StatusOK {
					t.Fatal("expected session in context")
				}
				if tt.wantCode == http.StatusOK && sess.ID != "abc" {
					t.Fatalf("unexpected session id: %v", sess.ID)
				}
				w.WriteHeader(http.StatusOK)
			})
			handler := mw.RequireSession(inner)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
			if tt.name == "expired session" && deleted == "" {
				t.Fatal("expected expired session deletion")
			}
		})
	}
}

func TestMiddleware_RequireAdmin(t *testing.T) {
	tests := []struct {
		name     string
		ctxUser  *model.User
		userErr  error
		wantCode int
	}{
		{"no session in context", nil, nil, http.StatusUnauthorized},
		{"session but nil user", nil, nil, http.StatusForbidden},
		{"non-admin", &model.User{ID: 1, IsAdmin: false}, nil, http.StatusForbidden},
		{"admin", &model.User{ID: 1, IsAdmin: true}, nil, http.StatusOK},
		{"db error", nil, errors.New("boom"), http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						if tt.ctxUser != nil {
							return tt.ctxUser, tt.userErr
						}
						return nil, tt.userErr
					},
				},
			}
			mw := NewMiddleware(store, nil)
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler := mw.RequireAdmin(inner)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.name != "no session in context" {
				sess := &model.Session{UserID: 1}
				req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey, sess))
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Static pages
// ------------------------------------------------------------------

func TestServer_StaticPages(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}
	store := &repository.MockStore{
		UserRepo: repository.MockUserRepo{
			CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
		},
	}

	t.Run("index requires session", func(t *testing.T) {
		srv := newTestServer(t, store, nil, nil, cfg, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("login public", func(t *testing.T) {
		srv := newTestServer(t, store, nil, nil, cfg, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/login.html", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "login") {
			t.Fatal("expected login page body")
		}
	})

	t.Run("bootstrap public", func(t *testing.T) {
		srv := newTestServer(t, store, nil, nil, cfg, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/bootstrap.html", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("css public", func(t *testing.T) {
		fs := newTestFS(map[string]string{"css/theme.css": "body{}"})
		srv := newTestServer(t, store, nil, nil, cfg, nil, nil, nil, fs)
		req := httptest.NewRequest(http.MethodGet, "/css/theme.css", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

// ------------------------------------------------------------------
// Auth handlers
// ------------------------------------------------------------------

type staticHasher struct {
	fixed string
}

func (h *staticHasher) Hash(password string) (string, error) { return h.fixed, nil }
func (h *staticHasher) Compare(hash, password string) error {
	if hash == h.fixed && password == "correct" {
		return nil
	}
	if hash == h.fixed && password == "secret" {
		return nil
	}
	return errors.New("mismatch")
}

func TestServer_Bootstrap(t *testing.T) {
	hasher := &staticHasher{fixed: "hashed"}
	cfg := &internal.Config{SessionTimeoutHours: 24}

	t.Run("create first admin", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				CountUsersFunc: func(ctx context.Context) (int, error) { return 0, nil },
				CreateUserFunc: func(ctx context.Context, user *model.User) (int64, error) { return 7, nil },
			},
		}
		repo := repository.MockSessionRepo{
			CreateSessionFunc: func(ctx context.Context, session *model.Session) error { return nil },
		}
		sm := auth.NewSessionManager(&repo, &clock.MockClock{T: time.Now()}, time.Hour)
		srv := newTestServer(t, store, hasher, sm, cfg, nil, nil, nil, nil)

		body := `{"username":"admin","password":"secret"}`
		req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["username"] != "admin" {
			t.Fatalf("unexpected username: %v", resp["username"])
		}
		requireAuthCookie(t, rr)
	})

	t.Run("bootstrap already complete", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
			},
		}
		srv := newTestServer(t, store, hasher, nil, cfg, nil, nil, nil, nil)
		body := `{"username":"admin","password":"secret"}`
		req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		store := &repository.MockStore{UserRepo: repository.MockUserRepo{CountUsersFunc: func(ctx context.Context) (int, error) { return 0, nil }}}
		srv := newTestServer(t, store, hasher, nil, cfg, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader([]byte(`{"username":""}`)))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		srv := newTestServer(t, nil, hasher, nil, cfg, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
		}
	})
}

func TestServer_Login(t *testing.T) {
	hasher := &staticHasher{fixed: "hashed"}
	cfg := &internal.Config{SessionTimeoutHours: 24}

	t.Run("valid credentials", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
				GetUserByUsernameFunc: func(ctx context.Context, username string) (*model.User, error) {
					return &model.User{ID: 1, Username: "alice", PasswordHash: "hashed"}, nil
				},
			},
		}
		repo := repository.MockSessionRepo{
			CreateSessionFunc: func(ctx context.Context, session *model.Session) error { return nil },
		}
		sm := auth.NewSessionManager(&repo, &clock.MockClock{T: time.Now()}, time.Hour)
		srv := newTestServer(t, store, hasher, sm, cfg, nil, nil, nil, nil)
		body := `{"username":"alice","password":"correct"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["username"] != "alice" {
			t.Fatalf("unexpected username")
		}
		requireAuthCookie(t, rr)
	})

	t.Run("invalid credentials", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
				GetUserByUsernameFunc: func(ctx context.Context, username string) (*model.User, error) {
					return &model.User{ID: 1, Username: "alice", PasswordHash: "hashed"}, nil
				},
			},
		}
		srv := newTestServer(t, store, hasher, nil, cfg, nil, nil, nil, nil)
		body := `{"username":"alice","password":"wrong"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
				GetUserByUsernameFunc: func(ctx context.Context, username string) (*model.User, error) {
					return nil, nil
				},
			},
		}
		srv := newTestServer(t, store, hasher, nil, cfg, nil, nil, nil, nil)
		body := `{"username":"nobody","password":"pass"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(body)))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})
}

func TestServer_SessionCookieSecure(t *testing.T) {
	hasher := &staticHasher{fixed: "hashed"}
	store := &repository.MockStore{
		UserRepo: repository.MockUserRepo{
			CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
			GetUserByUsernameFunc: func(ctx context.Context, username string) (*model.User, error) {
				return &model.User{ID: 1, Username: "alice", PasswordHash: "hashed"}, nil
			},
		},
	}
	repo := repository.MockSessionRepo{
		CreateSessionFunc: func(ctx context.Context, session *model.Session) error { return nil },
	}
	sm := auth.NewSessionManager(&repo, &clock.MockClock{T: time.Now()}, time.Hour)

	t.Run("Secure=true by default", func(t *testing.T) {
		cfg := &internal.Config{SessionTimeoutHours: 24, SecureCookies: true}
		srv := newTestServer(t, store, hasher, sm, cfg, nil, nil, nil, nil)
		body := `{"username":"alice","password":"correct"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		for _, c := range rr.Result().Cookies() {
			if c.Name == "session" && c.Secure != true {
				t.Fatalf("expected Secure=true, got Secure=%v", c.Secure)
			}
		}
	})

	t.Run("Secure=false", func(t *testing.T) {
		cfg := &internal.Config{SessionTimeoutHours: 24, SecureCookies: false}
		srv := newTestServer(t, store, hasher, sm, cfg, nil, nil, nil, nil)
		body := `{"username":"alice","password":"correct"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		for _, c := range rr.Result().Cookies() {
			if c.Name == "session" && c.Secure != false {
				t.Fatalf("expected Secure=false, got Secure=%v", c.Secure)
			}
		}
	})

	t.Run("clear cookie respects Secure config", func(t *testing.T) {
		var deleted string
		sessStore := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
			},
			SessionRepo: repository.MockSessionRepo{
				GetSessionByIDFunc: func(ctx context.Context, id string) (*model.Session, error) {
					if id == "abc" {
						return &model.Session{ID: "abc", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
					}
					return nil, nil
				},
				DeleteSessionFunc: func(ctx context.Context, id string) error {
					deleted = id
					return nil
				},
			},
		}
		logoutSM := auth.NewSessionManager(&sessStore.SessionRepo, &clock.MockClock{T: time.Now()}, time.Hour)
		cfg := &internal.Config{SessionTimeoutHours: 24, SecureCookies: false}
		srv := newTestServer(t, sessStore, nil, logoutSM, cfg, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rr.Code)
		}
		if deleted != "abc" {
			t.Fatalf("expected session abc to be deleted, got %q", deleted)
		}
		for _, c := range rr.Result().Cookies() {
			if c.Name == "session" && c.Secure != false {
				t.Fatalf("expected Secure=false on cleared cookie, got Secure=%v", c.Secure)
			}
		}
	})
}

func TestServer_Logout(t *testing.T) {
	cfg := &internal.Config{SessionTimeoutHours: 24}

	t.Run("valid session logout", func(t *testing.T) {
		var deleted string
		repo := repository.MockSessionRepo{
			GetSessionByIDFunc: func(ctx context.Context, id string) (*model.Session, error) {
				if id == "abc" {
					return &model.Session{ID: "abc", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
				}
				return nil, nil
			},
			DeleteSessionFunc: func(ctx context.Context, id string) error {
				deleted = id
				return nil
			},
		}
		sm := auth.NewSessionManager(&repo, &clock.MockClock{T: time.Now()}, time.Hour)
		store := &repository.MockStore{UserRepo: repository.MockUserRepo{CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil }}}
		srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rr.Code)
		}
		if deleted != "abc" {
			t.Fatalf("expected session abc to be deleted, got %q", deleted)
		}
		for _, c := range rr.Result().Cookies() {
			if c.Name == "session" && c.MaxAge != -1 {
				t.Fatal("expected cleared session cookie")
			}
		}
	})

	t.Run("no cookie logout", func(t *testing.T) {
		store := &repository.MockStore{UserRepo: repository.MockUserRepo{CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil }}}
		srv := newTestServer(t, store, nil, nil, cfg, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})
}

func TestServer_Healthz(t *testing.T) {
	srv := newTestServer(t, &repository.MockStore{}, nil, nil, &internal.Config{}, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestServer_Readyz(t *testing.T) {
	t.Run("ping ok", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil }},
		}
		store2 := &mockPingStore{store: store, err: nil}
		srv := newTestServer(t, store2, nil, nil, &internal.Config{}, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("ping fail", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil }},
		}
		store2 := &mockPingStore{store: store, err: errors.New("down")}
		srv := newTestServer(t, store2, nil, nil, &internal.Config{}, nil, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}
	})
}

// ------------------------------------------------------------------
// Media handler tests (table-driven with mocked service)
// ------------------------------------------------------------------

func TestServer_MediaList(t *testing.T) {
	hasher := &staticHasher{fixed: "hashed"}
	cfg := &internal.Config{SessionTimeoutHours: 24, MaxUploadSizeMB: 10}

	tests := []struct {
		name         string
		filter       repository.MediaFilter
		listResult   []model.Media
		listErr      error
		query        string
		wantCode     int
		wantResponse string
	}{
		{
			name:       "ok",
			listResult: []model.Media{{ID: 1, FileName: "a.mp4"}, {ID: 2, FileName: "b.mp3"}},
			wantCode:   http.StatusOK,
		},
		{
			name:     "service error",
			listErr:  errors.New("boom"),
			wantCode: http.StatusInternalServerError,
		},
		{
			name:       "with query params",
			query: "?set_id=1&type=video&search=foo&tags=bar,baz&favorites=true&min_duration=10&max_duration=100&sort=name&limit=5&offset=10",
			filter: repository.MediaFilter{SetID: intPtr(1), Type: (*model.MediaType)(func() *string { s := "video"; return &s }()), Search: "foo", Tags: []string{"bar", "baz"}, Favorites: true, MinDuration: floatPtr(10), MaxDuration: floatPtr(100), Sort: "name", Limit: 5, Offset: 10},
			listResult: []model.Media{},
			wantCode:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotFilter repository.MediaFilter
			ms := &service.MockMediaService{
				ListMediaFunc: func(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error) {
					gotFilter = filter
					return tt.listResult, tt.listErr
				},
			}
			store := buildSessionStore(1)
			sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
			srv := newTestServer(t, buildCountStore(1), hasher, sm, cfg, ms, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/media"+tt.query, nil)
			req.AddCookie(addSessionCookie(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
			if tt.query != "" {
				if gotFilter.SetID != nil && *gotFilter.SetID != *tt.filter.SetID {
					t.Fatalf("unexpected set_id")
				}
			}
		})
	}
}

func TestServer_MediaDetail(t *testing.T) {
	tests := []struct {
		name             string
		id               string
		result           *service.MediaDetail
		err              error
		wantCode         int
		wantMedia        bool
		wantResumeFrom   float64
		wantProgressNil  bool
	}{
		{
			name:           "ok with progress",
			id:             "42",
			result:         &service.MediaDetail{Media: &model.Media{ID: 42, FileName: "a.mp4"}, Progress: &model.PlaybackProgress{UserID: 1, MediaID: 42, PositionSeconds: 77}},
			err:            nil,
			wantCode:       http.StatusOK,
			wantMedia:      true,
			wantResumeFrom: 77,
		},
		{"ok without progress", "42", &service.MediaDetail{Media: &model.Media{ID: 42, FileName: "a.mp4"}}, nil, http.StatusOK, true, 0, true},
		{"invalid id", "abc", nil, nil, http.StatusBadRequest, false, 0, false},
		{"not found", "7", nil, nil, http.StatusNotFound, false, 0, false},
		{"service error", "7", nil, errors.New("boom"), http.StatusInternalServerError, false, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &service.MockMediaService{
				GetMediaDetailFunc: func(ctx context.Context, mediaID, userID int64) (*service.MediaDetail, error) {
					return tt.result, tt.err
				},
			}
			store := buildSessionStore(1)
			sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
			cfg := &internal.Config{SessionTimeoutHours: 24}
			srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/media/%s", tt.id), nil)
			req.AddCookie(addSessionCookie(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
			if tt.wantCode != http.StatusOK {
				return
			}
			var resp struct {
				Media    *model.Media              `json:"media"`
				Progress *model.PlaybackProgress  `json:"progress"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}
			if tt.wantMedia && resp.Media == nil {
				t.Fatal("expected media in response")
			}
			var gotResume float64
			if resp.Progress != nil {
				gotResume = resp.Progress.PositionSeconds
			}
			if gotResume != tt.wantResumeFrom {
				t.Fatalf("expected resume_from %v, got %v", tt.wantResumeFrom, gotResume)
			}
		})
	}
}

func TestServer_Favorite(t *testing.T) {
	ms := &service.MockMediaService{
		ToggleFavoriteFunc: func(ctx context.Context, userID, mediaID int64) (bool, error) {
			return true, nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/media/5/favorite", strings.NewReader(`{}`))
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var resp map[string]bool
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp["favorite"] {
		t.Fatal("expected favorite true")
	}
}

func TestServer_AddTag(t *testing.T) {
	ms := &service.MockMediaService{
		AssignTagFunc: func(ctx context.Context, mediaID, userID int64, tagName string) error {
			if tagName == "fail" {
				return errors.New("boom")
			}
			return nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	t.Run("add tag", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/media/1/tags", strings.NewReader(`{"tag":"rock"}`))
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("service error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/media/1/tags", strings.NewReader(`{"tag":"fail"}`))
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})

	t.Run("missing tag", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/media/1/tags", strings.NewReader(`{"tag":""}`))
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})
}

func TestServer_RemoveTag(t *testing.T) {
	ms := &service.MockMediaService{
		RemoveTagFunc: func(ctx context.Context, mediaID, userID int64, tagName string) error {
			return nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/media/1/tags/rock", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestServer_SoftDelete(t *testing.T) {
	ms := &service.MockMediaService{
		SoftDeleteMediaFunc: func(ctx context.Context, mediaID, userID int64) error {
			return nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/media/99", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestServer_Restore(t *testing.T) {
	ms := &service.MockMediaService{
		RestoreMediaFunc: func(ctx context.Context, mediaID, userID int64) error {
			return nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/media/99/restore", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestServer_SoftDelete_Forbidden(t *testing.T) {
	ms := &service.MockMediaService{
		SoftDeleteMediaFunc: func(ctx context.Context, mediaID, userID int64) error {
			return service.ErrForbidden
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/media/99", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestServer_Restore_Forbidden(t *testing.T) {
	ms := &service.MockMediaService{
		RestoreMediaFunc: func(ctx context.Context, mediaID, userID int64) error {
			return service.ErrForbidden
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/media/99/restore", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

// ------------------------------------------------------------------
// Notes
// ------------------------------------------------------------------

func TestServer_Notes(t *testing.T) {
	ms := &service.MockMediaService{
		GetNoteFunc: func(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
			if mediaID == 1 {
				return &model.Note{MediaID: 1, UserID: userID, Content: "hello"}, nil
			}
			return nil, nil
		},
		UpsertNoteFunc: func(ctx context.Context, note *model.Note) error {
			return nil
		},
		DeleteNoteFunc: func(ctx context.Context, mediaID, userID int64) error {
			return nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	t.Run("get note", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/media/1/notes", nil)
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("get no note", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/media/2/notes", nil)
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rr.Code)
		}
	})

	t.Run("upsert", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/media/1/notes", strings.NewReader(`{"content":"hi"}`))
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/media/1/notes", nil)
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

// ------------------------------------------------------------------
// Progress
// ------------------------------------------------------------------

func TestServer_Progress(t *testing.T) {
	var called bool
	ps := &service.MockProgressService{
		UpdateProgressFunc: func(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error {
			called = true
			return nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, nil, nil, ps, nil)

	t.Run("ok", func(t *testing.T) {
		called = false
		req := httptest.NewRequest(http.MethodPost, "/api/progress", strings.NewReader(`{"media_id":5,"position_seconds":12.3}`))
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
		if !called {
			t.Fatal("expected progress service called")
		}
	})

	t.Run("missing media_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/progress", strings.NewReader(`{"position_seconds":1}`))
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})
}

// ------------------------------------------------------------------
// Share routes
// ------------------------------------------------------------------

func TestServer_Shares(t *testing.T) {
	ms := &service.MockMediaService{
		CreateShareFunc: func(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error) {
			return &model.Share{Token: "abc", MediaID: mediaID}, nil
		},
		ListSharesFunc: func(ctx context.Context, mediaID, userID int64) ([]model.Share, error) {
			return []model.Share{{Token: "abc"}}, nil
		},
		RevokeShareFunc: func(ctx context.Context, token string, userID int64) error {
			return nil
		},
		ValidateShareTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
			return &model.Share{Token: token, MediaID: 1}, nil
		},
		StreamSharedMediaFunc: func(ctx context.Context, token string) (*service.FileResult, error) {
			return &service.FileResult{Path: "", FileName: "x.mp4"}, nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, ShareDefaultExpiryDays: 14}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	t.Run("create share", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/media/1/shares", nil)
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("list shares", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/media/1/shares", nil)
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("revoke share", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/shares/abc", nil)
		req.AddCookie(addSessionCookie(t, store, sm, 1))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("share page public", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("share stream public", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		// file open will fail because path is empty; at least not 501
		if rr.Code == http.StatusNotImplemented {
			t.Fatal("unexpected 501")
		}
	})
}

// ------------------------------------------------------------------
// Admin routes
// ------------------------------------------------------------------

func TestServer_AdminRoutes(t *testing.T) {
	adminUser := &model.User{ID: 1, Username: "admin", IsAdmin: true}
	as := &service.MockAdminService{
		ListTrashFunc: func(ctx context.Context) ([]model.Media, error) {
			return []model.Media{}, nil
		},
		TriggerRescanFunc: func(ctx context.Context) error { return nil },
		ListUsersFunc:     func(ctx context.Context) ([]model.User, error) { return []model.User{*adminUser}, nil },
		CreateUserFunc: func(ctx context.Context, username, password string, isAdmin bool) (*model.User, error) {
			return &model.User{ID: 2, Username: username, IsAdmin: isAdmin}, nil
		},
		DeleteUserFunc:       func(ctx context.Context, id int64) error { return nil },
		ListPermissionsFunc:  func(ctx context.Context) (*service.PermissionsMatrix, error) { return nil, nil },
		GrantPermissionFunc:  func(ctx context.Context, setID, userID int64, role model.Role) error { return nil },
		RevokePermissionFunc: func(ctx context.Context, setID, userID int64) error { return nil },
	}

	store := buildSessionStore(1)
	store.UserRepo.GetUserByIDFunc = func(ctx context.Context, id int64) (*model.User, error) {
		return adminUser, nil
	}
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, nil, as, nil, nil)

	cookie := addSessionCookie(t, store, sm, 1)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"list trash", "GET", "/api/admin/trash", "", http.StatusOK},
		{"rescan", "POST", "/api/admin/rescan", "", http.StatusOK},
		{"list users", "GET", "/api/admin/users", "", http.StatusOK},
		{"create user", "POST", "/api/admin/users", `{"username":"bob","password":"pass","is_admin":false}`, http.StatusOK},
		{"delete user", "DELETE", "/api/admin/users/2", "", http.StatusOK},
		{"list perms", "GET", "/api/admin/permissions", "", http.StatusOK},
		{"grant perm", "POST", "/api/admin/permissions", `{"set_id":1,"user_id":2,"role":"viewer"}`, http.StatusOK},
		{"revoke perm", "DELETE", "/api/admin/permissions", `{"set_id":1,"user_id":2}`, http.StatusOK},
		{"delete self", "DELETE", "/api/admin/users/1", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body *strings.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			req.AddCookie(cookie)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, rr.Code)
			}
		})
	}
}

// ------------------------------------------------------------------
// Sets
// ------------------------------------------------------------------

func TestServer_ListSets(t *testing.T) {
	ms := &service.MockMediaService{
		ListSetsFunc: func(ctx context.Context, userID int64) ([]model.Set, error) {
			return []model.Set{{ID: 1, Name: "music"}}, nil
		},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, buildCountStore(1), nil, sm, cfg, ms, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sets", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var sets []model.Set
	_ = json.Unmarshal(rr.Body.Bytes(), &sets)
	if len(sets) != 1 || sets[0].Name != "music" {
		t.Fatalf("unexpected sets response")
	}
}

// ------------------------------------------------------------------
// Helper types
// ------------------------------------------------------------------

func buildSessionStore(userID int64) *repository.MockStore {
	return &repository.MockStore{
		SessionRepo: repository.MockSessionRepo{
			CreateSessionFunc: func(ctx context.Context, session *model.Session) error { return nil },
			GetSessionByIDFunc: func(ctx context.Context, id string) (*model.Session, error) {
				return &model.Session{ID: id, UserID: userID, ExpiresAt: time.Now().Add(time.Hour)}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
		},
	}
}

func buildCountStore(count int) *repository.MockStore {
	return &repository.MockStore{
		UserRepo: repository.MockUserRepo{
			CountUsersFunc: func(ctx context.Context) (int, error) { return count, nil },
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, Username: "admin", IsAdmin: true}, nil
			},
		},
	}
}

func addSessionCookieStore(t *testing.T, sm *auth.SessionManager, userID int64) *http.Cookie {
	t.Helper()
	id, _ := sm.CreateSession(context.Background(), userID)
	return &http.Cookie{Name: "session", Value: id}
}

// mockPingStore wraps a Store and adds Ping capability.
type mockPingStore struct {
	store repository.Store
	err   error
}

func (m *mockPingStore) CreateUser(ctx context.Context, user *model.User) (int64, error) {
	return m.store.CreateUser(ctx, user)
}
func (m *mockPingStore) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return m.store.GetUserByID(ctx, id)
}
func (m *mockPingStore) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	return m.store.GetUserByUsername(ctx, username)
}
func (m *mockPingStore) ListUsers(ctx context.Context) ([]model.User, error) {
	return m.store.ListUsers(ctx)
}
func (m *mockPingStore) DeleteUser(ctx context.Context, id int64) error {
	return m.store.DeleteUser(ctx, id)
}
func (m *mockPingStore) CountUsers(ctx context.Context) (int, error) {
	return m.store.CountUsers(ctx)
}
func (m *mockPingStore) CreateSet(ctx context.Context, set *model.Set) (int64, error) {
	return m.store.CreateSet(ctx, set)
}
func (m *mockPingStore) GetSetByID(ctx context.Context, id int64) (*model.Set, error) {
	return m.store.GetSetByID(ctx, id)
}
func (m *mockPingStore) ListSets(ctx context.Context) ([]model.Set, error) {
	return m.store.ListSets(ctx)
}
func (m *mockPingStore) UpdateSet(ctx context.Context, set *model.Set) error {
	return m.store.UpdateSet(ctx, set)
}
func (m *mockPingStore) DeleteSet(ctx context.Context, id int64) error {
	return m.store.DeleteSet(ctx, id)
}
func (m *mockPingStore) GrantPermission(ctx context.Context, perm *model.SetPermission) error {
	return m.store.GrantPermission(ctx, perm)
}
func (m *mockPingStore) RevokePermission(ctx context.Context, setID, userID int64) error {
	return m.store.RevokePermission(ctx, setID, userID)
}
func (m *mockPingStore) GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
	return m.store.GetPermission(ctx, setID, userID)
}
func (m *mockPingStore) ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error) {
	return m.store.ListPermissionsBySet(ctx, setID)
}
func (m *mockPingStore) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error) {
	return m.store.ListPermissionsByUser(ctx, userID)
}
func (m *mockPingStore) CreateMedia(ctx context.Context, media *model.Media) (int64, error) {
	return m.store.CreateMedia(ctx, media)
}
func (m *mockPingStore) GetMediaByID(ctx context.Context, id int64) (*model.Media, error) {
	return m.store.GetMediaByID(ctx, id)
}
func (m *mockPingStore) UpdateMedia(ctx context.Context, media *model.Media) error {
	return m.store.UpdateMedia(ctx, media)
}
func (m *mockPingStore) SoftDeleteMedia(ctx context.Context, id int64) error {
	return m.store.SoftDeleteMedia(ctx, id)
}
func (m *mockPingStore) RestoreMedia(ctx context.Context, id int64) error {
	return m.store.RestoreMedia(ctx, id)
}
func (m *mockPingStore) HardDeleteMedia(ctx context.Context, id int64) error {
	return m.store.HardDeleteMedia(ctx, id)
}
func (m *mockPingStore) ListMedia(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
	return m.store.ListMedia(ctx, filter)
}
func (m *mockPingStore) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	return m.store.ListDeletedMedia(ctx)
}
func (m *mockPingStore) IncrementPlayCount(ctx context.Context, id int64) error {
	return m.store.IncrementPlayCount(ctx, id)
}
func (m *mockPingStore) CreateTag(ctx context.Context, name string) (int64, error) {
	return m.store.CreateTag(ctx, name)
}
func (m *mockPingStore) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	return m.store.GetTagByID(ctx, id)
}
func (m *mockPingStore) GetTagByName(ctx context.Context, name string) (*model.Tag, error) {
	return m.store.GetTagByName(ctx, name)
}
func (m *mockPingStore) ListTags(ctx context.Context) ([]model.Tag, error) {
	return m.store.ListTags(ctx)
}
func (m *mockPingStore) DeleteTag(ctx context.Context, id int64) error {
	return m.store.DeleteTag(ctx, id)
}
func (m *mockPingStore) AssignTag(ctx context.Context, mediaID, tagID int64) error {
	return m.store.AssignTag(ctx, mediaID, tagID)
}
func (m *mockPingStore) RemoveTag(ctx context.Context, mediaID, tagID int64) error {
	return m.store.RemoveTag(ctx, mediaID, tagID)
}
func (m *mockPingStore) ListTagsByMedia(ctx context.Context, mediaID int64) ([]model.Tag, error) {
	return m.store.ListTagsByMedia(ctx, mediaID)
}
func (m *mockPingStore) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	return m.store.ToggleFavorite(ctx, userID, mediaID)
}
func (m *mockPingStore) IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	return m.store.IsFavorite(ctx, userID, mediaID)
}
func (m *mockPingStore) ListFavoritesByUser(ctx context.Context, userID int64) ([]model.Favorite, error) {
	return m.store.ListFavoritesByUser(ctx, userID)
}
func (m *mockPingStore) UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error {
	return m.store.UpsertProgress(ctx, progress)
}
func (m *mockPingStore) GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error) {
	return m.store.GetProgress(ctx, userID, mediaID)
}
func (m *mockPingStore) ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error) {
	return m.store.ListProgressByUser(ctx, userID)
}
func (m *mockPingStore) UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error {
	return m.store.UpsertAccumulator(ctx, acc)
}
func (m *mockPingStore) GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
	return m.store.GetAccumulator(ctx, sessionID, mediaID)
}
func (m *mockPingStore) CreateSession(ctx context.Context, session *model.Session) error {
	return m.store.CreateSession(ctx, session)
}
func (m *mockPingStore) GetSessionByID(ctx context.Context, id string) (*model.Session, error) {
	return m.store.GetSessionByID(ctx, id)
}
func (m *mockPingStore) DeleteSession(ctx context.Context, id string) error {
	return m.store.DeleteSession(ctx, id)
}
func (m *mockPingStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return m.store.DeleteExpiredSessions(ctx, now)
}
func (m *mockPingStore) CreateShare(ctx context.Context, share *model.Share) error {
	return m.store.CreateShare(ctx, share)
}
func (m *mockPingStore) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	return m.store.GetShareByToken(ctx, token)
}
func (m *mockPingStore) ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error) {
	return m.store.ListSharesByMedia(ctx, mediaID)
}
func (m *mockPingStore) UseShare(ctx context.Context, token string) error {
	return m.store.UseShare(ctx, token)
}
func (m *mockPingStore) DeleteShare(ctx context.Context, token string) error {
	return m.store.DeleteShare(ctx, token)
}
func (m *mockPingStore) DeleteExpiredShares(ctx context.Context, now time.Time) error {
	return m.store.DeleteExpiredShares(ctx, now)
}
func (m *mockPingStore) UpsertNote(ctx context.Context, note *model.Note) error {
	return m.store.UpsertNote(ctx, note)
}
func (m *mockPingStore) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	return m.store.GetNote(ctx, mediaID, userID)
}
func (m *mockPingStore) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	return m.store.DeleteNote(ctx, mediaID, userID)
}
func (m *mockPingStore) Ping(ctx context.Context) error {
	return m.err
}
