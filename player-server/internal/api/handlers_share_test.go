package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

// TestCreateShare_UsesInjectedClock pins "now" via a clock.MockClock and
// asserts that handleCreateShare derives expiresAt from the injected clock —
// not from time.Now(). This guards against the previous flakiness where the
// expiry was computed off the wall clock and could drift between assertion
// runs (e.g. when the test goroutine was descheduled).
func TestCreateShare_UsesInjectedClock(t *testing.T) {
	// Pin a deterministic instant well in the past so any accidental
	// time.Now() leak would produce a wildly different expiresAt.
	fixedNow := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockClk := &clock.MockClock{T: fixedNow}

	const expiryDays = 14
	wantExpiresAt := fixedNow.Add(expiryDays * 24 * time.Hour)

	var capturedExpiresAt time.Time
	ms := &service.MockMediaService{
		CreateShareFunc: func(_ context.Context, _, mediaID int64, expiresAt time.Time) (*model.Share, error) {
			capturedExpiresAt = expiresAt
			return &model.Share{Token: "tok", MediaID: mediaID}, nil
		},
	}
	// authSvc satisfies the BootstrapRedirect middleware (CountUsers > 0
	// so requests aren't redirected to /bootstrap.html) and RequireSession
	// indirectly via session validation — no admin check on this route.
	authSvc := &service.MockAuthService{
		CountUsersFunc:  func(context.Context) (int, error) { return 1, nil },
		GetUserByIDFunc: func(_ context.Context, id int64) (*model.User, error) { return &model.User{ID: id}, nil },
	}

	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, mockClk, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, ShareDefaultExpiryDays: expiryDays}

	// Build the Server directly so we can inject the mock clock — the
	// shared newTestServer helper doesn't expose Clock yet, and adding it
	// there would force every existing test to thread an extra arg.
	srv, err := NewServer(ServerDeps{
		Store:          buildCountStore(1),
		SessionManager: sm,
		Config:         cfg,
		Services: ServerServices{
			Media: MediaServices{
				Browse:   ms,
				Write:    ms,
				Share:    ms,
				Tag:      ms,
				Favorite: ms,
				Note:     ms,
			},
			Auth: authSvc,
		},
		StaticFS:      newTestFS(map[string]string{"index.html": "x"}),
		MediaStreamer: service.NewMediaStreamer(nil, ""),
		Clock:         mockClk,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/media/1/shares", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rr.Code, rr.Body.String())
	}
	if !capturedExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("expected expiresAt %v, got %v", wantExpiresAt, capturedExpiresAt)
	}
}

// TestSetSessionCookie_UsesInjectedClock asserts the session-cookie Expires
// field is derived from s.clk.Now(), not time.Now(). We exercise this via
// handleLogin (the public Login route), which calls setSessionCookie on
// success — that's the only handler path that produces a Set-Cookie header
// with a non-empty Expires.
func TestSetSessionCookie_UsesInjectedClock(t *testing.T) {
	fixedNow := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	mockClk := &clock.MockClock{T: fixedNow}

	const sessionHours = 12
	wantExpires := fixedNow.Add(sessionHours * time.Hour)

	authSvc := &service.MockAuthService{
		CountUsersFunc: func(context.Context) (int, error) { return 1, nil },
		LoginFunc: func(_ context.Context, _, _ string) (*service.AuthResult, error) {
			return &service.AuthResult{
				SessionID: "sess-xyz",
				User:      &model.User{ID: 1, Username: "alice", IsAdmin: false},
			}, nil
		},
	}

	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, mockClk, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: sessionHours}

	srv, err := NewServer(ServerDeps{
		Store:          buildCountStore(1),
		SessionManager: sm,
		Config:         cfg,
		Services: ServerServices{
			Auth: authSvc,
		},
		StaticFS:      newTestFS(map[string]string{"index.html": "x"}),
		MediaStreamer: service.NewMediaStreamer(nil, ""),
		Clock:         mockClk,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"username":"alice","password":"pw"}`))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rr.Code, rr.Body.String())
	}

	var sessionCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie in response")
	}
	if !sessionCookie.Expires.Equal(wantExpires) {
		t.Fatalf("expected cookie Expires %v, got %v", wantExpires, sessionCookie.Expires)
	}
}
