package app

// End-to-end tests of the compatibility stream through the production
// wiring: real SQLite store, real access checks, real share accounting, real
// rendition cache and the real routes. Only ffmpeg is replaced by a fake
// runner. Unit tests with mocked stores cannot show that a set permission or
// a revoked share is honoured on these routes; these tests can.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/transcode"
)

// fakeRunner stands in for ffmpeg. When release is non-nil a job blocks until
// it is closed.
type fakeRunner struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (f *fakeRunner) Transcode(ctx context.Context, job transcode.Job) error {
	outputPath := job.Output
	f.calls.Add(1)
	if f.release != nil {
		f.started <- struct{}{}
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return os.WriteFile(outputPath, []byte("transcoded"), 0o644)
}

// e2e is a fully wired server with a small library:
//
//	set 1 (alice may view): clip.avi, third.flv (legacy), movie.mp4 (native), pic.png
//	set 2 (bob may view):   other.wmv (legacy)
//
// admin is an administrator and sees everything.
type e2e struct {
	t      *testing.T
	server http.Handler
	deps   *Deps
	store  repository.Store
	// ids by user name / file name.
	users map[string]int64
	media map[string]int64
}

func newE2E(t *testing.T, runner transcode.Runner) *e2e {
	t.Helper()
	dir := t.TempDir()
	store, err := repository.Open(filepath.Join(dir, "media.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := &internal.Config{
		MediaRoot:              filepath.Join(dir, "media"),
		SessionTimeoutHours:    1,
		GCIntervalMinutes:      1,
		PodcastCheckMinutes:    1,
		ShareDefaultExpiryDays: 7,
		TranscodeCacheDir:      filepath.Join(dir, "transcode-cache"),
		TranscodeCacheMaxMB:    1,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	deps := WireWithRunner(cfg, store, logger, ctx, runner)
	t.Cleanup(func() {
		cancel()
		stopTranscoding(deps)
		_ = store.Close()
	})
	server, err := NewAPIServer(deps, http.Dir(t.TempDir()), logger)
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	e := &e2e{t: t, server: server, deps: deps, store: store, users: map[string]int64{}, media: map[string]int64{}}
	e.seed(cfg.MediaRoot)
	return e
}

// seed creates users, sets, permissions, media rows and the media files.
func (e *e2e) seed(mediaRoot string) {
	e.t.Helper()
	ctx, now := context.Background(), time.Now()
	for _, name := range []string{"admin", "alice", "bob"} {
		id, err := e.store.CreateUser(ctx, &model.User{Username: name, PasswordHash: "x", IsAdmin: name == "admin", CreatedAt: now})
		e.must(err)
		e.users[name] = id
	}
	library := []struct {
		set, viewer string
		files       map[string]model.MediaType
	}{
		{"set1", "alice", map[string]model.MediaType{"clip.avi": model.MediaTypeVideo, "third.flv": model.MediaTypeVideo, "movie.mp4": model.MediaTypeVideo, "pic.png": model.MediaTypeImage}},
		{"set2", "bob", map[string]model.MediaType{"other.wmv": model.MediaTypeVideo}},
	}
	for _, l := range library {
		setID, err := e.store.CreateSet(ctx, &model.Set{Name: l.set, RootPath: l.set, CreatedAt: now})
		e.must(err)
		e.must(e.store.GrantPermission(ctx, &model.SetPermission{SetID: setID, UserID: e.users[l.viewer], Role: model.RoleViewer, CreatedAt: now}))
		e.must(os.MkdirAll(filepath.Join(mediaRoot, l.set), 0o755))
		for file, typ := range l.files {
			abs := filepath.Join(mediaRoot, l.set, file)
			e.must(os.WriteFile(abs, []byte("original "+file), 0o644))
			// Not "still being uploaded": the cache waits for fresh files.
			settled := now.Add(-time.Hour)
			e.must(os.Chtimes(abs, settled, settled))
			id, err := e.store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: l.set + "/" + file, FileName: file, AbsPath: abs, Type: typ, CreatedAt: now})
			e.must(err)
			e.media[file] = id
		}
	}
}

func (e *e2e) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

// request performs a request as user ("" = anonymous).
func (e *e2e) request(ctx context.Context, method, path, user, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Accept", "application/json")
	if user != "" {
		session, err := e.deps.SM.CreateSession(context.Background(), e.users[user])
		e.must(err)
		req.AddCookie(&http.Cookie{Name: "session", Value: session})
	}
	rr := httptest.NewRecorder()
	e.server.ServeHTTP(rr, req)
	return rr
}

func (e *e2e) get(path, user string) *httptest.ResponseRecorder {
	return e.request(context.Background(), http.MethodGet, path, user, "")
}

func (e *e2e) compatPath(file string) string {
	return "/api/v1/media/" + strconv.FormatInt(e.media[file], 10) + "/compat"
}

// share creates a share link for file as user and returns its token.
func (e *e2e) share(file, user, body string) string {
	e.t.Helper()
	path := "/api/v1/media/" + strconv.FormatInt(e.media[file], 10) + "/shares"
	rr := e.request(context.Background(), http.MethodPost, path, user, body)
	var share model.Share
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &share) != nil || share.Token == "" {
		e.t.Fatalf("create share: %d %s", rr.Code, rr.Body.String())
	}
	return share.Token
}

