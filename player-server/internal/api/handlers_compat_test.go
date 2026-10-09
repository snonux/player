package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
	"codeberg.org/snonux/player/internal/transcode"
)

const (
	compatBody = "0123456789abcdefghij"
	compatETag = `"m5-6-1-v1.mp4-abc"`
)

// compatTestEnv is a Server wired with a fake CompatStreamService plus a
// session cookie for user 1.
type compatTestEnv struct {
	srv    *Server
	cookie *http.Cookie
}

func newCompatTestEnv(t *testing.T, compat service.CompatStreamService) compatTestEnv {
	t.Helper()
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	authSvc := &service.MockAuthService{
		CountUsersFunc:  func(context.Context) (int, error) { return 1, nil },
		GetUserByIDFunc: func(context.Context, int64) (*model.User, error) { return &model.User{ID: 1}, nil },
	}
	services := ServerServices{Auth: authSvc}
	// Assign only a non-nil service: a nil *Mock in the interface field
	// would not be nil for requireService.
	if compat != nil {
		services.Media.Compat = compat
	}
	srv, err := NewServer(ServerDeps{
		Store:          store,
		SessionManager: sm,
		Config:         &internal.Config{},
		Services:       services,
		StaticFS:       newTestFS(map[string]string{"index.html": "index", "login.html": "login"}),
		MediaStreamer:  service.NewMediaStreamer(nil, ""),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return compatTestEnv{srv: srv, cookie: addSessionCookie(t, repository.Store(store), sm, 1)}
}

// compatEnvReturning is an env whose service answers every request with the
// given rendition and error.
func compatEnvReturning(t *testing.T, r *transcode.Rendition, err error) compatTestEnv {
	t.Helper()
	return newCompatTestEnv(t, &service.MockCompatStreamService{
		CompatStreamFunc:       func(context.Context, int64, int64) (*transcode.Rendition, error) { return r, err },
		SharedCompatStreamFunc: func(context.Context, string) (*transcode.Rendition, error) { return r, err },
	})
}

// do performs a GET; authenticated requests carry the session cookie.
func (e compatTestEnv) do(ctx context.Context, path string, authenticated bool, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	for k, v := range header {
		req.Header[k] = v
	}
	if authenticated {
		req.AddCookie(e.cookie)
	}
	rr := httptest.NewRecorder()
	e.srv.ServeHTTP(rr, req)
	return rr
}

// get is an authenticated GET without extra headers.
func (e compatTestEnv) get(path string) *httptest.ResponseRecorder {
	return e.do(context.Background(), path, true, nil)
}

// writeRendition creates a finished rendition file as the cache would.
func writeRendition(t *testing.T) *transcode.Rendition {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m5-6-1-v1.mp4")
	if err := os.WriteFile(path, []byte(compatBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return &transcode.Rendition{Path: path, ContentType: "video/mp4", ETag: strings.Trim(compatETag, `"`)}
}

// errorBody decodes the JSON error body and fails when it is anything but
// the single "error" field (plus the retry fields of a 503).
func errorBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %q", rr.Body.String())
	}
	return body
}

func TestHandleCompatStream_ServesRendition(t *testing.T) {
	rendition := writeRendition(t)
	var gotMedia, gotUser int64
	env := newCompatTestEnv(t, &service.MockCompatStreamService{
		CompatStreamFunc: func(_ context.Context, mediaID, userID int64) (*transcode.Rendition, error) {
			gotMedia, gotUser = mediaID, userID
			return rendition, nil
		},
	})

	// handleBoth must register the legacy and the v1 prefix.
	for _, prefix := range []string{"/api", "/api/v1"} {
		rr := env.get(prefix + "/media/5/compat")
		if rr.Code != http.StatusOK || rr.Body.String() != compatBody {
			t.Fatalf("%s: status %d body %q", prefix, rr.Code, rr.Body.String())
		}
		if gotMedia != 5 || gotUser != 1 {
			t.Errorf("%s: service called with media=%d user=%d", prefix, gotMedia, gotUser)
		}
		h := rr.Header()
		if h.Get("Content-Type") != "video/mp4" || h.Get("Accept-Ranges") != "bytes" || h.Get("ETag") != compatETag {
			t.Errorf("%s: headers = %v", prefix, h)
		}
		// The ETag is the only validator.
		if lm := h.Get("Last-Modified"); lm != "" {
			t.Errorf("%s: unexpected Last-Modified %q", prefix, lm)
		}
	}
}

func TestHandleCompatStream_RangeAndValidators(t *testing.T) {
	env := compatEnvReturning(t, writeRendition(t), nil)
	tests := []struct {
		name       string
		header     http.Header
		wantStatus int
		wantBody   string
	}{
		{"seek", http.Header{"Range": {"bytes=10-14"}}, http.StatusPartialContent, "abcde"},
		{"resume with matching If-Range", http.Header{"Range": {"bytes=0-3"}, "If-Range": {compatETag}}, http.StatusPartialContent, "0123"},
		// A rebuilt rendition has another ETag: the client gets the whole
		// new file instead of a splice of two encodes.
		{"resume with stale If-Range", http.Header{"Range": {"bytes=0-3"}, "If-Range": {`"older-encode"`}}, http.StatusOK, compatBody},
		{"revalidate", http.Header{"If-None-Match": {compatETag}}, http.StatusNotModified, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := env.do(context.Background(), "/api/v1/media/5/compat", true, tt.header)
			if rr.Code != tt.wantStatus || rr.Body.String() != tt.wantBody {
				t.Errorf("status %d body %q, want %d %q", rr.Code, rr.Body.String(), tt.wantStatus, tt.wantBody)
			}
		})
	}
}

// compatErrorCase maps a service error to the expected response.
type compatErrorCase struct {
	name       string
	err        error
	wantStatus int
	wantError  string // exact value of the JSON "error" field
}

func compatErrorCases() []compatErrorCase {
	const secret = "/media/secret/path.avi"
	ffmpegErr := errors.New("ffmpeg transcode: exit status 1: " + secret + ": Invalid data")
	wrap := func(err error) error { return fmt.Errorf("transcode media 5: %w", err) }
	return []compatErrorCase{
		{"unknown id", service.ErrNotFound, http.StatusNotFound, "not found"},
		// Even if a lower layer wraps a sentinel with a path, only the
		// sentinel's own text reaches the client.
		{"not found with path detail", fmt.Errorf("%w: stat %s: no such file or directory", service.ErrNotFound, secret), http.StatusNotFound, "not found"},
		{"no access", fmt.Errorf("%w: path escapes media root", service.ErrForbidden), http.StatusForbidden, "forbidden"},
		{"image media", service.ErrNotTranscodable, http.StatusUnsupportedMediaType, "media type cannot be transcoded"},
		{"native media", service.ErrCompatNotNeeded, http.StatusBadRequest, "media does not need transcoding; play the stream endpoint instead"},
		{"disk full", service.ErrTranscodeNoSpace, http.StatusInsufficientStorage, "insufficient storage for transcode"},
		{"ffmpeg failure", wrap(ffmpegErr), http.StatusInternalServerError, "transcode failed"},
		{"failed recently", wrap(transcode.ErrFailedRecently), http.StatusInternalServerError, "transcode failed"},
		// A job stopped by shutdown or timeout is a server-side failure,
		// not "client closed request" — the client is still there.
		{"job aborted", wrap(transcode.ErrAborted), http.StatusInternalServerError, "transcode failed"},
		{"job context error", wrap(context.Canceled), http.StatusInternalServerError, "transcode failed"},
	}
}

func TestCompatStream_ErrorResponses(t *testing.T) {
	for _, path := range []string{"/api/v1/media/5/compat", "/s/tok/compat"} {
		for _, tt := range compatErrorCases() {
			t.Run(path+" "+tt.name, func(t *testing.T) {
				rr := compatEnvReturning(t, nil, tt.err).do(context.Background(), path, true, nil)
				if rr.Code != tt.wantStatus {
					t.Fatalf("status = %d, want %d (%s)", rr.Code, tt.wantStatus, rr.Body.String())
				}
				body := errorBody(t, rr)
				if len(body) != 1 || body["error"] != tt.wantError {
					t.Errorf("body = %v, want only error=%q", body, tt.wantError)
				}
				// Belt and braces: no server path anywhere in the response.
				if strings.Contains(rr.Body.String(), "/media/") {
					t.Errorf("response leaks a server path: %s", rr.Body.String())
				}
			})
		}
	}
}

func TestHandleCompatStream_InvalidID(t *testing.T) {
	env := compatEnvReturning(t, writeRendition(t), nil)
	for _, path := range []string{"/api/v1/media/abc/compat", "/api/v1/media/0/compat"} {
		if rr := env.get(path); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, rr.Code)
		}
	}
}

