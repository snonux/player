package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/service"
)

func TestShareCredential_QueryParameterBeforeCookie(t *testing.T) {
	tests := []struct {
		name, target, cookie, want string
	}{
		{"none", "/s/tok/stream", "", ""},
		{"cookie", "/s/tok/stream", "c", "c"},
		{"parameter", "/s/tok/stream?view=q", "", "q"},
		{"parameter wins", "/s/tok/stream?view=q", "c", "q"},
		{"empty parameter falls back to the cookie", "/s/tok/stream?view=", "c", "c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodHead, tt.target, nil)
			req.SetPathValue("token", "tok")
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: shareViewCookie, Value: tt.cookie})
			}
			got := shareAccess(req)
			if got.Credential != tt.want || got.Token != "tok" || !got.Probe {
				t.Errorf("shareAccess = %+v, want credential %q for a probe of tok", got, tt.want)
			}
		})
	}
}

// The viewing cookie follows SECURE_COOKIES, is sent only for a viewing the
// request opened, and public share answers carry the privacy headers.
func TestShareViewingCookie(t *testing.T) {
	path := makeTempFile(t, "shared media")
	now := time.Now()
	tests := []struct {
		name    string
		secure  bool
		viewing service.ShareViewing
		want    []string // substrings of Set-Cookie; nil: no cookie at all
	}{
		{"opened, secure", true, service.ShareViewing{Credential: "123.abc", ExpiresAt: now.Add(time.Hour), Opened: true},
			[]string{"share_view=123.abc", "Path=/s/abc", "HttpOnly", "Secure", "SameSite=Lax", "Max-Age="}},
		{"opened, plain http", false, service.ShareViewing{Credential: "123.abc", ExpiresAt: now.Add(time.Hour), Opened: true},
			[]string{"share_view=123.abc", "Path=/s/abc", "HttpOnly", "SameSite=Lax"}},
		{"existing viewing", true, service.ShareViewing{Credential: "123.abc", ExpiresAt: now.Add(time.Hour)}, nil},
		{"no viewing", true, service.ShareViewing{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &service.MockMediaService{
				StreamSharedMediaFunc: func(context.Context, service.ShareAccess) (*service.FileResult, service.ShareViewing, error) {
					return &service.FileResult{Path: path, FileName: "a.mp4"}, tt.viewing, nil
				},
			}
			cfg := &internal.Config{SessionTimeoutHours: 24, SecureCookies: tt.secure}
			srv := newTestServer(t, buildSessionStore(1), nil, nil, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/s/abc/stream", nil))

			if rr.Code != http.StatusOK || rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("status %d, headers %v", rr.Code, rr.Header())
			}
			setCookie := rr.Header().Get("Set-Cookie")
			if tt.want == nil {
				if setCookie != "" {
					t.Fatalf("Set-Cookie = %q, want none", setCookie)
				}
				return
			}
			for _, w := range tt.want {
				if !strings.Contains(setCookie, w) {
					t.Errorf("Set-Cookie %q lacks %q", setCookie, w)
				}
			}
			if !tt.secure && strings.Contains(setCookie, "Secure") {
				t.Errorf("Set-Cookie %q is Secure although SECURE_COOKIES is off", setCookie)
			}
		})
	}
}
