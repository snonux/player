package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/paul/kiss-media-player/internal"
	"github.com/paul/kiss-media-player/internal/auth"
	"github.com/paul/kiss-media-player/internal/clock"
	"github.com/paul/kiss-media-player/internal/model"
	"github.com/paul/kiss-media-player/internal/repository"
)

func newTestServer(t *testing.T, store repository.Store, hasher auth.Hasher, sm *auth.SessionManager, cfg *internal.Config) *Server {
	t.Helper()
	return NewServer(store, hasher, sm, cfg)
}

func TestMiddleware_BootstrapRedirect(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		userCount  int
		userErr    error
		wantCode   int
		wantLoc    string
		wantBody   string
	}{
		{"public bootstrap html", "/bootstrap.html", 0, nil, http.StatusOK, "", ""},
		{"public api bootstrap", "/api/bootstrap", 0, nil, http.StatusOK, "", ""},
		{"public login html", "/login.html", 0, nil, http.StatusOK, "", ""},
		{"public api login", "/api/login", 0, nil, http.StatusOK, "", ""},
		{"public healthz", "/healthz", 0, nil, http.StatusOK, "", ""},
		{"public readyz", "/readyz", 0, nil, http.StatusOK, "", ""},
		{"protected no users", "/", 0, nil, http.StatusTemporaryRedirect, "/bootstrap.html", ""},
		{"protected users exist", "/", 1, nil, http.StatusOK, "", ""},
		{"count error", "/", 0, errors.New("boom"), http.StatusInternalServerError, "", ""},
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
					if id == "abc" {
						return tt.session, tt.sessErr
					}
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
				// For "session but nil user" and "db error" we still want a valid session in context.
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

type staticHasher struct {
	fixed string
}

func (h *staticHasher) Hash(password string) (string, error) { return h.fixed, nil }
func (h *staticHasher) Compare(hash, password string) error {
	if hash == h.fixed && password == "correct" {
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
		srv := newTestServer(t, store, hasher, sm, cfg)

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
		cookies := rr.Result().Cookies()
		var sessCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "session" {
				sessCookie = c
				break
			}
		}
		if sessCookie == nil {
			t.Fatal("expected session cookie")
		}
		if !sessCookie.HttpOnly || !sessCookie.Secure || sessCookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("unexpected cookie attrs: HttpOnly=%v Secure=%v SameSite=%v", sessCookie.HttpOnly, sessCookie.Secure, sessCookie.SameSite)
		}
	})

	t.Run("bootstrap already complete", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{
				CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil },
			},
		}
		srv := newTestServer(t, store, hasher, nil, cfg)
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
		srv := newTestServer(t, store, hasher, nil, cfg)
		req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader([]byte(`{"username":""}`)))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		srv := newTestServer(t, nil, hasher, nil, cfg)
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
		srv := newTestServer(t, store, hasher, sm, cfg)
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
		cookies := rr.Result().Cookies()
		var sessCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "session" {
				sessCookie = c
				break
			}
		}
		if sessCookie == nil {
			t.Fatal("expected session cookie after login")
		}
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
		srv := newTestServer(t, store, hasher, nil, cfg)
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
		srv := newTestServer(t, store, hasher, nil, cfg)
		body := `{"username":"nobody","password":"pass"}`
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(body)))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
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
		srv := newTestServer(t, store, nil, sm, cfg)

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
		cookies := rr.Result().Cookies()
		var sessCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "session" {
				sessCookie = c
				break
			}
		}
		if sessCookie == nil || sessCookie.MaxAge != -1 {
			t.Fatal("expected cleared session cookie")
		}
	})

	t.Run("no cookie logout", func(t *testing.T) {
		store := &repository.MockStore{UserRepo: repository.MockUserRepo{CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil }}}
		srv := newTestServer(t, store, nil, nil, cfg)
		req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})
}

func TestServer_Healthz(t *testing.T) {
	cfg := &internal.Config{}
	srv := newTestServer(t, &repository.MockStore{}, nil, nil, cfg)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestServer_Readyz(t *testing.T) {
	cfg := &internal.Config{}
	t.Run("ping ok", func(t *testing.T) {
		store := &repository.MockStore{
			UserRepo: repository.MockUserRepo{CountUsersFunc: func(ctx context.Context) (int, error) { return 1, nil }},
		}
		store2 := &mockPingStore{store: store, err: nil}
		srv := newTestServer(t, store2, nil, nil, cfg)
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
		srv := newTestServer(t, store2, nil, nil, cfg)
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}
	})
}

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