func TestCompatStream_RetryResponses(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus string
		wantError  string
	}{
		{"still transcoding", service.ErrTranscodePending, "transcoding", "transcode in progress"},
		{"transcoder busy", service.ErrTranscodeBusy, "busy", "transcoder busy"},
	}
	for _, path := range []string{"/api/v1/media/5/compat", "/s/tok/compat"} {
		for _, tt := range tests {
			t.Run(path+" "+tt.name, func(t *testing.T) {
				rr := compatEnvReturning(t, nil, tt.err).get(path)
				if rr.Code != http.StatusServiceUnavailable || rr.Header().Get("Retry-After") != "5" {
					t.Fatalf("status %d Retry-After %q, want 503 and 5", rr.Code, rr.Header().Get("Retry-After"))
				}
				body := errorBody(t, rr)
				if body["status"] != tt.wantStatus || body["error"] != tt.wantError || body["retry_after_seconds"] != float64(5) {
					t.Errorf("unexpected body %v", body)
				}
			})
		}
	}
}

// A rendition evicted between the service call and the open is not "gone":
// the next request rebuilds it, so the client is told to retry.
func TestHandleCompatStream_EvictedRenditionAsksForRetry(t *testing.T) {
	gone := &transcode.Rendition{Path: filepath.Join(t.TempDir(), "gone.mp4")}
	rr := compatEnvReturning(t, gone, nil).get("/api/v1/media/5/compat")
	if rr.Code != http.StatusServiceUnavailable || rr.Header().Get("Retry-After") != "5" {
		t.Fatalf("status %d Retry-After %q, want 503 and 5", rr.Code, rr.Header().Get("Retry-After"))
	}
	if body := errorBody(t, rr); body["status"] != "transcoding" {
		t.Errorf("unexpected body %v", body)
	}
	if strings.Contains(rr.Body.String(), gone.Path) {
		t.Errorf("response leaks the cache path: %s", rr.Body.String())
	}
}

