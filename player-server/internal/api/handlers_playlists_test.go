package api

import (
	"context"
	"encoding/json"
	"log/slog"
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

func TestPlaylistAPIValidationOwnerAndSavedState(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	user, err := store.CreateUser(context.Background(), &model.User{Username: "p", PasswordHash: "x", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	browse := service.NewMediaService(store, &clock.MockClock{T: time.Now()}, "", nil, nil)
	s := &Server{media: MediaServices{Playlists: service.NewPlaylistService(store, browse)}, logger: slog.Default()}
	request := func(method, id, body string, owner int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/playlists", strings.NewReader(body))
		r.SetPathValue("playlist_id", id)
		r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: owner}))
		w := httptest.NewRecorder()
		s.handlePlaylists(w, r)
		return w
	}
	for _, body := range []string{`{`, `{"name":" "}`, `{"name":"ok","visibility":"public"}`, `{"name":"ok","owner_id":1}`, `{"name":"ok","unknown":true}`, `{"name":"ok","comment":` + "\"" + strings.Repeat("a", 2001) + "\"}"} {
		if w := request("POST", "", body, user); w.Code != 400 {
			t.Fatalf("bad request %d %s", w.Code, w.Body.String())
		}
	}
	w := request("POST", "", `{"name":"東京 🌙","comment":"line\\n<text>"}`, user)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var p service.PlaylistView
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "東京 🌙" || p.SongCount != 0 {
		t.Fatalf("create %+v", p)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		if w := request(method, p.ID, `{"name":"changed"}`, user+100); w.Code != 404 {
			t.Fatalf("foreign %s %d", method, w.Code)
		}
	}
	if w := request("PATCH", p.ID, `{"name":"Renamed","comment":"Saved comment"}`, user); w.Code != 200 {
		t.Fatalf("patch %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", p.ID, "", user); !strings.Contains(w.Body.String(), "Saved comment") {
		t.Fatal(w.Body.String())
	}

	other, err := store.CreateUser(context.Background(), &model.User{Username: "reader", PasswordHash: "x", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if w := request("PATCH", p.ID, `{"name":"Renamed","comment":"Saved comment","visibility":"shared"}`, user); w.Code != 200 {
		t.Fatalf("share %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", p.ID, "", other); w.Code != 200 || !strings.Contains(w.Body.String(), `"can_edit":false`) {
		t.Fatalf("shared reader %d %s", w.Code, w.Body.String())
	}
	for _, method := range []string{"PATCH", "DELETE"} {
		if w := request(method, p.ID, `{"name":"stolen"}`, other); w.Code != 403 {
			t.Fatalf("read-only %s %d %s", method, w.Code, w.Body.String())
		}
	}
	if w := request("PATCH", p.ID, `{"name":"Renamed","comment":"Saved comment","visibility":"private"}`, user); w.Code != 200 {
		t.Fatalf("private %d", w.Code)
	}
	if w := request("GET", p.ID, "", other); w.Code != 404 {
		t.Fatalf("private reader %d", w.Code)
	}
	if w := request("DELETE", p.ID, "", user); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	if w := request("GET", p.ID, "", user); w.Code != 404 {
		t.Fatalf("gone %d", w.Code)
	}
}

func TestPlaylistRoutesRequireSessionBothPrefixes(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	clk := &clock.MockClock{T: time.Now().UTC()}
	user, err := store.CreateUser(context.Background(), &model.User{Username: "routes", PasswordHash: "x", CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(store, clk, time.Hour)
	browse := service.NewMediaService(store, clk, "", nil, nil)
	srv := newTestServer(t, store, nil, sm, &internal.Config{}, browse, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	srv.media.Playlists = service.NewPlaylistService(store, browse)
	cookie := addSessionCookie(t, store, sm, user)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, authenticated := range []bool{false, true} {
			r := httptest.NewRequest("GET", prefix+"/playlists", nil)
			if authenticated {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			want := 401
			if authenticated {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("%s auth=%v got %d body=%s", prefix, authenticated, w.Code, w.Body.String())
			}
		}
	}
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, authenticated := range []bool{false, true} {
			r := httptest.NewRequest("POST", prefix+"/playlists/missing/songs", strings.NewReader(`{"action":"add","media_ids":[1]}`))
			if authenticated {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			want := 401
			if authenticated {
				want = 404
			}
			if w.Code != want {
				t.Fatalf("song route %s auth=%v %d %s", prefix, authenticated, w.Code, w.Body.String())
			}
		}
	}

}
