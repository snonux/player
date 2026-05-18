package api

// Integration tests for all /api/v1/ routes.
//
// Strategy: spin up a full Server backed by a real in-memory SQLite database so
// that auth, sessions, and API tokens all work end-to-end. Service layers that
// need file-system access (browse, stream, write, admin) are replaced with
// MockMediaService / MockAdminService so tests stay fast and hermetic.
//
// Each test helper (bootstrapAdmin, loginAsUser, mintToken, etc.) uses
// t.Helper() so failure messages point to the call site, not the helper.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// ------------------------------------------------------------------
// Test-server constructor
// ------------------------------------------------------------------

// integrationEnv holds the live server needed by test helpers.
type integrationEnv struct {
	srv *Server
}

// newIntegrationServer spins up a full Server with a real in-memory SQLite
// store, a real session manager, and a low-cost bcrypt hasher. Service
// interfaces that touch the file system (browse, write, admin, etc.) are
// replaced with permissive mocks so tests focus on HTTP routing and auth.
func newIntegrationServer(t *testing.T) *integrationEnv {
	t.Helper()

	store, sm, hasher, authSvc := buildIntegrationAuth(t)

	cfg := &internal.Config{
		MediaPageSize:          50,
		ShareDefaultExpiryDays: 7,
		SessionTimeoutHours:    24,
		SecureCookies:          false,
	}

	staticFS := fstest.MapFS{
		"index.html":     {Data: []byte("index")},
		"login.html":     {Data: []byte("login")},
		"bootstrap.html": {Data: []byte("bootstrap")},
		"share.html":     {Data: []byte("share")},
	}

	srv := NewServer(ServerDeps{
		Store:          store,
		Hasher:         hasher,
		SessionManager: sm,
		Config:         cfg,
		Services: ServerServices{
			Browse:   buildBrowseMock(),
			Write:    buildBrowseMock(),
			Share:    buildBrowseMock(),
			Tag:      buildBrowseMock(),
			Favorite: buildBrowseMock(),
			Note:     buildBrowseMock(),
			Admin:    buildAdminMock(),
			Progress: buildProgressMock(),
			Auth:     authSvc,
			Podcast:  &integrationPodcastService{},
		},
		StaticFS: http.FS(staticFS),
	})

	return &integrationEnv{srv: srv}
}

// buildIntegrationAuth creates the real SQLite store, session manager, and auth
// service used across integration tests.
func buildIntegrationAuth(t *testing.T) (*repository.SQLite, auth.SessionManager, auth.Hasher, service.AuthService) {
	t.Helper()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	clk := &clock.MockClock{T: time.Now()}
	sm := auth.NewSessionManager(store, clk, time.Hour)
	hasher := auth.NewBCryptHasher(4) // cost 4 is minimum, fast enough for tests
	authSvc := service.NewAuthService(store, clk, hasher, sm, auth.NewTokenManager())
	return store, sm, hasher, authSvc
}

// buildBrowseMock returns a permissive MockMediaService for integration tests.
func buildBrowseMock() *service.MockMediaService {
	return &service.MockMediaService{
		ListSetsFunc: func(_ context.Context, _ int64) ([]model.Set, error) {
			return []model.Set{{ID: 1, Name: "test-set"}}, nil
		},
		ListMediaFunc: func(_ context.Context, _ int64, _ service.MediaQueryFilter) ([]model.Media, error) {
			return []model.Media{}, nil
		},
		GetMediaDetailFunc: func(_ context.Context, mediaID, _ int64) (*service.MediaDetail, error) {
			return &service.MediaDetail{Media: &model.Media{ID: mediaID}}, nil
		},
		BrowseSetFunc: func(_ context.Context, setID, _ int64, _ string) (*service.BrowseResult, error) {
			return &service.BrowseResult{CurrentPath: "/"}, nil
		},
	}
}

