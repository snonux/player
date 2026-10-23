package api

import (
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMusicEntityFavoritesAccountACLAndFilters(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	now := time.Now().UTC()
	user, err := store.CreateUser(ctx, &model.User{Username: "one", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, &model.User{Username: "two", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: user, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	media, err := store.CreateMedia(ctx, &model.Media{SetID: set, FileName: "a.mp3", RelPath: "a.mp3", Type: model.MediaTypeAudio, CreatedAt: now, Music: &model.MusicMetadata{Album: "A", Artist: "B"}})
	if err != nil {
		t.Fatal(err)
	}
	browse := service.NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	playlists := service.NewPlaylistService(store, browse)
	server := &Server{store: store, media: MediaServices{Browse: browse, Playlists: playlists}}
	albums, err := service.MusicAlbums(ctx, browse, user)
	if err != nil || len(albums) != 1 {
		t.Fatalf("albums %v %v", albums, err)
	}
	playlist, err := playlists.Create(ctx, user, "Private", "")
	if err != nil {
		t.Fatal(err)
	}
	heart := func(method, kind, key string, account int64) int {
		r := httptest.NewRequest(method, "/api/music/favorites/"+kind+"/"+key, nil)
		r.SetPathValue("kind", kind)
		r.SetPathValue("entity_key", key)
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: account}))
		w := httptest.NewRecorder()
		server.handleMusicFavorite(w, r)
		return w.Code
	}
	for _, entity := range [][2]string{{"album", albums[0].Key}, {"artist", service.MusicArtists(albums)[0].Key}, {"playlist", playlist.ID}} {
		for range 2 {
			if code := heart("PUT", entity[0], entity[1], user); code != 200 {
				t.Fatalf("save %v code=%d", entity, code)
			}
		}
		if code := heart("PUT", entity[0], entity[1], other); code != 404 {
			t.Fatalf("hidden %v code=%d", entity, code)
		}
	}
	r := httptest.NewRequest("GET", "/api/music/albums?favorite=true", nil)
	r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: user}))
	w := httptest.NewRecorder()
	server.handleMusicAlbums(w, r)
	var got []service.Album
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got) != 1 || !got[0].IsFavorite {
		t.Fatalf("filtered %s %v", w.Body, err)
	}

	for _, endpoint := range []struct {
		path   string
		handle func(http.ResponseWriter, *http.Request)
	}{{"artists", server.handleMusicArtists}, {"playlists", server.handlePlaylists}} {
		path := "/api/music/" + endpoint.path + "?favorite=true"
		if endpoint.path == "playlists" {
			path = "/api/playlists?favorite=true"
		}
		r := httptest.NewRequest("GET", path, nil)
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: user}))
		w := httptest.NewRecorder()
		endpoint.handle(w, r)
		var values []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &values); err != nil || len(values) != 1 || values[0]["is_favorite"] != true {
			t.Fatalf("%s favorites %s %v", endpoint.path, w.Body, err)
		}
	}
	if yes, err := store.IsFavorite(ctx, user, media); err != nil || yes {
		t.Fatalf("entity changed song heart %v %v", yes, err)
	}
	if code := heart("DELETE", "album", albums[0].Key, user); code != 200 {
		t.Fatal(code)
	}
	w = httptest.NewRecorder()
	server.handleMusicAlbums(w, r)
	if w.Body.String() != "[]\n" {
		t.Fatalf("unfavorite collection %s", w.Body)
	}

	shared := "shared"
	if _, err := playlists.UpdateDetails(ctx, user, playlist.ID, "Shared", "", &shared, nil); err != nil {
		t.Fatal(err)
	}
	if code := heart("PUT", "playlist", playlist.ID, other); code != 200 {
		t.Fatalf("shared reader heart %d", code)
	}
	private := "private"
	if _, err := playlists.UpdateDetails(ctx, user, playlist.ID, "Private", "", &private, nil); err != nil {
		t.Fatal(err)
	}
	hiddenRequest := httptest.NewRequest("GET", "/api/playlists?favorite=true", nil)
	hiddenRequest = hiddenRequest.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: other}))
	hiddenResponse := httptest.NewRecorder()
	server.handlePlaylists(hiddenResponse, hiddenRequest)
	if hiddenResponse.Body.String() != "[]\n" {
		t.Fatalf("permission loss exposed playlist %s", hiddenResponse.Body)
	}
	keys, err := store.ListMusicFavorites(ctx, other, "album")
	if err != nil || len(keys) != 0 {
		t.Fatalf("account scope %v %v", keys, err)
	}
}

func (m *mockPingStore) ListMusicFavorites(ctx context.Context, user int64, kind string) (map[string]bool, error) {
	return m.store.ListMusicFavorites(ctx, user, kind)
}
func (m *mockPingStore) SetMusicFavorite(ctx context.Context, user int64, kind, key string, value bool) error {
	return m.store.SetMusicFavorite(ctx, user, kind, key, value)
}
