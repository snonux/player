package api

import (
	"log/slog"
	"testing"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/service"
)

// TestNewServerWithLogger_ErrorsOnNilConfig verifies that the constructor
// returns an error (rather than panicking) when deps.Config is nil. The
// caller in cmd/player/main.go relies on this to fail gracefully.
func TestNewServerWithLogger_ErrorsOnNilConfig(t *testing.T) {
	srv, err := NewServerWithLogger(ServerDeps{
		Config:   nil,
		StaticFS: nil,
	}, slog.Default())
	if err == nil {
		t.Fatal("expected error for nil Config, got nil")
	}
	if srv != nil {
		t.Fatalf("expected nil Server on error, got %v", srv)
	}
}

// TestNewServerWithLogger_ErrorsOnNilMediaStreamer verifies that the
// constructor refuses to build a Server when deps.MediaStreamer is nil.
// serveFileResult used to fall back to a default streamer at request time,
// which silently hid wiring mistakes and violated DIP. Construction now
// fails fast, mirroring the explicit-deps pattern in podcast/auth services.
func TestNewServerWithLogger_ErrorsOnNilMediaStreamer(t *testing.T) {
	srv, err := NewServerWithLogger(ServerDeps{
		Config:        &internal.Config{},
		MediaStreamer: nil,
	}, slog.Default())
	if err == nil {
		t.Fatal("expected error for nil MediaStreamer, got nil")
	}
	if srv != nil {
		t.Fatalf("expected nil Server on error, got %v", srv)
	}
}

// TestNewServerWithLogger_SucceedsWithMediaStreamer is a happy-path sanity
// check ensuring the new MediaStreamer validation does not reject valid
// dependency sets.
func TestNewServerWithLogger_SucceedsWithMediaStreamer(t *testing.T) {
	srv, err := NewServerWithLogger(ServerDeps{
		Config:        &internal.Config{},
		MediaStreamer: service.NewMediaStreamer(nil, ""),
	}, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil Server")
	}
}

// TestServer_PublicRouteRegistry verifies that the public-route registry on
// the Middleware is populated by Server.routes() — i.e. the new "register
// at declaration time" mechanism actually wires every previously-hardcoded
// public path. If a route is added in server.go without using the
// handlePublic* helpers, this test catches the regression before the
// silent-401/redirect bug bites users in production.
func TestServer_PublicRouteRegistry(t *testing.T) {
	srv, err := NewServerWithLogger(ServerDeps{
		Config:        &internal.Config{},
		MediaStreamer: service.NewMediaStreamer(nil, ""),
	}, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Exact paths that must be public for bootstrap/login/probes to work
	// before any user exists.
	wantExact := []string{
		"/bootstrap.html", "/api/bootstrap", "/api/v1/auth/bootstrap",
		"/login.html", "/api/login", "/api/v1/auth/login",
		"/healthz", "/readyz",
		"/favicon.svg", "/favicon.ico", "/logo.svg", "/logo.png",
		"/manifest.json", "/sw.js",
	}
	for _, p := range wantExact {
		if !srv.mw.isPublic(p) {
			t.Errorf("expected %q to be public, but isPublic returned false", p)
		}
	}

	// Prefixed routes: static asset trees and dynamic share URLs.
	wantPrefixed := []string{
		"/css/site.css",
		"/js/app.js",
		"/images/logo.png",
		"/s/abcdef",
		"/s/abcdef/stream",
	}
	for _, p := range wantPrefixed {
		if !srv.mw.isPublic(p) {
			t.Errorf("expected %q to be public via prefix, but isPublic returned false", p)
		}
	}

	// Negative: an arbitrary protected path must NOT be public.
	if srv.mw.isPublic("/api/media") {
		t.Error("/api/media should not be public")
	}
	if srv.mw.isPublic("/") {
		t.Error("/ should not be public")
	}
}