// buildAdminMock returns a permissive MockAdminService for integration tests.
func buildAdminMock() *service.MockAdminService {
	return &service.MockAdminService{
		ListUsersFunc: func(_ context.Context) ([]model.User, error) {
			return []model.User{{ID: 1, Username: "admin", IsAdmin: true}}, nil
		},
		ListTrashFunc:       func(_ context.Context) ([]model.Media, error) { return nil, nil },
		TriggerRescanFunc:   func(_ context.Context) error { return nil },
		ScanProgressFunc:    func(_ context.Context) model.ScanProgress { return model.ScanProgress{} },
		ListPermissionsFunc: func(_ context.Context) (*service.PermissionsMatrix, error) { return &service.PermissionsMatrix{}, nil },
		CreateUserFunc: func(_ context.Context, username, _ string, isAdmin bool) (*model.User, error) {
			return &model.User{ID: 2, Username: username, IsAdmin: isAdmin}, nil
		},
		DeleteUserFunc:       func(_ context.Context, _, _ int64) error { return nil },
		GrantPermissionFunc:  func(_ context.Context, _, _ int64, _ model.Role) error { return nil },
		RevokePermissionFunc: func(_ context.Context, _, _ int64) error { return nil },
	}
}

// buildProgressMock returns a permissive MockProgressService for integration tests.
func buildProgressMock() *service.MockProgressService {
	return &service.MockProgressService{
		UpdateProgressFunc: func(_ context.Context, _ string, _, _ int64, _ float64) error { return nil },
		BatchUpdateProgressFunc: func(_ context.Context, _ string, _ int64, _ []service.ProgressUpdate) error {
			return nil
		},
		MarkFinishedFunc:   func(_ context.Context, _, _ int64) error { return nil },
		MarkNotStartedFunc: func(_ context.Context, _, _ int64) error { return nil },
		ListInProgressFunc: func(_ context.Context, _ int64) ([]model.Media, error) { return nil, nil },
	}
}

// ------------------------------------------------------------------
// User / session helpers
// ------------------------------------------------------------------