func TestCompatStream_SetPermissions(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	tests := []struct {
		name, user, path string
		want             int
	}{
		{"viewer of the set", "alice", e.compatPath("clip.avi"), http.StatusOK},
		{"viewer of another set", "bob", e.compatPath("clip.avi"), http.StatusForbidden},
		{"viewer of another set (reverse)", "alice", e.compatPath("other.wmv"), http.StatusForbidden},
		{"viewer of the second set", "bob", e.compatPath("other.wmv"), http.StatusOK},
		{"admin sees every set", "admin", e.compatPath("other.wmv"), http.StatusOK},
		{"anonymous", "", e.compatPath("clip.avi"), http.StatusUnauthorized},
		{"unknown media", "alice", "/api/v1/media/9999/compat", http.StatusNotFound},
		{"native media is not transcoded", "alice", e.compatPath("movie.mp4"), http.StatusBadRequest},
		{"image", "alice", e.compatPath("pic.png"), http.StatusUnsupportedMediaType},
		// The legacy prefix is the same route.
		{"legacy prefix", "alice", strings.Replace(e.compatPath("clip.avi"), "/api/v1/", "/api/", 1), http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := e.get(tt.path, tt.user)
			if rr.Code != tt.want {
				t.Fatalf("GET %s as %q = %d, want %d (%s)", tt.path, tt.user, rr.Code, tt.want, rr.Body.String())
			}
			if tt.want == http.StatusOK && rr.Body.String() != "transcoded" {
				t.Errorf("body = %q, want the rendition", rr.Body.String())
			}
		})
	}
}

// A cached rendition must not outlive the permission to see its source.
func TestCompatStream_RevokedPermissionBlocksCachedRendition(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	path := e.compatPath("clip.avi")
	if rr := e.get(path, "alice"); rr.Code != http.StatusOK {
		t.Fatalf("before revoke = %d", rr.Code)
	}
	media, err := e.store.GetMediaByID(context.Background(), e.media["clip.avi"])
	e.must(err)
	e.must(e.store.RevokePermission(context.Background(), media.SetID, e.users["alice"]))
	if rr := e.get(path, "alice"); rr.Code != http.StatusForbidden {
		t.Fatalf("after revoke = %d, want 403", rr.Code)
	}
}

