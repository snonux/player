package api

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

func TestMusicArtworkPermissionsAndInvalidation(t *testing.T) {
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
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: viewer, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateMedia(ctx, &model.Media{SetID: set, FileName: "song.mp3", RelPath: "song.mp3", Type: model.MediaTypeAudio, CreatedAt: now, Music: &model.MusicMetadata{Artist: "Artist", Album: "Album"}})
	if err != nil {
		t.Fatal(err)
	}
	browse := service.NewMediaService(store, clk, "", nil, nil)
	albums, err := service.MusicAlbums(ctx, browse, viewer)
	if err != nil {
		t.Fatal(err)
	}
	key := service.MusicArtists(albums)[0].Key
	playlists := service.NewPlaylistService(store, browse)
	p, err := playlists.Create(ctx, admin, "Shared", "note")
	if err != nil {
		t.Fatal(err)
	}
	visibility := "shared"
	if _, err := playlists.UpdateDetails(ctx, admin, p.ID, p.Name, p.Comment, &visibility, nil); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store, media: MediaServices{Browse: browse, Playlists: playlists}}
	request := func(method, kind, key string, user int64, data []byte, image bool, etag string) *httptest.ResponseRecorder {
		path := "/api/music/artwork/" + kind + "/" + key
		if image {
			path += "/image"
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.SetPathValue("kind", kind)
		r.SetPathValue("entity_key", key)
		r.Header.Set("If-None-Match", etag)
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: user}))
		w := httptest.NewRecorder()
		server.handleMusicArtwork(w, r)
		return w
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"artist", "playlist"} {
		entity := key
		if kind == "playlist" {
			entity = p.ID
		}
		if w := request("PUT", kind, entity, viewer, encoded.Bytes(), false, ""); w.Code != 403 {
			t.Fatalf("viewer changed %s: %d", kind, w.Code)
		}
		if w := request("PUT", kind, entity, admin, []byte("invalid"), false, ""); w.Code != 400 {
			t.Fatalf("invalid %d", w.Code)
		}
		if w := request("PUT", kind, entity, admin, encoded.Bytes(), false, ""); w.Code != 200 {
			t.Fatalf("upload %d %s", w.Code, w.Body)
		}
		if kind == "playlist" {
			detail, err := playlists.Get(ctx, viewer, entity)
			if err != nil || detail.ArtworkRevision == "" {
				t.Fatal("detail missing custom revision", err)
			}
			catalog, err := playlists.List(ctx, viewer)
			if err != nil || len(catalog) != 1 || catalog[0].ArtworkRevision != detail.ArtworkRevision {
				t.Fatal("catalog missing current custom revision", err)
			}
		}
		w := request("GET", kind, entity, viewer, nil, true, "")
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("read %d", w.Code)
		}
		if got := request("GET", kind, entity, viewer, nil, true, w.Header().Get("ETag")); got.Code != 304 {
			t.Fatalf("etag %d", got.Code)
		}
	}
	if err := store.RevokePermission(ctx, set, viewer); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "artist", key, viewer, nil, true, ""); w.Code != 404 {
		t.Fatalf("revoked read %d", w.Code)
	}
	if w := request("DELETE", "artist", key, admin, nil, false, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "artist", key, admin, nil, true, ""); w.Code != 404 {
		t.Fatal("remove", w.Code)
	}
	if err := playlists.Delete(ctx, admin, p.ID); err != nil {
		t.Fatal(err)
	}
	if art, err := store.GetMusicArtwork(ctx, "playlist", p.ID); err != nil || art != nil {
		t.Fatal("playlist art not cleaned", err)
	}
}