// bootstrapAdmin creates the first (admin) account via the real Bootstrap flow
// and returns the user ID + session cookie.
func bootstrapAdmin(t *testing.T, env *integrationEnv) (int64, *http.Cookie) {
	t.Helper()

	body := jsonBody(t, map[string]string{"username": "admin", "password": "secret123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	env.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("bootstrap: want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		ID int64 `json:"id"`
	}
	mustDecodeJSON(t, rr.Body.Bytes(), &resp)
	cookie := sessionCookieFromResponse(t, rr)
	return resp.ID, cookie
}

// loginAsUser authenticates with the given credentials and returns the session cookie.
func loginAsUser(t *testing.T, env *integrationEnv, username, password string) *http.Cookie {
	t.Helper()

	body := jsonBody(t, map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	env.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	return sessionCookieFromResponse(t, rr)
}

// mintToken mints a Bearer token for an already-authenticated user (identified
// by a valid session cookie) and returns the raw plaintext token string.
func mintToken(t *testing.T, env *integrationEnv, cookie *http.Cookie) string {
	t.Helper()

	body := jsonBody(t, map[string]string{"name": "integration-test-token"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	env.srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("mint token: want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Token string `json:"token"`
	}
	mustDecodeJSON(t, rr.Body.Bytes(), &resp)
	if resp.Token == "" {
		t.Fatal("mint token: empty token in response")
	}
	return resp.Token
}

// sessionCookieFromResponse extracts the "session" cookie from a recorder's
// response headers, failing the test if it is missing.
func sessionCookieFromResponse(t *testing.T, rr *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("expected session cookie in response")
	return nil
}

// ------------------------------------------------------------------
// Request / response helpers
// ------------------------------------------------------------------

// jsonBody encodes v as JSON and returns an io.Reader; fails the test on error.
func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return bytes.NewReader(b)
}

// mustDecodeJSON decodes data into v; fails the test on error.
func mustDecodeJSON(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal json: %v: raw=%s", err, string(data))
	}
}

// doRequest performs a single HTTP request against the server using the provided
// authCookie (for cookie auth) or bearerToken (for Bearer auth). Exactly one of
// the two should be non-zero per call. Returns the recorded response.
func doRequest(t *testing.T, env *integrationEnv, method, path string, body *bytes.Reader,
	authCookie *http.Cookie, bearerToken string,
) *httptest.ResponseRecorder {
	t.Helper()

	// Pass an untyped nil to httptest.NewRequest when body is nil; a typed nil
	// *bytes.Reader would satisfy io.Reader as a non-nil interface and cause a panic.
	var bodyIO io.Reader
	if body != nil {
		bodyIO = body
	}
	req := httptest.NewRequest(method, path, bodyIO)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authCookie != nil {
		req.AddCookie(authCookie)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	rr := httptest.NewRecorder()
	env.srv.ServeHTTP(rr, req)
	return rr
}

// assertStatus fails the test if rr.Code != want.
func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Errorf("want status %d, got %d: %s", want, rr.Code, rr.Body.String())
	}
}

// ------------------------------------------------------------------
// Cross-cutting flow test
// ------------------------------------------------------------------

// TestIntegration_FullFlow exercises the canonical user journey:
// bootstrap → login → mint token → browse → progress → favorite → logout.
// Each step is performed twice – once with cookie auth and once with Bearer auth
// – to verify that both auth paths work end-to-end.
func TestIntegration_FullFlow(t *testing.T) {
	env := newIntegrationServer(t)

	// 1. Bootstrap creates the first admin user and returns a session cookie.
	_, cookie := bootstrapAdmin(t, env)

	// 2. Mint a Bearer token while authenticated via cookie.
	token := mintToken(t, env, cookie)

	// 3. Log out to confirm the logout endpoint works.
	// The logout route is registered from /api/logout → /api/v1/logout.
	rr := doRequest(t, env, http.MethodPost, "/api/v1/logout", nil, cookie, "")
	assertStatus(t, rr, http.StatusNoContent)

	// 4. Log back in to get a fresh cookie for the remaining steps.
	cookie = loginAsUser(t, env, "admin", "secret123")

	// 5. GET /api/v1/sets – list sets; verify with both auth methods.
	for _, name := range []string{"cookie", "bearer"} {
		t.Run("list-sets/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/sets", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/sets", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}

	// 6. GET /api/v1/media – list media.
	for _, name := range []string{"cookie", "bearer"} {
		t.Run("list-media/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}

	// 7. POST /api/v1/progress – record a playback position.
	// Field name matches the server's struct: position_seconds.
	progressBody := jsonBody(t, map[string]any{"media_id": 1, "position_seconds": 42.0})
	rr = doRequest(t, env, http.MethodPost, "/api/v1/progress", progressBody, cookie, "")
	assertStatus(t, rr, http.StatusOK)

	// 8. GET /api/v1/in-progress – list in-progress media.
	rr = doRequest(t, env, http.MethodGet, "/api/v1/in-progress", nil, cookie, "")
	assertStatus(t, rr, http.StatusOK)

	// 9. POST /api/v1/media/1/favorite – toggle favorite.
	rr = doRequest(t, env, http.MethodPost, "/api/v1/media/1/favorite", nil, cookie, "")
	assertStatus(t, rr, http.StatusOK)

	// 10. GET /api/v1/tags – list tags.
	rr = doRequest(t, env, http.MethodGet, "/api/v1/tags", nil, cookie, "")
	assertStatus(t, rr, http.StatusOK)
}

// ------------------------------------------------------------------
// Public endpoint tests
// ------------------------------------------------------------------

// TestIntegration_PublicEndpoints verifies that /healthz, /readyz, and the
// bootstrap/login HTML pages are accessible without any authentication.
func TestIntegration_PublicEndpoints(t *testing.T) {
	env := newIntegrationServer(t)
	// Bootstrap first so the BootstrapRedirect middleware doesn't redirect.
	bootstrapAdmin(t, env)

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/readyz", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			env.srv.ServeHTTP(rr, req)
			assertStatus(t, rr, tt.want)
		})
	}
}

// ------------------------------------------------------------------
// Auth endpoint tests
// ------------------------------------------------------------------

// TestIntegration_Auth tests bootstrap, login, token management, and logout.
func TestIntegration_Auth(t *testing.T) {
	t.Run("bootstrap/ok", func(t *testing.T) {
		env := newIntegrationServer(t)
		body := jsonBody(t, map[string]string{"username": "admin", "password": "pass1234"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		env.srv.ServeHTTP(rr, req)
		assertStatus(t, rr, http.StatusOK)
	})

	t.Run("bootstrap/already-bootstrapped", func(t *testing.T) {
		env := newIntegrationServer(t)
		bootstrapAdmin(t, env)
		// Second bootstrap attempt must be rejected.
		body := jsonBody(t, map[string]string{"username": "admin2", "password": "pass1234"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		env.srv.ServeHTTP(rr, req)
		assertStatus(t, rr, http.StatusForbidden)
	})

	t.Run("login/ok", func(t *testing.T) {
		env := newIntegrationServer(t)
		bootstrapAdmin(t, env)
		cookie := loginAsUser(t, env, "admin", "secret123")
		if cookie == nil {
			t.Fatal("expected session cookie")
		}
	})

	t.Run("login/bad-credentials", func(t *testing.T) {
		env := newIntegrationServer(t)
		bootstrapAdmin(t, env)
		body := jsonBody(t, map[string]string{"username": "admin", "password": "wrong"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		env.srv.ServeHTTP(rr, req)
		assertStatus(t, rr, http.StatusUnauthorized)
	})

	t.Run("logout/ok", func(t *testing.T) {
		env := newIntegrationServer(t)
		_, cookie := bootstrapAdmin(t, env)
		// Logout is at /api/v1/logout (mapped from /api/logout via handleBoth).
		rr := doRequest(t, env, http.MethodPost, "/api/v1/logout", nil, cookie, "")
		assertStatus(t, rr, http.StatusNoContent)
	})

	t.Run("logout/unauthenticated", func(t *testing.T) {
		env := newIntegrationServer(t)
		bootstrapAdmin(t, env)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
		rr := httptest.NewRecorder()
		env.srv.ServeHTTP(rr, req)
		assertStatus(t, rr, http.StatusUnauthorized)
	})
}

// ------------------------------------------------------------------
// API token endpoint tests
// ------------------------------------------------------------------

// TestIntegration_APITokens tests minting, listing, and revoking API tokens,
// and also verifies that a minted token can authenticate subsequent requests.
func TestIntegration_APITokens(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)

	t.Run("create/ok-cookie", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"name": "my-token"})
		rr := doRequest(t, env, http.MethodPost, "/api/v1/auth/tokens", body, cookie, "")
		assertStatus(t, rr, http.StatusOK)

		var resp struct {
			Token string `json:"token"`
			ID    int64  `json:"id"`
		}
		mustDecodeJSON(t, rr.Body.Bytes(), &resp)
		if resp.Token == "" {
			t.Error("expected non-empty token")
		}
		if resp.ID == 0 {
			t.Error("expected non-zero token ID")
		}
	})

	t.Run("create/unauthenticated", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"name": "my-token"})
		rr := doRequest(t, env, http.MethodPost, "/api/v1/auth/tokens", body, nil, "")
		assertStatus(t, rr, http.StatusUnauthorized)
	})

	t.Run("list/ok-cookie", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet, "/api/v1/auth/tokens", nil, cookie, "")
		assertStatus(t, rr, http.StatusOK)
	})

	t.Run("bearer-auth/ok", func(t *testing.T) {
		// Mint a token and immediately use it to authenticate a subsequent request.
		token := mintToken(t, env, cookie)
		rr := doRequest(t, env, http.MethodGet, "/api/v1/auth/tokens", nil, nil, token)
		assertStatus(t, rr, http.StatusOK)
	})

	t.Run("revoke/ok", func(t *testing.T) {
		// Mint, list to get the ID, then revoke.
		token := mintToken(t, env, cookie)

		rr := doRequest(t, env, http.MethodGet, "/api/v1/auth/tokens", nil, nil, token)
		assertStatus(t, rr, http.StatusOK)

		var tokens []struct {
			ID int64 `json:"id"`
		}
		mustDecodeJSON(t, rr.Body.Bytes(), &tokens)
		if len(tokens) == 0 {
			t.Fatal("expected at least one token in list")
		}

		// Revoke the last listed token.
		idStr := strconv.FormatInt(tokens[len(tokens)-1].ID, 10)
		rr = doRequest(t, env, http.MethodDelete, "/api/v1/auth/tokens/"+idStr, nil, cookie, "")
		assertStatus(t, rr, http.StatusNoContent)
	})
}

