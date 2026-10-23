package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/player/internal/jukebox"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

type jukeboxBrowseStub struct {
	root string
}

func (j jukeboxBrowseStub) StreamMedia(_ context.Context, mediaID, _ int64) (*service.FileResult, error) {
	if mediaID <= 0 {
		return nil, service.ErrNotFound
	}
	return &service.FileResult{
		Path:     filepath.Join(j.root, "track.mp3"),
		FileName: "track.mp3",
		Duration: 42,
	}, nil
}

func TestJukebox_API_PermissionAndControl(t *testing.T) {
	root := t.TempDir()
	fake := jukebox.NewFake()
	engine := service.NewJukeboxEngine(true, root, fake, jukeboxBrowseStub{root: root})
	svc := service.NewJukeboxService(engine)

	store := &repository.MockStore{}
	store.UserRepo.GetUserByIDFunc = func(_ context.Context, id int64) (*model.User, error) {
		return &model.User{ID: id, IsAdmin: id == 1}, nil
	}
	srv := &Server{store: store, jukeboxSvc: svc}

	viewerGet := httptest.NewRequest(http.MethodGet, "/api/jukebox", nil)
	viewerGet = viewerGet.WithContext(context.WithValue(viewerGet.Context(), sessionCtxKey, &model.Session{UserID: 2}))
	rec := httptest.NewRecorder()
	srv.handleJukeboxStatus(rec, viewerGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer status: %d %s", rec.Code, rec.Body.String())
	}
	var status jukebox.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.CanControl {
		t.Fatal("viewer must not control")
	}

	body := `{"session_id":"0123456789abcdef0123456789abcdef","revision":1,"action":"load","media_id":9,"position":1.5}`
	viewerPost := httptest.NewRequest(http.MethodPost, "/api/jukebox/control", strings.NewReader(body))
	viewerPost = viewerPost.WithContext(context.WithValue(viewerPost.Context(), sessionCtxKey, &model.Session{UserID: 2}))
	rec = httptest.NewRecorder()
	srv.handleJukeboxControl(rec, viewerPost)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer control: %d %s", rec.Code, rec.Body.String())
	}

	adminPost := httptest.NewRequest(http.MethodPost, "/api/jukebox/control", strings.NewReader(body))
	adminPost = adminPost.WithContext(context.WithValue(adminPost.Context(), sessionCtxKey, &model.Session{UserID: 1}))
	rec = httptest.NewRecorder()
	srv.handleJukeboxControl(rec, adminPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin load: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "PLAYING" {
		t.Fatalf("unexpected status %+v", status)
	}
	if len(fake.Loads) != 1 {
		t.Fatalf("loads=%v", fake.Loads)
	}
}
