package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
		SharedCompatStreamFunc: func(context.Context, string, string) (*transcode.Rendition, error) { return r, err },
	})
}

// do performs a GET; authenticated requests carry the session cookie.
func (e compatTestEnv) do(ctx context.Context, path string, authenticated bool, header http.Header) *httptest.ResponseRecorder {
	return e.send(ctx, http.MethodGet, path, authenticated, header)
}

// head performs an authenticated HEAD, the clients' readiness probe.
func (e compatTestEnv) head(path string) *httptest.ResponseRecorder {
	return e.send(context.Background(), http.MethodHead, path, true, nil)
}

func (e compatTestEnv) send(ctx context.Context, method, path string, authenticated bool, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil).WithContext(ctx)
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
				// The same status as a header, for clients probing with HEAD.
				if got := rr.Header().Get("X-Transcode-Status"); got != tt.wantStatus {
					t.Errorf("X-Transcode-Status = %q, want %q", got, tt.wantStatus)
				}
				head := compatEnvReturning(t, nil, tt.err).head(path)
				// (The recorder keeps the body; a real connection drops it
				// for HEAD, which the end-to-end tests in internal/app check.)
				if head.Code != http.StatusServiceUnavailable || head.Header().Get("X-Transcode-Status") != tt.wantStatus ||
					head.Header().Get("Retry-After") != "5" {
					t.Errorf("HEAD: status %d headers %v", head.Code, head.Header())
				}
			})
		}
	}
}

// HEAD is the readiness probe: same checks and headers as GET, no body, and
// X-Transcode-Status only on 503.
func TestCompatStream_HeadWhenReady(t *testing.T) {
	env := compatEnvReturning(t, writeRendition(t), nil)
	for _, path := range []string{"/api/media/5/compat", "/api/v1/media/5/compat", "/s/tok/compat"} {
		rr := env.head(path)
		h := rr.Header()
		if rr.Code != http.StatusOK || rr.Body.Len() != 0 {
			t.Fatalf("HEAD %s: status %d body %q", path, rr.Code, rr.Body.String())
		}
		if h.Get("Content-Type") != "video/mp4" || h.Get("ETag") != compatETag || h.Get("Accept-Ranges") != "bytes" ||
			h.Get("Content-Length") != "20" || h.Get("X-Transcode-Status") != "" {
			t.Errorf("HEAD %s: headers = %v", path, h)
		}
	}
}