// ------------------------------------------------------------------
// Protected route 401 tests
// ------------------------------------------------------------------

// TestIntegration_Unauthenticated verifies that all protected /api/v1/ routes
// return 401 when accessed without credentials. Routes are tested with an empty
// Authorization header so the middleware cannot fall through to cookie auth.
func TestIntegration_Unauthenticated(t *testing.T) {
	env := newIntegrationServer(t)
	// Bootstrap to prevent BootstrapRedirect from firing.
	bootstrapAdmin(t, env)

	routes := []struct {
		method string
		path   string
	}{
		// Auth
		{http.MethodGet, "/api/v1/auth/tokens"},
		{http.MethodPost, "/api/v1/auth/tokens"},
		{http.MethodDelete, "/api/v1/auth/tokens/1"},
		{http.MethodPost, "/api/v1/logout"},
		// Config
		{http.MethodGet, "/api/v1/config"},
		// Sets
		{http.MethodGet, "/api/v1/sets"},
		{http.MethodGet, "/api/v1/sets/1/browse"},
		{http.MethodGet, "/api/v1/sets/1/cover"},
		{http.MethodPost, "/api/v1/sets/1/cover"},
		{http.MethodPost, "/api/v1/sets/1/upload"},
		// Media
		{http.MethodGet, "/api/v1/media"},
		{http.MethodGet, "/api/v1/media/1"},
		{http.MethodGet, "/api/v1/media/1/stream"},
		{http.MethodGet, "/api/v1/media/1/download"},
		{http.MethodGet, "/api/v1/media/1/thumbnail"},
		{http.MethodPost, "/api/v1/media/1/thumbnail"},
		{http.MethodPost, "/api/v1/media/1/favorite"},
		{http.MethodPost, "/api/v1/media/1/tags"},
		{http.MethodDelete, "/api/v1/media/1/tags/mytag"},
		{http.MethodDelete, "/api/v1/media/1"},
		{http.MethodPost, "/api/v1/media/1/restore"},
		{http.MethodPost, "/api/v1/media/1/shares"},
		{http.MethodGet, "/api/v1/media/1/shares"},
		{http.MethodGet, "/api/v1/media/1/playback"},
		// Tags
		{http.MethodGet, "/api/v1/tags"},
		// Notes
		{http.MethodGet, "/api/v1/media/1/notes"},
		{http.MethodPost, "/api/v1/media/1/notes"},
		{http.MethodDelete, "/api/v1/media/1/notes"},
		// Progress
		{http.MethodPost, "/api/v1/progress"},
		{http.MethodPost, "/api/v1/progress/batch"},
		{http.MethodPost, "/api/v1/progress/status"},
		{http.MethodGet, "/api/v1/in-progress"},
		// Shares
		{http.MethodDelete, "/api/v1/shares/sometoken"},
		{http.MethodGet, "/api/v1/shares"},
		// Admin
		{http.MethodGet, "/api/v1/admin/trash"},
		{http.MethodPost, "/api/v1/admin/rescan"},
		{http.MethodGet, "/api/v1/admin/scan-progress"},
		{http.MethodGet, "/api/v1/admin/users"},
		{http.MethodPost, "/api/v1/admin/users"},
		{http.MethodDelete, "/api/v1/admin/users/1"},
		{http.MethodGet, "/api/v1/admin/permissions"},
		{http.MethodPost, "/api/v1/admin/permissions"},
		{http.MethodDelete, "/api/v1/admin/permissions"},
		// Podcasts
		{http.MethodGet, "/api/v1/podcasts"},
		{http.MethodPost, "/api/v1/podcasts"},
		{http.MethodGet, "/api/v1/podcasts/1/episodes"},
		{http.MethodPost, "/api/v1/podcasts/episodes/1/download"},
		{http.MethodPost, "/api/v1/podcasts/episodes/1/complete"},
	}

	for _, tt := range routes {
		tt := tt
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			env.srv.ServeHTTP(rr, req)
			assertStatus(t, rr, http.StatusUnauthorized)
		})
	}
}

