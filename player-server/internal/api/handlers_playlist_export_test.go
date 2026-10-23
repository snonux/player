package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

func TestPlaylistExportRoutesAndHeaders(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clk := &clock.MockClock{T: time.Now().UTC()}
	user, err := store.CreateUser(context.Background(), &model.User{Username: "export", PasswordHash: "x", CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(store, clk, time.Hour)
	browse := service.NewMediaService(store, clk, "", nil, nil)
	srv := newTestServer(t, store, nil, sm, &internal.Config{}, browse, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	srv.media.Playlists = service.NewPlaylistService(store, browse)
	p, err := srv.media.Playlists.Create(context.Background(), user, "Unicode 東京", "")
	if err != nil {
		t.Fatal(err)
	}
	cookie := addSessionCookie(t, store, sm, user)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, authorized := range []bool{false, true} {
			r := httptest.NewRequest("GET", prefix+"/playlists/"+p.ID+"/export.m3u", nil)
			if authorized {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			want := 401
			if authorized {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("%s auth=%v: %d %s", prefix, authorized, w.Code, w.Body.String())
			}
			if !authorized {
				continue
			}
			if !strings.Contains(w.Header().Get("Content-Type"), "mpegurl") || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "playlist-"+p.ID+".m3u8") || !strings.HasPrefix(w.Body.String(), "#EXTM3U\n#PLAYLIST:Unicode 東京\n") {
				t.Fatalf("headers/body %v %s", w.Header(), w.Body.String())
			}
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/playlists/missing/export.m3u", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("missing: %d %s", w.Code, w.Body.String())
	}
}
