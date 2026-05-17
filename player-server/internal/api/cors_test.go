package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/repository"
)

func TestServer_CORSDisabled(t *testing.T) {
	srv := newCORSTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS origin header, got %q", got)
	}
	if got := rr.Header().Get("Vary"); got != "" {
		t.Fatalf("expected no Vary header, got %q", got)
	}
}

func TestServer_CORSAllowedOrigin(t *testing.T) {
	const origin = "http://localhost:5173"
	srv := newCORSTestServer(t, []string{origin})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media", nil)
	req.Header.Set("Origin", origin)
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	assertCORSHeaders(t, rr.Header(), origin)
}

func TestServer_CORSDisallowedOrigin(t *testing.T) {
	srv := newCORSTestServer(t, []string{"https://player.example.com"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS origin header, got %q", got)
	}
	if got := rr.Header().Get("Vary"); got != "" {
		t.Fatalf("expected no Vary header, got %q", got)
	}
}

func TestServer_CORSPreflightAPIV1Media(t *testing.T) {
	const origin = "http://localhost:5173"
	srv := newCORSTestServer(t, []string{origin})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/media", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	assertCORSHeaders(t, rr.Header(), origin)
}

func newCORSTestServer(t *testing.T, allowedOrigins []string) *Server {
	t.Helper()
	return newTestServer(
		t,
		&repository.MockStore{},
		nil,
		nil,
		&internal.Config{CORSAllowedOrigins: allowedOrigins},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func assertCORSHeaders(t *testing.T, h http.Header, origin string) {
	t.Helper()
	if got := h.Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Access-Control-Allow-Origin: expected %q, got %q", origin, got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got != corsAllowMethods {
		t.Fatalf("Access-Control-Allow-Methods: expected %q, got %q", corsAllowMethods, got)
	}
	if got := h.Get("Access-Control-Allow-Headers"); got != corsAllowHeaders {
		t.Fatalf("Access-Control-Allow-Headers: expected %q, got %q", corsAllowHeaders, got)
	}
	if got := h.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials: expected true, got %q", got)
	}
	if got := h.Get("Vary"); got != "Origin" {
		t.Fatalf("Vary: expected %q, got %q", "Origin", got)
	}
}
