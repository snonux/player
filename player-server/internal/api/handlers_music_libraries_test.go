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
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

func TestMusicLibraries_AdminAndCallerACL(t *testing.T) {
	dir := t.TempDir()
	store, err := repository.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	clk := &clock.MockClock{T: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	rootA := filepath.Join(dir, "a")
	rootB := filepath.Join(dir, "b")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(filepath.Join(root, "Music"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	roots := service.NewPathRoots(rootA)
	media := service.NewMediaService(store, clk, rootA, nil, nil)
	libs := service.NewMusicLibraryService(store, clk, roots, media)
	if _, err := libs.EnsureDefault(ctx, rootA); err != nil {
		t.Fatal(err)
	}
	libB, err := libs.Create(ctx, "Other", rootB)
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "x", IsAdmin: true, CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}
	viewerID, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "x", CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSet(ctx, &model.Set{LibraryID: libB.ID, Name: "Music", RootPath: "Music", CreatedAt: clk.T}); err != nil {
		t.Fatal(err)
	}
	if err := libs.GrantAccess(ctx, libB.ID, viewerID, model.RoleViewer); err != nil {
		t.Fatal(err)
	}

	hasher := auth.NewBCryptHasher(4)
	sm := auth.NewSessionManager(store, clk, time.Hour)
	cfg := &internal.Config{Port: 0, MediaRoot: rootA, SecureCookies: false}
	authSvc := &service.MockAuthService{
		CountUsersFunc: func(context.Context) (int, error) { return 2, nil },
		GetUserByIDFunc: func(_ context.Context, id int64) (*model.User, error) {
			return store.GetUserByID(ctx, id)
		},
	}
	srv := newTestServer(t, store, hasher, sm, cfg, media, nil, nil, nil, nil, nil, nil, nil, authSvc, nil,
		service.NewMediaStreamer(nil, rootA).WithPathRoots(roots))
	srv.musicLibs = libs
	srv.pathRoots = roots

	if err := store.CreateSession(ctx, &model.Session{ID: "sa", UserID: adminID, ExpiresAt: clk.T.Add(time.Hour), CreatedAt: clk.T}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, &model.Session{ID: "sv", UserID: viewerID, ExpiresAt: clk.T.Add(time.Hour), CreatedAt: clk.T}); err != nil {
		t.Fatal(err)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/admin/libraries", nil)
	adminReq.AddCookie(&http.Cookie{Name: "session", Value: "sa"})
	adminRR := httptest.NewRecorder()
	srv.ServeHTTP(adminRR, adminReq)
	if adminRR.Code != http.StatusOK {
		t.Fatalf("admin libraries: %d %s", adminRR.Code, adminRR.Body)
	}
	var adminLibs []model.MusicLibrary
	if err := json.Unmarshal(adminRR.Body.Bytes(), &adminLibs); err != nil || len(adminLibs) != 2 {
		t.Fatalf("admin body: %s err=%v", adminRR.Body, err)
	}

	viewerReq := httptest.NewRequest(http.MethodGet, "/api/music/libraries", nil)
	viewerReq.AddCookie(&http.Cookie{Name: "session", Value: "sv"})
	viewerRR := httptest.NewRecorder()
	srv.ServeHTTP(viewerRR, viewerReq)
	if viewerRR.Code != http.StatusOK {
		t.Fatalf("viewer libraries: %d %s", viewerRR.Code, viewerRR.Body)
	}
	var viewerLibs []model.MusicLibrary
	if err := json.Unmarshal(viewerRR.Body.Bytes(), &viewerLibs); err != nil || len(viewerLibs) != 1 || viewerLibs[0].ID != libB.ID {
		t.Fatalf("viewer should only see Other: %s", viewerRR.Body)
	}
}