func TestCompatStream_Shares(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()

	limited := e.share("clip.avi", "alice", `{"max_uses":2}`)
	for i, want := range []int{http.StatusOK, http.StatusOK, http.StatusGone} {
		if rr := e.get("/s/"+limited+"/compat", ""); rr.Code != want {
			t.Fatalf("use %d of a max_uses=2 share = %d, want %d", i+1, rr.Code, want)
		}
	}

	revoked := e.share("clip.avi", "alice", "")
	if rr := e.request(ctx, http.MethodDelete, "/api/v1/shares/"+revoked, "alice", ""); rr.Code != http.StatusOK {
		t.Fatalf("revoke share = %d", rr.Code)
	}
	e.must(e.store.CreateShare(ctx, &model.Share{
		Token: "expired", MediaID: e.media["clip.avi"], CreatedBy: e.users["alice"],
		CreatedAt: time.Now().Add(-48 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
	}))
	native := e.share("movie.mp4", "alice", "")

	tests := []struct {
		name, token string
		want        int
	}{
		{"revoked", revoked, http.StatusNotFound},
		{"expired", "expired", http.StatusGone},
		{"unknown", "no-such-token", http.StatusNotFound},
		{"native media", native, http.StatusBadRequest},
	}
	for _, tt := range tests {
		if rr := e.get("/s/"+tt.token+"/compat", ""); rr.Code != tt.want {
			t.Errorf("%s share: GET compat = %d, want %d (%s)", tt.name, rr.Code, tt.want, rr.Body.String())
		}
	}
}

// The fields clients use to pick the URL, as served by the real stack.
func TestCompatStream_ClientContractFields(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	id := strconv.FormatInt(e.media["clip.avi"], 10)
	token := e.share("clip.avi", "alice", "")
	tests := []struct {
		name, path, user string
		want             []string
	}{
		{"media detail", "/api/v1/media/" + id, "alice", []string{`"transcoded":true`}},
		{"media list", "/api/v1/media", "alice", []string{`"transcoded":true`, `"transcoded":false`}},
		{"playback hint", "/api/v1/media/" + id + "/playback", "alice", []string{`"playback_url":"/api/v1/media/` + id + `/compat"`, `"transcoded":true`}},
		{"share metadata", "/s/" + token, "", []string{`"playback_url":"/s/` + token + `/compat"`, `"transcoded":true`}},
	}
	for _, tt := range tests {
		rr := e.get(tt.path, tt.user)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status %d (%s)", tt.name, rr.Code, rr.Body.String())
		}
		for _, w := range tt.want {
			if !strings.Contains(rr.Body.String(), w) {
				t.Errorf("%s: body lacks %s: %s", tt.name, w, rr.Body.String())
			}
		}
	}
}

// The client gives up while the transcode is running: it gets no success
// response, the job survives, and the next request is served from cache.
func TestCompatStream_CancelledRequest(t *testing.T) {
	runner := &fakeRunner{started: make(chan struct{}, 1), release: make(chan struct{})}
	e := newE2E(t, runner)
	path := e.compatPath("clip.avi")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- e.request(ctx, http.MethodGet, path, "alice", "") }()
	<-runner.started
	cancel()
	if rr := <-done; rr.Code != 499 {
		t.Fatalf("cancelled request = %d, want 499", rr.Code)
	}

	close(runner.release)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		rr := e.get(path, "alice")
		if rr.Code == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rendition never became available, last status %d", rr.Code)
		}
	}
	if got := runner.calls.Load(); got != 1 {
		t.Errorf("runner calls = %d, want 1 (the cancelled request must not restart the transcode)", got)
	}
}

// Soft-deleted media is gone for users; a share link to it behaves exactly
// like the plain share stream does (the two routes must not diverge).
func TestCompatStream_SoftDeletedMedia(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	path := e.compatPath("clip.avi")
	token := e.share("clip.avi", "alice", "")
	if rr := e.get(path, "alice"); rr.Code != http.StatusOK {
		t.Fatalf("before delete = %d", rr.Code)
	}

	del := "/api/v1/media/" + strconv.FormatInt(e.media["clip.avi"], 10)
	if rr := e.request(context.Background(), http.MethodDelete, del, "admin", ""); rr.Code != http.StatusOK {
		t.Fatalf("soft delete = %d (%s)", rr.Code, rr.Body.String())
	}
	for _, user := range []string{"alice", "admin"} {
		if rr := e.get(path, user); rr.Code != http.StatusNotFound {
			t.Errorf("compat of soft-deleted media as %s = %d, want 404", user, rr.Code)
		}
	}
	stream, compat := e.get("/s/"+token+"/stream", ""), e.get("/s/"+token+"/compat", "")
	if stream.Code != compat.Code {
		t.Errorf("share of soft-deleted media: stream = %d, compat = %d; the routes must agree", stream.Code, compat.Code)
	}
}

// HEAD is the clients' readiness probe. It must never consume a share use:
// with max_uses=1 any number of probes still leaves the one GET.
func TestCompatStream_HeadProbesDoNotConsumeShareUses(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := e.share("clip.avi", "alice", `{"max_uses":1}`)
	path := "/s/" + token + "/compat"

	for i := range 4 {
		rr := e.request(context.Background(), http.MethodHead, path, "", "")
		if rr.Code != http.StatusOK || rr.Body.Len() != 0 || rr.Header().Get("Content-Type") != "video/mp4" || rr.Header().Get("ETag") == "" {
			t.Fatalf("HEAD probe %d = %d, body %q, headers %v", i+1, rr.Code, rr.Body.String(), rr.Header())
		}
	}
	if rr := e.get(path, ""); rr.Code != http.StatusOK || rr.Body.String() != "transcoded" {
		t.Fatalf("GET after probes = %d %q, want the rendition", rr.Code, rr.Body.String())
	}
	// The single use is spent now, for GET and HEAD alike.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if rr := e.request(context.Background(), method, path, "", ""); rr.Code != http.StatusGone {
			t.Errorf("%s after the last use = %d, want 410", method, rr.Code)
		}
	}
	// Authenticated HEAD works the same way.
	if rr := e.request(context.Background(), http.MethodHead, e.compatPath("clip.avi"), "alice", ""); rr.Code != http.StatusOK || rr.Body.Len() != 0 {
		t.Errorf("authenticated HEAD = %d, body %q", rr.Code, rr.Body.String())
	}
}