func TestCompatStream_HeadTerminalStatuses(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{service.ErrNotFound, http.StatusNotFound},
		{service.ErrForbidden, http.StatusForbidden},
		{service.ErrCompatNotNeeded, http.StatusBadRequest},
		{service.ErrNotTranscodable, http.StatusUnsupportedMediaType},
		{service.ErrShareExpired, http.StatusGone},
		{service.ErrTranscodeNoSpace, http.StatusInsufficientStorage},
		{errors.New("ffmpeg failed"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		for _, path := range []string{"/api/v1/media/5/compat", "/s/tok/compat"} {
			if rr := compatEnvReturning(t, nil, tt.err).head(path); rr.Code != tt.want {
				t.Errorf("HEAD %s with %v = %d, want %d (same as GET)", path, tt.err, rr.Code, tt.want)
			}
		}
	}
	// HEAD needs a session exactly like GET.
	env := compatEnvReturning(t, writeRendition(t), nil)
	if rr := env.send(context.Background(), http.MethodHead, "/api/v1/media/5/compat", false, nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated HEAD = %d, want 401", rr.Code)
	}
}

// fakeViewCredential is the only viewing credential shareUseEnv accepts.
const fakeViewCredential = "good-credential"

// shareUseEnv is an env whose service returns rendition and counts the share
// uses the handler asks for: one per viewing it has to open, i.e. per call
// without fakeViewCredential.
func shareUseEnv(t *testing.T, rendition *transcode.Rendition, useErr error) (compatTestEnv, *int) {
	t.Helper()
	uses := new(int)
	return newCompatTestEnv(t, &service.MockCompatStreamService{
		SharedCompatStreamFunc: func(context.Context, string, string) (*transcode.Rendition, error) { return rendition, nil },
		EnsureShareViewingFunc: func(_ context.Context, token, credential string) (service.ShareViewing, error) {
			if token != "tok" {
				t.Errorf("viewing requested for token %q", token)
			}
			if credential == fakeViewCredential {
				return service.ShareViewing{Credential: credential}, nil
			}
			*uses++
			if useErr != nil {
				return service.ShareViewing{}, useErr
			}
			return service.ShareViewing{Credential: fakeViewCredential, ExpiresAt: time.Now().Add(time.Hour), Opened: true}, nil
		},
	}), uses
}

// A share use is counted per viewing: a GET without a viewing credential
// opens one (and is handed the cookie), requests that present the credential
// — as cookie or as "view" parameter — are free, and so is every HEAD probe.
func TestHandleShareCompatStream_CountsUsePerViewing(t *testing.T) {
	env, uses := shareUseEnv(t, writeRendition(t), nil)
	cookie := http.Header{"Cookie": {shareViewCookie + "=" + fakeViewCredential}}
	ranged := http.Header{"Range": {"bytes=0-4"}}
	steps := []struct {
		method     string
		path       string
		header     http.Header
		wantCode   int
		wantUses   int
		wantCookie bool
	}{
		{http.MethodHead, "/s/tok/compat", nil, http.StatusOK, 0, false},
		{http.MethodGet, "/s/tok/compat", nil, http.StatusOK, 1, true},
		{http.MethodGet, "/s/tok/compat", cookie, http.StatusOK, 1, false},
		{http.MethodGet, "/s/tok/compat?view=" + fakeViewCredential, ranged, http.StatusPartialContent, 1, false},
		{http.MethodHead, "/s/tok/compat", nil, http.StatusOK, 1, false},
		{http.MethodGet, "/s/tok/compat?view=forged", nil, http.StatusOK, 2, true},
	}
	for i, st := range steps {
		rr := env.send(context.Background(), st.method, st.path, false, st.header)
		if rr.Code != st.wantCode || *uses != st.wantUses {
			t.Fatalf("step %d (%s): status %d, uses %d; want %d, %d", i, st.method, rr.Code, *uses, st.wantCode, st.wantUses)
		}
		setCookie := rr.Header().Get("Set-Cookie")
		if got := setCookie != ""; got != st.wantCookie {
			t.Fatalf("step %d: Set-Cookie %q, want cookie = %v", i, setCookie, st.wantCookie)
		}
		if st.wantCookie && !strings.HasPrefix(setCookie, shareViewCookie+"="+fakeViewCredential+"; Path=/s/tok;") {
			t.Fatalf("step %d: Set-Cookie %q is not the viewing cookie scoped to the share", i, setCookie)
		}
	}
}

// A rendition evicted before it could be opened answers 503 — and must not
// have cost the share a use, since nothing was delivered.
func TestHandleShareCompatStream_NoUseWhenRenditionCannotBeOpened(t *testing.T) {
	gone := &transcode.Rendition{Path: filepath.Join(t.TempDir(), "gone.mp4")}
	env, uses := shareUseEnv(t, gone, nil)
	rr := env.send(context.Background(), http.MethodGet, "/s/tok/compat", false, nil)
	if rr.Code != http.StatusServiceUnavailable || rr.Header().Get("X-Transcode-Status") != "transcoding" {
		t.Fatalf("status %d %v, want 503 transcoding", rr.Code, rr.Header())
	}
	if *uses != 0 {
		t.Errorf("share uses = %d, want 0", *uses)
	}
}

// The share ran out between the lookup and the count (another request took
// the last use): 410 and no content.
func TestHandleShareCompatStream_UseRefused(t *testing.T) {
	env, uses := shareUseEnv(t, writeRendition(t), service.ErrShareExpired)
	rr := env.send(context.Background(), http.MethodGet, "/s/tok/compat", false, nil)
	if rr.Code != http.StatusGone || strings.Contains(rr.Body.String(), compatBody) || *uses != 1 {
		t.Fatalf("status %d body %q uses %d, want 410 without content", rr.Code, rr.Body.String(), *uses)
	}
}

// Answers from the negative cache do no work and can be requested
// anonymously in a loop, so they must not produce an Error line each.
func TestCompatStream_BackoffHitsAreNotLoggedAsErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantError bool
	}{
		{"failed recently", fmt.Errorf("transcode media 5: %w", transcode.ErrFailedRecently), false},
		{"aborted", fmt.Errorf("transcode media 5: %w", transcode.ErrAborted), false},
		{"real failure", errors.New("ffmpeg transcode: exit status 1"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged strings.Builder
			env := compatEnvReturning(t, nil, tt.err)
			env.srv.logger = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelInfo}))
			if rr := env.get("/s/tok/compat"); rr.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rr.Code)
			}
			if got := strings.Contains(logged.String(), "level=ERROR"); got != tt.wantError {
				t.Errorf("error logged = %v, want %v: %q", got, tt.wantError, logged.String())
			}
		})
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
				SharedCompatStreamFunc: func(_ context.Context, token, _ string) (*transcode.Rendition, error) {
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
