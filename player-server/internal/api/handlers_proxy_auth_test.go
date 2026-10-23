package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/auth"
	"github.com/snonux/player/internal/clock"
)

func TestAuthStatusPublic(t *testing.T) {
	pa, err := auth.ParseProxyAuth("10.0.0.0/8", "X-User", "https://idp.example/out")
	if err != nil {
		t.Fatal(err)
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	srv := newTestServer(t, store, nil, sm, &internal.Config{ProxyAuth: pa}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	for _, path := range []string{"/api/auth/status", "/api/v1/auth/status"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rr.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["proxy"] != true || body["password"] != true {
			t.Fatalf("%s: unexpected body %#v", path, body)
		}
		proxy, _ := body["proxy_auth"].(map[string]any)
		if proxy["user_header"] != "X-User" || proxy["logout_url"] != "https://idp.example/out" {
			t.Fatalf("%s: proxy_auth %#v", path, proxy)
		}
	}
}

func TestLogoutReturnsProxyLogoutURL(t *testing.T) {
	pa, err := auth.ParseProxyAuth("127.0.0.1/32", "", "https://idp.example/logout")
	if err != nil {
		t.Fatal(err)
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	srv := newTestServer(t, store, nil, sm, &internal.Config{ProxyAuth: pa, SecureCookies: false, SessionTimeoutHours: 24}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(addSessionCookie(t, store, sm, 1))
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with logout_url, got %d body %s", rr.Code, rr.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["logout_url"] != "https://idp.example/logout" {
		t.Fatalf("unexpected body %#v", body)
	}
}