// abandoned sends a request and gives up after a moment, leaving its
// transcode job in flight. It returns the status the handler recorded.
func (e *e2e) abandoned(path, user string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	return e.request(ctx, http.MethodGet, path, user, "").Code
}

// All anonymous share requests share one job budget (two jobs), so share
// links cannot fill the transcode queue and lock out signed-in users.
func TestCompatStream_SharesHaveOneCommonJobBudget(t *testing.T) {
	runner := &fakeRunner{started: make(chan struct{}, 8), release: make(chan struct{})}
	e := newE2E(t, runner)
	defer close(runner.release)
	first := e.share("clip.avi", "alice", "")
	second := e.share("other.wmv", "bob", "")
	third := e.share("third.flv", "alice", "")

	for i, token := range []string{first, second} {
		if code := e.abandoned("/s/"+token+"/compat", ""); code != 499 {
			t.Fatalf("share request %d = %d, want it accepted (499 after giving up)", i+1, code)
		}
	}
	rr := e.get("/s/"+third+"/compat", "")
	if rr.Code != http.StatusServiceUnavailable || rr.Header().Get("X-Transcode-Status") != "busy" || rr.Header().Get("Retry-After") != "5" {
		t.Fatalf("third share = %d %v, want 503 busy", rr.Code, rr.Header())
	}
	// A signed-in user asking for the very same file is still served.
	if code := e.abandoned(e.compatPath("third.flv"), "alice"); code != 499 {
		t.Errorf("signed-in user = %d, want the job accepted while shares are at their limit", code)
	}
}

// After shutdown began no job is started; clients are told to retry. Over a
// real connection, to also check that a HEAD 503 carries its status in the
// header and has no body.
func TestCompatStream_AfterShutdownAnswersBusy(t *testing.T) {
	runner := &fakeRunner{}
	e := newE2E(t, runner)
	token := e.share("clip.avi", "alice", "")
	srv := httptest.NewServer(e.server)
	defer srv.Close()

	e.deps.Transcodes.Close()
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req, err := http.NewRequest(method, srv.URL+"/s/"+token+"/compat", nil)
		e.must(err)
		resp, err := srv.Client().Do(req)
		e.must(err)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("X-Transcode-Status") != "busy" || resp.Header.Get("Retry-After") != "5" {
			t.Errorf("%s after shutdown = %d %v, want 503 busy", method, resp.StatusCode, resp.Header)
		}
		if method == http.MethodHead && len(body) != 0 {
			t.Errorf("HEAD response has a body: %q", body)
		}
	}
	if runner.calls.Load() != 0 {
		t.Error("a transcode was started after shutdown began")
	}
}

func TestWire_UsesFFmpegRunnerAndChecksSetup(t *testing.T) {
	dir := t.TempDir()
	store, err := repository.Open(filepath.Join(dir, "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := &internal.Config{MediaRoot: dir, SessionTimeoutHours: 1, GCIntervalMinutes: 1, PodcastCheckMinutes: 1, TranscodeCacheDir: filepath.Join(dir, "cache"), TranscodeCacheMaxMB: 1}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	deps := Wire(cfg, store, logger, context.Background())
	if deps.CompatSvc == nil || deps.Transcodes == nil || deps.GCWorker == nil {
		t.Fatalf("Wire left transcoding unwired: %+v", deps)
	}
	// The startup check only logs; it must cope with a missing ffmpeg and
	// must create the cache directory when it can.
	checkTranscoding(deps, transcode.NewFFmpegRunner(cfg.TranscodeMaxJobs))
	if _, err := os.Stat(cfg.TranscodeCacheDir); err != nil {
		t.Errorf("startup check did not create the cache dir: %v", err)
	}
}