func TestHandleCompatStream_ClientCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, path := range []string{"/api/v1/media/5/compat", "/s/tok/compat"} {
		rr := compatEnvReturning(t, nil, context.Canceled).do(ctx, path, true, nil)
		if rr.Code != statusClientClosedRequest {
			t.Errorf("%s: status = %d, want %d", path, rr.Code, statusClientClosedRequest)
		}
	}
}

func TestHandleCompatStream_RequiresSession(t *testing.T) {
	called := false
	env := newCompatTestEnv(t, &service.MockCompatStreamService{
		CompatStreamFunc: func(context.Context, int64, int64) (*transcode.Rendition, error) {
			called = true
			return writeRendition(t), nil
		},
	})
	rr := env.do(context.Background(), "/api/v1/media/5/compat", false, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if called {
		t.Error("service must not be reached without a session")
	}
}

func TestHandleCompatStream_NilRenditionAndMissingService(t *testing.T) {
	// The mock's nil-func default returns a nil rendition.
	env := newCompatTestEnv(t, &service.MockCompatStreamService{})
	if rr := env.get("/api/v1/media/5/compat"); rr.Code != http.StatusNotFound {
		t.Errorf("nil rendition: status = %d, want 404", rr.Code)
	}

	// No compat service wired at all.
	env = newCompatTestEnv(t, nil)
	if rr := env.get("/api/v1/media/5/compat"); rr.Code != http.StatusNotImplemented {
		t.Errorf("no service: status = %d, want 501", rr.Code)
	}
	if rr := env.do(context.Background(), "/s/tok/compat", false, nil); rr.Code != http.StatusNotImplemented {
		t.Errorf("no service (share): status = %d, want 501", rr.Code)
	}
}

func TestHandleShareCompatStream(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"ok", nil, http.StatusPartialContent},
		{"unknown token", service.ErrShareNotFound, http.StatusNotFound},
		{"media gone", service.ErrMediaNotFound, http.StatusNotFound},
		{"expired", service.ErrShareExpired, http.StatusGone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendition := writeRendition(t)
			var gotToken string
			env := newCompatTestEnv(t, &service.MockCompatStreamService{
				SharedCompatStreamFunc: func(_ context.Context, token string) (*transcode.Rendition, error) {
					gotToken = token
					return rendition, tt.err
				},
			})
			// Public route: no session cookie.
			rr := env.do(context.Background(), "/s/tok123/compat", false, http.Header{"Range": {"bytes=0-4"}})
			if rr.Code != tt.wantStatus || gotToken != "tok123" {
				t.Fatalf("status = %d token = %q, want %d (%s)", rr.Code, gotToken, tt.wantStatus, rr.Body.String())
			}
			if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
			if tt.err == nil && rr.Body.String() != "01234" {
				t.Errorf("body = %q", rr.Body.String())
			}
		})
	}
}

// deadlineRecorder is a ResponseWriter that records SetWriteDeadline calls
// the way a real connection accepts them.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadline = t
	return nil
}

// The handler may have waited up to 20 s of the 30 s write timeout for the
// transcode; the body must get a fresh, full write window.
func TestHandleCompatStream_RestartsWriteDeadline(t *testing.T) {
	env := compatEnvReturning(t, writeRendition(t), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/5/compat", nil)
	req.AddCookie(env.cookie)
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now()
	env.srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.deadline.Sub(before); got < httpWriteTimeout-time.Second || got > httpWriteTimeout+5*time.Second {
		t.Errorf("write deadline set %s ahead, want about %s", got, httpWriteTimeout)
	}
}
