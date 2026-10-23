package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

func TestMusicEnrichmentACLAndRefresh(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	clk := &clock.MockClock{T: now}
	admin, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "x", IsAdmin: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, &model.User{Username: "other", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: viewer, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateMedia(ctx, &model.Media{SetID: set, FileName: "song.mp3", RelPath: "song.mp3", Type: model.MediaTypeAudio, CreatedAt: now, Music: &model.MusicMetadata{Title: "Song", Artist: "Artist", Album: "Album"}})
	if err != nil {
		t.Fatal(err)
	}
	browse := service.NewMediaService(store, clk, "", nil, nil)
	albums, err := service.MusicAlbums(ctx, browse, viewer)
	if err != nil || len(albums) != 1 {
		t.Fatalf("albums %v %v", albums, err)
	}
	calls := 0
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"description":"Fixture description","source":"Fixture"}`))
	}))
	defer agent.Close()
	server := &Server{store: store, media: MediaServices{Browse: browse}, cfg: &internal.Config{MusicEnrichmentURL: agent.URL}, clk: clk}
	request := func(method, key string, user int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/music/enrichment/album/"+key, nil)
		r.SetPathValue("kind", "album")
		r.SetPathValue("entity_key", key)
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: user}))
		w := httptest.NewRecorder()
		server.handleMusicEnrichment(w, r)
		return w
	}
	for _, tc := range []struct {
		method, key string
		user        int64
		status      int
	}{{"GET", albums[0].Key, viewer, 200}, {"POST", albums[0].Key, viewer, 403}, {"GET", albums[0].Key, other, 404}, {"POST", "hidden", admin, 404}, {"POST", albums[0].Key, admin, 200}} {
		w := request(tc.method, tc.key, tc.user)
		if w.Code != tc.status {
			t.Fatalf("%+v => %d %s", tc, w.Code, w.Body)
		}
	}
	if calls != 1 {
		t.Fatalf("unauthorized/GET fetched externally: %d", calls)
	}
	if err := store.RevokePermission(ctx, set, viewer); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", albums[0].Key, viewer); w.Code != 404 {
		t.Fatalf("cached metadata leaked after revocation: %d %s", w.Code, w.Body)
	}
}
