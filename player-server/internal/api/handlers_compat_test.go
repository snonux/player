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

const compatBody = "0123456789abcdefghij"

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

// writeRendition creates a finished rendition file as the cache would.
func writeRendition(t *testing.T) *transcode.Rendition {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m5-6-1-v1.mp4")
	if err := os.WriteFile(path, []byte(compatBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return &transcode.Rendition{Path: path, ContentType: "video/mp4", ETag: "m5-6-1-v1.mp4"}
}

func TestHandleCompatStream_ServesRenditionWithRange(t *testing.T) {
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
		rr := env.do(context.Background(), prefix+"/media/5/compat", true, nil)
		if rr.Code != http.StatusOK || rr.Body.String() != compatBody {
			t.Fatalf("%s: status %d body %q", prefix, rr.Code, rr.Body.String())
		}
		if gotMedia != 5 || gotUser != 1 {
			t.Errorf("%s: service called with media=%d user=%d", prefix, gotMedia, gotUser)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "video/mp4" {
			t.Errorf("%s: Content-Type = %q", prefix, ct)
		}
		if rr.Header().Get("Accept-Ranges") != "bytes" || rr.Header().Get("ETag") != `"m5-6-1-v1.mp4"` {
			t.Errorf("%s: headers = %v", prefix, rr.Header())
		}
		// The cache bumps the mtime on every hit, so it must not be a validator.
		if lm := rr.Header().Get("Last-Modified"); lm != "" {
			t.Errorf("%s: unexpected Last-Modified %q", prefix, lm)
		}
	}

	// Seeking: a Range request yields 206 with exactly the requested bytes.
	rr := env.do(context.Background(), "/api/v1/media/5/compat", true, http.Header{"Range": {"bytes=10-14"}})
	if rr.Code != http.StatusPartialContent || rr.Body.String() != "abcde" {
		t.Fatalf("range: status %d body %q", rr.Code, rr.Body.String())
	}
	if cr := rr.Header().Get("Content-Range"); cr != "bytes 10-14/20" {
		t.Errorf("Content-Range = %q", cr)
	}

	// If-Range with the stable ETag keeps the request partial.
	rr = env.do(context.Background(), "/api/v1/media/5/compat", true, http.Header{"Range": {"bytes=0-3"}, "If-Range": {`"m5-6-1-v1.mp4"`}})
	if rr.Code != http.StatusPartialContent || rr.Body.String() != "0123" {
		t.Errorf("if-range: status %d body %q", rr.Code, rr.Body.String())
	}

	// Revalidation of an unchanged rendition.
	rr = env.do(context.Background(), "/api/v1/media/5/compat", true, http.Header{"If-None-Match": {`"m5-6-1-v1.mp4"`}})
	if rr.Code != http.StatusNotModified {
		t.Errorf("if-none-match: status %d", rr.Code)
	}
}

func TestHandleCompatStream_Errors(t *testing.T) {
	ffmpegErr := errors.New("ffmpeg transcode /media/secret/path.avi: exit status 1: Invalid data")
	tests := []struct {
		name       string
		path       string
		err        error
		wantStatus int
		wantBody   string // substring
	}{
		{"unknown id", "/api/v1/media/5/compat", service.ErrNotFound, http.StatusNotFound, "not found"},
		{"no access", "/api/v1/media/5/compat", service.ErrForbidden, http.StatusForbidden, "forbidden"},
		{"image media", "/api/v1/media/5/compat", service.ErrNotTranscodable, http.StatusUnsupportedMediaType, "cannot be transcoded"},
		{"ffmpeg failure", "/api/v1/media/5/compat", fmt.Errorf("transcode media 5: %w", ffmpegErr), http.StatusInternalServerError, "transcode failed"},
		{"cancelled request", "/api/v1/media/5/compat", fmt.Errorf("transcode media 5: %w", context.Canceled), statusClientClosedRequest, "cancelled"},
		{"malformed id", "/api/v1/media/abc/compat", nil, http.StatusBadRequest, "invalid media id"},
		{"zero id", "/api/v1/media/0/compat", nil, http.StatusBadRequest, "invalid media id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newCompatTestEnv(t, &service.MockCompatStreamService{
				CompatStreamFunc: func(context.Context, int64, int64) (*transcode.Rendition, error) { return nil, tt.err },
			})
			rr := env.do(context.Background(), tt.path, true, nil)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want substring %q", rr.Body.String(), tt.wantBody)
			}
			// ffmpeg diagnostics contain server paths and must not leak.
			if strings.Contains(rr.Body.String(), "/media/secret") {
				t.Errorf("response leaks internal path: %s", rr.Body.String())
			}
		})
	}
}

