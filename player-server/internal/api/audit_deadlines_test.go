package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internal "github.com/snonux/player/internal"
	"github.com/snonux/player/internal/auth"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

// startWithTimeouts serves h like NewGracefulServer does, but with short
// read/write deadlines so a slow transfer outlasts them quickly.
func startWithTimeouts(t *testing.T, h http.Handler, read, write time.Duration) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(h)
	ts.Config.ReadTimeout = read
	ts.Config.WriteTimeout = write
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// TestUpload_SlowBodyOutlastsReadTimeout: an upload slower than the server's
// ReadTimeout must still arrive whole (it used to fail with 400 after 30s).
func TestUpload_SlowBodyOutlastsReadTimeout(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, MaxUploadSizeMB: 10}
	var got int64
	ms := &service.MockMediaService{
		UploadMediaFunc: func(_ context.Context, _, _ int64, _ string, r io.Reader, _ int64) (*model.Media, error) {
			n, _ := io.Copy(io.Discard, r)
			got = n
			return &model.Media{ID: 1}, nil
		},
	}
	srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
	ts := startWithTimeouts(t, srv, 200*time.Millisecond, 5*time.Second)

	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, _ := mw.CreateFormFile("file", "a.mp4")
	_, _ = part.Write(bytes.Repeat([]byte("x"), 4096))
	_ = mw.Close()
	body := form.Bytes()

	pr, pw := io.Pipe()
	go func() {
		// Dribble the body for ~4x the read timeout.
		chunk := len(body)/8 + 1
		for len(body) > 0 {
			n := min(len(body), chunk)
			if _, err := pw.Write(body[:n]); err != nil {
				return
			}
			body = body[n:]
			time.Sleep(100 * time.Millisecond)
		}
		_ = pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sets/1/upload", pr)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d (%s), want 200", resp.StatusCode, b)
	}
	if got != 4096 {
		t.Fatalf("service received %d bytes, want 4096", got)
	}
}

// TestDownload_SlowClientOutlastsWriteTimeout: an attachment download read
// slower than the server's WriteTimeout must not be cut midway.
func TestDownload_SlowClientOutlastsWriteTimeout(t *testing.T) {
	const size = 16 << 20
	path := filepath.Join(t.TempDir(), "big.mp4")
	if err := os.WriteFile(path, bytes.Repeat([]byte("y"), size), 0o600); err != nil {
		t.Fatal(err)
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	ms := &service.MockMediaService{
		DownloadMediaFunc: func(context.Context, int64, int64) (*service.FileResult, error) {
			return &service.FileResult{Path: path, FileName: "big.mp4", FileSize: size}, nil
		},
	}
	srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
	ts := startWithTimeouts(t, srv, 5*time.Second, 300*time.Millisecond)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/media/1/download", nil)
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var n int64
	buf := make([]byte, 1<<20)
	for {
		k, err := resp.Body.Read(buf)
		n += int64(k)
		if err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n != size {
		t.Fatalf("read %d bytes, want %d", n, size)
	}
}

// B5: public JSON endpoints refuse oversized bodies with 413 instead of
// buffering them.
func TestPublicJSONBodiesAreBounded(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	big := `{"username":"a","password":"` + string(bytes.Repeat([]byte("x"), 1<<20)) + `"}`
	for _, path := range []string{"/api/login", "/api/bootstrap"} {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(big))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: status = %d, want 413", path, rr.Code)
		}
	}
}

// B6: state-changing requests that carry the session cookie are refused
// when a browser says they come from another origin or site.
func TestCrossOriginUnsafeRequestsRefused(t *testing.T) {
	store := buildAdminSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, CORSAllowedOrigins: []string{"https://app.example"}}
	srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cookie := sessionCookieForStore(t, store, sm, 1)
	tests := []struct {
		name, method, origin, site string
		refused                    bool
	}{
		{"cross-site POST", http.MethodPost, "https://evil.example", "cross-site", true},
		{"same-site POST", http.MethodPost, "https://sub.example.com", "same-site", true},
		{"cross-origin POST without fetch metadata", http.MethodPost, "https://evil.example", "", true},
		{"cross-site DELETE", http.MethodDelete, "https://evil.example", "cross-site", true},
		{"same-origin POST", http.MethodPost, "", "same-origin", false},
		{"native client POST", http.MethodPost, "", "", false},
		{"trusted CORS origin", http.MethodPost, "https://app.example", "cross-site", false},
		{"cross-site GET", http.MethodGet, "https://evil.example", "cross-site", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/admin/users"
			if tt.method == http.MethodDelete {
				path = "/api/auth/tokens/1"
			}
			req := httptest.NewRequest(tt.method, path, bytes.NewBufferString(`{"username":"x","password":"y"}`))
			req.Header.Set("Content-Type", "text/plain")
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.site != "" {
				req.Header.Set("Sec-Fetch-Site", tt.site)
			}
			req.AddCookie(cookie)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			refused := rr.Code == http.StatusForbidden && strings.Contains(rr.Body.String(), "cross-origin")
			if refused != tt.refused {
				t.Fatalf("status %d body %q, refused=%v want %v", rr.Code, rr.Body.String(), refused, tt.refused)
			}
		})
	}
}

// B15: CORS preflights allow every method the API uses.
func TestCORSPreflightAllowsPutAndPatch(t *testing.T) {
	h := withCORS([]string{"https://app.example"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		req := httptest.NewRequest(http.MethodOptions, "/api/queue", nil)
		req.Header.Set("Origin", "https://app.example")
		req.Header.Set("Access-Control-Request-Method", method)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent || !strings.Contains(rr.Header().Get("Access-Control-Allow-Methods"), method) {
			t.Errorf("%s preflight: %d %q", method, rr.Code, rr.Header().Get("Access-Control-Allow-Methods"))
		}
	}
}

// B16: unknown /api paths answer JSON 404 and wrong methods 405, never the
// app shell with 200.
func TestUnknownAPIPathsAreNotIndexHTML(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	srv := newTestServer(t, store, nil, sm, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cookie := sessionCookieForStore(t, store, sm, 1)
	tests := []struct {
		method, path string
		code         int
	}{
		{http.MethodPost, "/api/v1/nosuch", http.StatusNotFound},
		{http.MethodGet, "/api/nosuch/deeper", http.StatusNotFound},
		{http.MethodPost, "/api/sets", http.StatusMethodNotAllowed},
		{http.MethodPut, "/api/v1/sets", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != tt.code || !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s %s: %d %q %q", tt.method, tt.path, rr.Code, rr.Header().Get("Content-Type"), rr.Body.String())
		}
		if tt.code == http.StatusMethodNotAllowed && !strings.Contains(rr.Header().Get("Allow"), http.MethodGet) {
			t.Errorf("%s %s: Allow = %q", tt.method, tt.path, rr.Header().Get("Allow"))
		}
	}
}
