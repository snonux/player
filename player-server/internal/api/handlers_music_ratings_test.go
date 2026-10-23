package api

import (
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func (m *mockPingStore) ListMusicRatings(ctx context.Context, user int64, kind string) (map[string]int, error) {
	return m.store.ListMusicRatings(ctx, user, kind)
}
func (m *mockPingStore) SetMusicRating(ctx context.Context, user int64, kind, key string, rating int) error {
	return m.store.SetMusicRating(ctx, user, kind, key, rating)
}

func TestMusicRatingsAccountACLAndFilters(t *testing.T) {
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
	for _, id := range []int64{user, other} {
		if err := store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: id, Role: model.RoleViewer}); err != nil {
			t.Fatal(err)
		}
	}
	media, err := store.CreateMedia(ctx, &model.Media{SetID: set, FileName: "a.mp3", RelPath: "a.mp3", Type: model.MediaTypeAudio, CreatedAt: now, Music: &model.MusicMetadata{Title: "Song", Album: "Album", Artist: "Artist"}})
	if err != nil {
		t.Fatal(err)
	}
	browse := service.NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	server := &Server{store: store, media: MediaServices{Browse: browse}}
	albums, err := service.MusicAlbums(ctx, browse, user)
	if err != nil || len(albums) != 1 {
		t.Fatalf("catalog %v %v", albums, err)
	}
	keys := map[string]string{"song": strconv.FormatInt(media, 10), "album": albums[0].Key, "artist": service.MusicArtists(albums)[0].Key}
	rate := func(kind, key, body string, account int64) int {
		r := httptest.NewRequest("PUT", "/api/music/ratings/"+kind+"/"+key, strings.NewReader(body))
		r.SetPathValue("kind", kind)
		r.SetPathValue("entity_key", key)
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: account}))
		w := httptest.NewRecorder()
		server.handleMusicRating(w, r)
		return w.Code
	}
	get := func(path string, account int64, handle http.HandlerFunc) []byte {
		r := httptest.NewRequest("GET", path, nil).WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: account}))
		w := httptest.NewRecorder()
		handle(w, r)
		if w.Code != 200 {
			t.Fatalf("get %s %d %s", path, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	for kind, key := range keys {
		for _, value := range []int{1, 5, 3} {
			if code := rate(kind, key, fmt.Sprintf(`{"rating":%d}`, value), user); code != 200 {
				t.Fatalf("rate %s %d", kind, code)
			}
		}
		if values, err := store.ListMusicRatings(ctx, other, kind); err != nil || len(values) != 0 {
			t.Fatalf("account leak %v %v", values, err)
		}
		if code := rate(kind, "missing", `{"rating":4}`, user); code != 404 {
			t.Fatalf("missing %s %d", kind, code)
		}
	}
	for _, body := range []string{`{}`, `{"rating":null}`, `{"rating":-1}`, `{"rating":6}`, `{"rating":2.5}`, `{"rating":"5"}`, `{"rating":true}`, `{"rating":3,"extra":1}`} {
		if code := rate("album", keys["album"], body, user); code != 400 {
			t.Fatalf("invalid %s %d", body, code)
		}
	}
	if code := rate("playlist", "anything", `{"rating":5}`, user); code != 400 {
		t.Fatalf("unsupported %d", code)
	}
	for _, endpoint := range []struct {
		path   string
		handle http.HandlerFunc
	}{{"albums", server.handleMusicAlbums}, {"artists", server.handleMusicArtists}} {
		var values []map[string]any
		if err := json.Unmarshal(get("/api/music/"+endpoint.path+"?rated=true&sort=rating", user, endpoint.handle), &values); err != nil || len(values) != 1 || values[0]["rating"] != float64(3) {
			t.Fatalf("rated %s %v %v", endpoint.path, values, err)
		}
		if string(get("/api/music/"+endpoint.path+"?rated=true", other, endpoint.handle)) != "[]\n" {
			t.Fatal("other account rated list")
		}
	}
	var search service.MusicSearchResult
	if err := json.Unmarshal(get("/api/music/search?rated=true&sort=rating", user, server.handleMusicSearch), &search); err != nil || len(search.Songs) != 1 || search.Songs[0].Rating != 3 || len(search.Albums) != 1 || len(search.Artists) != 1 || search.Artists[0].Rating != 3 {
		t.Fatalf("search %+v %v", search, err)
	}
	for kind, key := range keys {
		if code := rate(kind, key, `{"rating":0}`, user); code != 200 {
			t.Fatal(code)
		}
	}
	if yes, err := store.IsFavorite(ctx, user, media); err != nil || yes {
		t.Fatalf("stars changed hearts %v %v", yes, err)
	}
	for kind, key := range keys {
		if code := rate(kind, key, `{"rating":5}`, other); code != 200 {
			t.Fatal(code)
		}
	}
	if err := store.RevokePermission(ctx, set, other); err != nil {
		t.Fatal(err)
	}
	for kind, key := range keys {
		if code := rate(kind, key, `{"rating":0}`, other); code != 404 {
			t.Fatalf("revoked %s %d", kind, code)
		}
	}
	if err := json.Unmarshal(get("/api/music/search?rated=true", other, server.handleMusicSearch), &search); err != nil || len(search.Songs) != 0 || len(search.Albums) != 0 || len(search.Artists) != 0 {
		t.Fatalf("revoked leak %+v %v", search, err)
	}
	if err := store.HardDeleteMedia(ctx, media); err != nil {
		t.Fatal(err)
	}
	values, err := store.ListMusicRatings(ctx, other, "song")
	if err != nil || len(values) != 0 {
		t.Fatalf("song cleanup %v %v", values, err)
	}
}