func TestHandleCompatStream_PendingTellsClientToRetry(t *testing.T) {
	env := newCompatTestEnv(t, &service.MockCompatStreamService{
		CompatStreamFunc: func(context.Context, int64, int64) (*transcode.Rendition, error) {
			return nil, service.ErrTranscodePending
		},
	})
	rr := env.do(context.Background(), "/api/v1/media/5/compat", true, nil)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	if ra := rr.Header().Get("Retry-After"); ra != "5" {
		t.Errorf("Retry-After = %q, want 5", ra)
	}
	var body struct {
		Error             string `json:"error"`
		Status            string `json:"status"`
		RetryAfterSeconds int    `json:"retry_after_seconds"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "transcoding" || body.RetryAfterSeconds != 5 || body.Error == "" {
		t.Errorf("unexpected body %+v", body)
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

func TestHandleCompatStream_MissingRenditionAndService(t *testing.T) {
	// Rendition evicted between the service call and the open.
	env := newCompatTestEnv(t, &service.MockCompatStreamService{
		CompatStreamFunc: func(context.Context, int64, int64) (*transcode.Rendition, error) {
			return &transcode.Rendition{Path: filepath.Join(t.TempDir(), "gone.mp4")}, nil
		},
	})
	if rr := env.do(context.Background(), "/api/v1/media/5/compat", true, nil); rr.Code != http.StatusNotFound {
		t.Errorf("evicted rendition: status = %d, want 404", rr.Code)
	}

	// The mock's nil-func default returns a nil rendition.
	env = newCompatTestEnv(t, &service.MockCompatStreamService{})
	if rr := env.do(context.Background(), "/api/v1/media/5/compat", true, nil); rr.Code != http.StatusNotFound {
		t.Errorf("nil rendition: status = %d, want 404", rr.Code)
	}

	// No compat service wired at all.
	env = newCompatTestEnv(t, nil)
	if rr := env.do(context.Background(), "/api/v1/media/5/compat", true, nil); rr.Code != http.StatusNotImplemented {
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
		{"ok", nil, http.StatusOK},
		{"unknown token", service.ErrShareNotFound, http.StatusNotFound},
		{"media gone", service.ErrMediaNotFound, http.StatusNotFound},
		{"expired", service.ErrShareExpired, http.StatusGone},
		{"image media", service.ErrNotTranscodable, http.StatusUnsupportedMediaType},
		{"still transcoding", service.ErrTranscodePending, http.StatusServiceUnavailable},
		{"ffmpeg failure", errors.New("boom"), http.StatusInternalServerError},
		{"cancelled request", context.Canceled, statusClientClosedRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendition := writeRendition(t)
			var gotToken string
			env := newCompatTestEnv(t, &service.MockCompatStreamService{
				SharedCompatStreamFunc: func(_ context.Context, token string) (*transcode.Rendition, error) {
					gotToken = token
					if tt.err != nil {
						return nil, tt.err
					}
					return rendition, nil
				},
			})
			// Public route: no session cookie.
			rr := env.do(context.Background(), "/s/tok123/compat", false, http.Header{"Range": {"bytes=0-4"}})
			want := tt.wantStatus
			if want == http.StatusOK {
				want = http.StatusPartialContent
			}
			if rr.Code != want {
				t.Fatalf("status = %d, want %d (%s)", rr.Code, want, rr.Body.String())
			}
			if gotToken != "tok123" {
				t.Errorf("token = %q", gotToken)
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

// TestCompatStream_EndToEndCancelledRequest drives the real service and cache
// through the HTTP layer: the client gives up while ffmpeg (a blocking fake)
// is still running, gets no success response, and a later request is served
// from the rendition the surviving job produced.
func TestCompatStream_EndToEndCancelledRequest(t *testing.T) {
	mediaRoot := t.TempDir()
	src := filepath.Join(mediaRoot, "clip.avi")
	if err := os.WriteFile(src, []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{GetMediaByIDFunc: func(context.Context, int64) (*model.Media, error) {
			return &model.Media{ID: 5, SetID: 1, Type: model.MediaTypeVideo, FileName: "clip.avi", AbsPath: src}, nil
		}},
		UserRepo: repository.MockUserRepo{GetUserByIDFunc: func(_ context.Context, id int64) (*model.User, error) {
			return &model.User{ID: id, IsAdmin: true}, nil
		}},
	}
	runner := &blockingRunner{started: make(chan struct{}, 1), release: make(chan struct{})}
	cache := transcode.NewCache(context.Background(), runner, clock.RealClock{}, nil, transcode.Options{
		Dir: filepath.Join(t.TempDir(), "cache"), MaxBytes: 1 << 20,
	})
	env := newCompatTestEnv(t, service.NewCompatStreamService(service.NewAccessHelper(store), nil, cache, mediaRoot))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- env.do(ctx, "/api/v1/media/5/compat", true, nil) }()
	<-runner.started
	cancel()
	if rr := <-done; rr.Code != statusClientClosedRequest {
		t.Fatalf("cancelled request: status = %d, want %d", rr.Code, statusClientClosedRequest)
	}

	close(runner.release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		rr := env.do(context.Background(), "/api/v1/media/5/compat", true, nil)
		if rr.Code == http.StatusOK {
			if rr.Body.String() != "transcoded" {
				t.Errorf("body = %q", rr.Body.String())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rendition never became available, last status %d", rr.Code)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if runner.calls != 1 {
		t.Errorf("runner calls = %d, want 1 (cancelled request must not restart the transcode)", runner.calls)
	}
}

// blockingRunner is a transcode.Runner that waits for release before writing
// its output. calls is only read after the job has finished.
type blockingRunner struct {
	calls   int
	started chan struct{}
	release chan struct{}
}

func (b *blockingRunner) Transcode(ctx context.Context, _ transcode.Kind, _, outputPath string) error {
	b.calls++
	b.started <- struct{}{}
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return os.WriteFile(outputPath, []byte("transcoded"), 0o644)
}
