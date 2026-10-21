package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

func TestMusicAlbumsRealPermissions(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	user, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	var sets []int64
	for _, name := range []string{"Visible", "Hidden"} {
		id, err := store.CreateSet(ctx, &model.Set{Name: name, RootPath: name, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		sets = append(sets, id)
	}
	if err := store.GrantPermission(ctx, &model.SetPermission{SetID: sets[0], UserID: user, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i, set := range []int64{sets[0], sets[1], sets[1]} {
		title := "Shared Album"
		if i == 2 {
			title = "Private Album"
		}
		id, err := store.CreateMedia(ctx, &model.Media{SetID: set, FileName: strconv.Itoa(i) + ".mp3", RelPath: strconv.Itoa(i) + ".mp3", AbsPath: "/not-read/" + strconv.Itoa(i), Type: model.MediaTypeAudio, Duration: 10, ThumbnailPath: "art", CreatedAt: now, Music: &model.MusicMetadata{Album: title, AlbumArtist: "Same Artist", Track: i + 1}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	svc := service.NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	server := &Server{media: MediaServices{Browse: svc}, logger: slog.Default()}
	request := func(key, query string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/music/albums"+query, nil)
		r.SetPathValue("album_key", key)
		r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: user}))
		w := httptest.NewRecorder()
		server.handleMusicAlbums(w, r)
		return w
	}
	list := request("", "")
	if list.Code != 200 {
		t.Fatalf("list %d %s", list.Code, list.Body.String())
	}
	var albums []service.Album
	if err := json.Unmarshal(list.Body.Bytes(), &albums); err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].SongCount != 1 || albums[0].Duration != 10 || albums[0].CoverMediaID != ids[0] {
		t.Fatalf("ACL aggregate leaked: %+v", albums)
	}
	if len(albums[0].Songs) != 0 {
		t.Fatal("list embeds songs")
	}
	detail := request(albums[0].Key, "")
	var album service.Album
	if err := json.Unmarshal(detail.Body.Bytes(), &album); err != nil {
		t.Fatal(err)
	}
	if detail.Code != 200 || len(album.Songs) != 1 || album.Songs[0].ID != ids[0] {
		t.Fatalf("detail leaked: %s", detail.Body.String())
	}
	if resolved := request("", "?media_id="+strconv.FormatInt(ids[1], 10)); resolved.Body.String() != "[]\n" {
		t.Fatalf("hidden media resolver: %s", resolved.Body.String())
	}
	// Granting permission changes aggregates immediately; derive the real private
	// key, revoke again and prove key knowledge confers no access.
	if err := store.GrantPermission(ctx, &model.SetPermission{SetID: sets[1], UserID: user, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	expanded := request("", "")
	if err := json.Unmarshal(expanded.Body.Bytes(), &albums); err != nil {
		t.Fatal(err)
	}
	var private string
	for _, a := range albums {
		if a.Title == "Private Album" {
			private = a.Key
		}
		if a.Title == "Shared Album" && a.SongCount != 2 {
			t.Fatal("cross-set album not merged")
		}
	}
	if private == "" {
		t.Fatal("private key absent after grant")
	}
	if err := store.RevokePermission(ctx, sets[1], user); err != nil {
		t.Fatal(err)
	}
	denied := request(private, "")
	missing := request("al_missing", "")
	if denied.Code != 404 || denied.Body.String() != missing.Body.String() {
		t.Fatalf("key existence leaked: %d %s", denied.Code, denied.Body.String())
	}
	if err := store.RevokePermission(ctx, sets[0], user); err != nil {
		t.Fatal(err)
	}
	if empty := request("", ""); empty.Code != 200 || empty.Body.String() != "[]\n" {
		t.Fatalf("no permission: %s", empty.Body.String())
	}
}
