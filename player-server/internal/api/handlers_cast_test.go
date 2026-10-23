package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/auth"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
	"github.com/snonux/player/internal/transcode"
)

// castTestEnv is a Server whose media service lets user 1 stream media 5
// (an MP3 file) and nothing else.
type castTestEnv struct {
	srv    *Server
	cookie *http.Cookie
	clk    *clock.MockClock
	// allowed is consulted on every access check, so a test can revoke
	// access after a ticket was issued.
	allowed *bool
}

func newCastTestEnv(t *testing.T) castTestEnv {
	t.Helper()
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.mp3")
	thumb := filepath.Join(dir, "song.jpg")
	for path, body := range map[string]string{audio: "ID3audio-bytes", thumb: "jpeg"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rendition := filepath.Join(dir, "song.m4a")
	if err := os.WriteFile(rendition, []byte("m4a-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	allowed := true
	access := func(mediaID, userID int64) error {
		if mediaID != 5 {
			return service.ErrNotFound
		}
		if userID != 1 || !allowed {
			return service.ErrForbidden
		}
		return nil
	}
	browse := &service.MockMediaService{
		StreamMediaFunc: func(_ context.Context, mediaID, userID int64) (*service.FileResult, error) {
			if err := access(mediaID, userID); err != nil {
				return nil, err
			}
			return &service.FileResult{Path: audio, FileName: "song.mp3"}, nil
		},
		GetThumbnailFunc: func(_ context.Context, mediaID, userID int64) (*service.FileResult, error) {
			if err := access(mediaID, userID); err != nil {
				return nil, err
			}
			return &service.FileResult{Path: thumb, FileName: "song.jpg"}, nil
		},
	}
	compat := &service.MockCompatStreamService{
		CompatStreamFunc: func(_ context.Context, mediaID, userID int64) (*transcode.Rendition, error) {
			if err := access(mediaID, userID); err != nil {
				return nil, err
			}
			return &transcode.Rendition{Path: rendition, ContentType: "audio/mp4", ETag: "e"}, nil
		},
	}

	clk := &clock.MockClock{T: time.Now()}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, clk, time.Hour)
	srv, err := NewServer(ServerDeps{
		Store:          store,
		SessionManager: sm,
		Config:         &internal.Config{},
		Services: ServerServices{
			Media: MediaServices{Browse: browse, Compat: compat},
			Auth: &service.MockAuthService{
				CountUsersFunc:  func(context.Context) (int, error) { return 1, nil },
				GetUserByIDFunc: func(_ context.Context, id int64) (*model.User, error) { return &model.User{ID: id}, nil },
			},
		},
		StaticFS:      newTestFS(map[string]string{"index.html": "index", "login.html": "login"}),
		MediaStreamer: service.NewMediaStreamer(nil, ""),
		Clock:         clk,
		CastTickets:   auth.NewCastTicketSignerWithKey([]byte("test"), clk),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return castTestEnv{srv: srv, cookie: addSessionCookie(t, store, sm, 1), clk: clk, allowed: &allowed}
}

func (e castTestEnv) do(method, path string, cookie bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if cookie {
		req.AddCookie(e.cookie)
	}
	rr := httptest.NewRecorder()
	e.srv.ServeHTTP(rr, req)
	return rr
}

func (e castTestEnv) ticket(t *testing.T) castTicketResponse {
	t.Helper()
	rr := e.do(http.MethodPost, "/api/v1/media/5/cast", true)
	if rr.Code != http.StatusOK {
		t.Fatalf("create ticket: %d %s", rr.Code, rr.Body.String())
	}
	var got castTicketResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCreateCastTicket(t *testing.T) {
	env := newCastTestEnv(t)
	got := env.ticket(t)
	if got.Ticket == "" || got.StreamContentType != "audio/mpeg" || got.CompatContentType != "audio/mp4" {
		t.Fatalf("response = %+v", got)
	}
	if want := env.clk.T.Add(castTicketTTL).Truncate(time.Second); !got.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at = %v, want %v", got.ExpiresAt, want)
	}

	for name, tc := range map[string]struct {
		path   string
		cookie bool
		want   int
	}{
		"signed out":    {"/api/media/5/cast", false, http.StatusUnauthorized},
		"unknown media": {"/api/media/6/cast", true, http.StatusNotFound},
		"bad id":        {"/api/media/x/cast", true, http.StatusBadRequest},
	} {
		if rr := env.do(http.MethodPost, tc.path, tc.cookie); rr.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rr.Code, tc.want)
		}
	}
}

func TestCastRoutes_ServeWithoutSession(t *testing.T) {
	env := newCastTestEnv(t)
	ticket := env.ticket(t).Ticket
	for path, want := range map[string]string{
		"/cast/" + ticket + "/stream":    "ID3audio-bytes",
		"/cast/" + ticket + "/compat":    "m4a-bytes",
		"/cast/" + ticket + "/thumbnail": "jpeg",
	} {
		rr := env.do(http.MethodGet, path, false)
		if rr.Code != http.StatusOK || rr.Body.String() != want {
			t.Fatalf("%s: %d %q", path, rr.Code, rr.Body.String())
		}
		if rr.Header().Get("Access-Control-Allow-Origin") != "*" || rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: headers %v", path, rr.Header())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/cast/"+ticket+"/stream", nil)
	req.Header.Set("Range", "bytes=3-7")
	rr := httptest.NewRecorder()
	env.srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusPartialContent || rr.Body.String() != "audio" {
		t.Fatalf("range: %d %q", rr.Code, rr.Body.String())
	}

	if rr := env.do(http.MethodOptions, "/cast/"+ticket+"/stream", false); rr.Code != http.StatusNoContent ||
		rr.Header().Get("Access-Control-Allow-Headers") != "Range" {
		t.Fatalf("preflight: %d %v", rr.Code, rr.Header())
	}
	if rr := env.do(http.MethodPost, "/cast/"+ticket+"/stream", false); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rr.Code)
	}
}

func TestCastRoutes_Reject(t *testing.T) {
	env := newCastTestEnv(t)
	ticket := env.ticket(t).Ticket

	for name, path := range map[string]string{
		"forged":       "/cast/AAAA/stream",
		"unknown leaf": "/cast/" + ticket + "/download",
	} {
		if rr := env.do(http.MethodGet, path, false); rr.Code != http.StatusNotFound {
			t.Errorf("%s: status %d", name, rr.Code)
		}
	}

	// Access is checked on every request, not only when the ticket is issued.
	*env.allowed = false
	if rr := env.do(http.MethodGet, "/cast/"+ticket+"/stream", false); rr.Code != http.StatusNotFound {
		t.Fatalf("revoked: status %d", rr.Code)
	}
	*env.allowed = true

	env.clk.T = env.clk.T.Add(castTicketTTL + time.Second)
	if rr := env.do(http.MethodGet, "/cast/"+ticket+"/stream", false); rr.Code != http.StatusNotFound {
		t.Fatalf("expired: status %d", rr.Code)
	}
}