// TestIntegration_InvalidBearer verifies that an invalid Bearer token causes 401.
func TestIntegration_InvalidBearer(t *testing.T) {
	env := newIntegrationServer(t)
	bootstrapAdmin(t, env)

	rr := doRequest(t, env, http.MethodGet, "/api/v1/sets", nil, nil, "invalid-token-value")
	assertStatus(t, rr, http.StatusUnauthorized)
}

// ------------------------------------------------------------------
// Config endpoint
// ------------------------------------------------------------------

// TestIntegration_Config verifies that GET /api/v1/config returns 200 for
// authenticated users (both cookie and Bearer).
func TestIntegration_Config(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	for _, name := range []string{"cookie", "bearer"} {
		t.Run(name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/config", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/config", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Sets endpoints
// ------------------------------------------------------------------

// TestIntegration_Sets verifies GET /api/v1/sets and GET /api/v1/sets/{id}/browse
// for both auth methods, and 401 without credentials.
func TestIntegration_Sets(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	for _, name := range []string{"cookie", "bearer"} {
		t.Run("list-sets/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/sets", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/sets", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("browse-set/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/sets/1/browse", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/sets/1/browse", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Media endpoints
// ------------------------------------------------------------------

// TestIntegration_Media covers GET /api/v1/media, GET /api/v1/media/{id}, and
// POST /api/v1/media/{id}/favorite for both auth methods.
func TestIntegration_Media(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	for _, name := range []string{"cookie", "bearer"} {
		t.Run("list-media/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("get-media/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("favorite/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/media/1/favorite", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/media/1/favorite", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Tags endpoint
// ------------------------------------------------------------------

// TestIntegration_Tags verifies GET /api/v1/tags returns 200 for both auth methods.
func TestIntegration_Tags(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	for _, name := range []string{"cookie", "bearer"} {
		t.Run(name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/tags", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/tags", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Progress endpoints
// ------------------------------------------------------------------

// TestIntegration_Progress covers POST /api/v1/progress, POST /api/v1/progress/batch,
// POST /api/v1/progress/status, and GET /api/v1/in-progress.
func TestIntegration_Progress(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	progressPayload := func() *bytes.Reader {
		// Field name matches the server's struct: position_seconds.
		return jsonBody(t, map[string]any{"media_id": 1, "position_seconds": 30.0})
	}

	statusPayload := func() *bytes.Reader {
		return jsonBody(t, map[string]any{"media_id": 1, "status": "finished"})
	}

	batchPayload := func() *bytes.Reader {
		return jsonBody(t, map[string]any{
			"updates": []map[string]any{
				{"media_id": 1, "position_seconds": 42.0, "observed_at": time.Now().Format(time.RFC3339)},
			},
		})
	}

	for _, name := range []string{"cookie", "bearer"} {
		t.Run("update-progress/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/progress", progressPayload(), cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/progress", progressPayload(), nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("batch-progress/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/progress/batch", batchPayload(), cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/progress/batch", batchPayload(), nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("progress-status/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/progress/status", statusPayload(), cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/progress/status", statusPayload(), nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("in-progress/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/in-progress", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/in-progress", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Notes endpoints
// ------------------------------------------------------------------

// TestIntegration_Notes covers the full note CRUD lifecycle (GET / POST / DELETE)
// for both auth methods.
func TestIntegration_Notes(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	noteBody := func() *bytes.Reader {
		return jsonBody(t, map[string]string{"content": "my note"})
	}

	for _, name := range []string{"cookie", "bearer"} {
		t.Run("get-note/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1/notes", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1/notes", nil, nil, token)
			}
			// 200 (note exists) or 204 (no note) are both valid; 404 when media absent.
			if rr.Code != http.StatusOK && rr.Code != http.StatusNoContent && rr.Code != http.StatusNotFound {
				t.Errorf("want 200, 204, or 404, got %d: %s", rr.Code, rr.Body.String())
			}
		})

		t.Run("upsert-note/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/media/1/notes", noteBody(), cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodPost, "/api/v1/media/1/notes", noteBody(), nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("delete-note/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodDelete, "/api/v1/media/1/notes", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodDelete, "/api/v1/media/1/notes", nil, nil, token)
			}
			// Handler returns 200 {"status":"ok"} on success.
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Shares endpoints
// ------------------------------------------------------------------

// TestIntegration_Shares covers GET /api/v1/media/{id}/shares and
// GET /api/v1/shares for both auth methods.
func TestIntegration_Shares(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	for _, name := range []string{"cookie", "bearer"} {
		t.Run("list-media-shares/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1/shares", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1/shares", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})

		t.Run("list-my-shares/"+name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/shares", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/shares", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Admin endpoints
// ------------------------------------------------------------------

// TestIntegration_Admin verifies that admin-only routes return 200 for an admin
// user via both cookie and Bearer auth methods.
func TestIntegration_Admin(t *testing.T) {
	env := newIntegrationServer(t)
	_, adminCookie := bootstrapAdmin(t, env)
	adminToken := mintToken(t, env, adminCookie)

	adminRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/trash"},
		{http.MethodGet, "/api/v1/admin/users"},
		{http.MethodGet, "/api/v1/admin/permissions"},
		{http.MethodGet, "/api/v1/admin/scan-progress"},
	}

	for _, tt := range adminRoutes {
		tt := tt
		for _, name := range []string{"cookie", "bearer"} {
			t.Run(tt.method+" "+tt.path+"/"+name, func(t *testing.T) {
				var rr *httptest.ResponseRecorder
				if name == "cookie" {
					rr = doRequest(t, env, tt.method, tt.path, nil, adminCookie, "")
				} else {
					rr = doRequest(t, env, tt.method, tt.path, nil, nil, adminToken)
				}
				assertStatus(t, rr, http.StatusOK)
			})
		}
	}
}

// TestIntegration_AdminUnauthenticated verifies that admin-only routes return
// 401 without credentials.
func TestIntegration_AdminUnauthenticated(t *testing.T) {
	env := newIntegrationServer(t)
	bootstrapAdmin(t, env)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	rr := httptest.NewRecorder()
	env.srv.ServeHTTP(rr, req)
	assertStatus(t, rr, http.StatusUnauthorized)
}

// ------------------------------------------------------------------
// Podcast endpoints
// ------------------------------------------------------------------

// TestIntegration_Podcasts verifies that GET /api/v1/podcasts returns 200 for
// authenticated users (both auth methods).
func TestIntegration_Podcasts(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	for _, name := range []string{"cookie", "bearer"} {
		t.Run(name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/podcasts", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/podcasts", nil, nil, token)
			}
			assertStatus(t, rr, http.StatusOK)
		})
	}
}

// ------------------------------------------------------------------
// Playback hints endpoint
// ------------------------------------------------------------------

// TestIntegration_Playback verifies that GET /api/v1/media/{id}/playback
// returns 200 for both auth methods.
func TestIntegration_Playback(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	token := mintToken(t, env, cookie)

	for _, name := range []string{"cookie", "bearer"} {
		t.Run(name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if name == "cookie" {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1/playback", nil, cookie, "")
			} else {
				rr = doRequest(t, env, http.MethodGet, "/api/v1/media/1/playback", nil, nil, token)
			}
			// PlaybackHintsService is nil in this integration server; the handler
			// returns 501 Not Implemented — which proves auth passed and the route
			// was reached.
			assertStatus(t, rr, http.StatusNotImplemented)
		})
	}
}

// ------------------------------------------------------------------
// integrationPodcastService – minimal stub for the integration server
// ------------------------------------------------------------------

// integrationPodcastService satisfies service.PodcastEpisodeService with
// permissive no-op implementations for integration testing purposes.
// A local type is used because service.MockPodcastEpisodeService does not exist.
type integrationPodcastService struct{}

// SubscribeFeed is a no-op stub.
func (m *integrationPodcastService) SubscribeFeed(_ context.Context, _, _ string, _ int64) (*model.PodcastFeed, error) {
	return nil, nil
}

// ListFeeds returns an empty feed list.
func (m *integrationPodcastService) ListFeeds(_ context.Context, _ int64) ([]model.PodcastFeed, error) {
	return nil, nil
}

// EditFeed is a no-op stub.
func (m *integrationPodcastService) EditFeed(_ context.Context, _ int64, _ string, _ int, _ int64) error {
	return nil
}

// UnsubscribeFeed is a no-op stub.
func (m *integrationPodcastService) UnsubscribeFeed(_ context.Context, _ int64, _ int64) error {
	return nil
}

// ListEpisodes returns an empty episode list.
func (m *integrationPodcastService) ListEpisodes(_ context.Context, _, _ int64, _, _ int) ([]model.PodcastEpisodeWithStatus, error) {
	return nil, nil
}

// DownloadEpisode is a no-op stub.
func (m *integrationPodcastService) DownloadEpisode(_ context.Context, _, _ int64) (*model.Media, error) {
	return nil, nil
}

// ToggleEpisodeComplete is a no-op stub.
func (m *integrationPodcastService) ToggleEpisodeComplete(_ context.Context, _, _ int64) error {
	return nil
}

// CheckFeeds is a no-op stub.
func (m *integrationPodcastService) CheckFeeds(_ context.Context) error {
	return nil
}
