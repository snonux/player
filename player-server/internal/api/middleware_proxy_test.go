package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snonux/player/internal/auth"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

func TestMiddleware_RequireSession_ProxyAuth(t *testing.T) {
	proxySession := &model.Session{ID: "proxy-user:3", UserID: 3, ExpiresAt: time.Now().Add(time.Hour)}
	pa, err := auth.ParseProxyAuth("127.0.0.1/32", "Remote-User", "")
	if err != nil {
		t.Fatal(err)
	}

	authSvc := &service.MockAuthService{
		AuthenticateProxyUserFunc: func(ctx context.Context, username string) (*model.Session, error) {
			if username != "alice" {
				t.Fatalf("unexpected username %q", username)
			}
			return proxySession, nil
		},
		AuthenticateBearerFunc: func(ctx context.Context, plaintext string) (*model.Session, error) {
			if plaintext != "good-token" {
				return nil, service.ErrInvalidCredentials
			}
			return &model.Session{ID: "api-token:1", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	}

	tests := []struct {
		name        string
		remoteAddr  string
		headerName  string
		headerValue string
		authHeader  string
		wantCode    int
		wantSession string
	}{
		{
			name:        "trusted proxy maps header",
			remoteAddr:  "127.0.0.1:54321",
			headerName:  "Remote-User",
			headerValue: "alice",
			wantCode:    http.StatusOK,
			wantSession: "proxy-user:3",
		},
		{
			name:        "untrusted client cannot spoof header",
			remoteAddr:  "203.0.113.50:1234",
			headerName:  "Remote-User",
			headerValue: "alice",
			wantCode:    http.StatusUnauthorized,
		},
		{
			name:       "trusted without header still unauthorized",
			remoteAddr: "127.0.0.1:1",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:        "bearer still wins over proxy header",
			remoteAddr:  "127.0.0.1:1",
			headerName:  "Remote-User",
			headerValue: "alice",
			authHeader:  "Bearer good-token",
			wantCode:    http.StatusOK,
			wantSession: "api-token:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := NewMiddleware(authSvc, nil, pa)
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sess, ok := r.Context().Value(sessionCtxKey).(*model.Session)
				if !ok || sess == nil {
					t.Fatal("expected session in context")
				}
				if sess.ID != tt.wantSession {
					t.Fatalf("expected session %q, got %q", tt.wantSession, sess.ID)
				}
				w.WriteHeader(http.StatusOK)
			})
			handler := mw.RequireSession(inner)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/media", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.headerName != "" {
				req.Header.Set(tt.headerName, tt.headerValue)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, rr.Code)
			}
		})
	}
}

func TestMiddleware_RequireSession_ProxyAuthDisabledIgnoresHeader(t *testing.T) {
	authSvc := &service.MockAuthService{
		AuthenticateProxyUserFunc: func(ctx context.Context, username string) (*model.Session, error) {
			t.Fatal("proxy auth must not run when disabled")
			return nil, nil
		},
	}
	mw := NewMiddleware(authSvc, nil, nil)
	handler := mw.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Remote-User", "alice")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}
