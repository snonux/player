package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
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
		id, err := store.CreateMedia(ctx, &model.Media{SetID: set, FileName: strconv.Itoa(i) + ".mp3", RelPath: strconv.Itoa(i) + ".mp3", AbsPath: "/not-read/" + strconv.Itoa(i), Type: model.MediaTypeAudio, Duration: 10, ThumbnailPath: "art", CreatedAt: now, Music: &model.MusicMetadata{Genres: []string{[]string{"Visible genre", "Restricted only", "Restricted only"}[i]}, Album: title, AlbumArtist: "Same Artist", Track: i + 1, Disc: i + 1, DiscSubtitle: []string{"Visible subtitle", "Hidden subtitle", "Private"}[i]}})
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
	hiddenFacet := request("", "?genre=Restricted%20only")
	if hiddenFacet.Code != 200 || strings.TrimSpace(hiddenFacet.Body.String()) != "[]" {
		t.Fatalf("facet ACL leaked: %s", hiddenFacet.Body.String())
	}
	badFacet := request("", "?year=2029-2020")
	if badFacet.Code != 400 {
		t.Fatalf("invalid facet accepted: %d", badFacet.Code)
	}
	facetRequest := httptest.NewRequest("GET", "/api/music/facets", nil)
	facetRequest = facetRequest.WithContext(context.WithValue(facetRequest.Context(), sessionCtxKey, &model.Session{UserID: user}))
	facetResponse := httptest.NewRecorder()
	server.handleMusicFacets(facetResponse, facetRequest)
	if facetResponse.Code != 200 || strings.Contains(facetResponse.Body.String(), "Restricted only") || !strings.Contains(facetResponse.Body.String(), "Visible genre") {
		t.Fatalf("facet options ACL: %s", facetResponse.Body.String())
	}
	// Queue ordering exposes only requested, permitted canonical membership.
	queueRequest := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/music/queue-albums", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: user}))
		w := httptest.NewRecorder()
		server.handleMusicQueueAlbums(w, r)
		return w
	}
	queueResult := queueRequest(`{"media_ids":[` + strconv.FormatInt(ids[0], 10) + `,` + strconv.FormatInt(ids[1], 10) + `]}`)
	var memberships map[string]struct {
		Key   string `json:"album_key"`
		Order int    `json:"order"`
	}
	if queueResult.Code != 200 {
		t.Fatalf("queue membership: %d %s", queueResult.Code, queueResult.Body.String())
	}
	if err := json.Unmarshal(queueResult.Body.Bytes(), &memberships); err != nil {
		t.Fatal(err)
	}
	if len(memberships) != 1 || memberships[strconv.FormatInt(ids[0], 10)].Key == "" || memberships[strconv.FormatInt(ids[0], 10)].Order != 0 {
		t.Fatalf("queue ACL: %+v", memberships)
	}
	if w := queueRequest(`{"media_ids":[]}`); strings.TrimSpace(w.Body.String()) != "{}" {
		t.Fatalf("unrequested IDs: %s", w.Body.String())
	}
	for _, body := range []string{`{}`, `{"media_ids":null}`, `{"media_ids":[0]}`, `{"media_ids":[-1]}`, `{"media_ids":[1.5]}`, `{"media_ids":[9007199254740992]}`, `bad`} {
		if w := queueRequest(body); w.Code != 400 {
			t.Fatalf("invalid IDs accepted: %s", body)
		}
	}
	if w := queueRequest(`{"media_ids":[` + strings.Repeat("1,", 10000) + `1]}`); w.Code != 400 {
		t.Fatal("oversized occurrence set accepted")
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
	if len(albums[0].Discs) != 0 {
		t.Fatal("summary embeds discs")
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
	if len(album.Discs) != 1 || album.Discs[0].Subtitle != "Visible subtitle" || album.Discs[0].CoverMediaID != ids[0] || len(album.Discs[0].SongIDs) != 1 {
		t.Fatalf("disc ACL leak %+v", album.Discs)
	}
	if resolved := request("", "?media_id="+strconv.FormatInt(ids[1], 10)); resolved.Body.String() != "[]\n" {
		t.Fatalf("hidden media resolver: %s", resolved.Body.String())
	}

	for _, query := range []string{"?q=Private", "?q=Hidden", "?artist=Secret"} {
		r := httptest.NewRequest("GET", "/api/music/search"+query, nil)
		r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: user}))
		w := httptest.NewRecorder()
		server.handleMusicSearch(w, r)
		if w.Code != 200 || w.Body.String() != "{\"songs\":[],\"albums\":[],\"artists\":[]}\n" {
			t.Fatalf("search ACL leak %s", w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/api/music/search?q=Shared", nil)
	r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: user}))
	w := httptest.NewRecorder()
	server.handleMusicSearch(w, r)
	var search service.MusicSearchResult
	if err := json.Unmarshal(w.Body.Bytes(), &search); err != nil {
		t.Fatal(err)
	}
	if len(search.Songs) != 1 || len(search.Albums) != 1 || search.Albums[0].SongCount != 1 || search.Songs[0].ID != ids[0] {
		t.Fatalf("search aggregate leaked %+v", search)
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

func TestMusicArtistsRealPermissions(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	uid, err := store.CreateUser(ctx, &model.User{Username: "credits-viewer", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	sets := make([]int64, 2)
	for i, name := range []string{"Visible", "Hidden"} {
		sets[i], err = store.CreateSet(ctx, &model.Set{Name: name, RootPath: name, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		composer := "Shared Composer"
		if i == 1 {
			composer = "Hidden Composer"
		}
		_, err = store.CreateMedia(ctx, &model.Media{SetID: sets[i], FileName: "song.flac", RelPath: "song.flac", AbsPath: "/not-read/" + name, Type: model.MediaTypeAudio, CreatedAt: now, Music: &model.MusicMetadata{Album: "Shared", Artist: "Track Artist", AlbumArtist: "Album Artist", Composers: []string{composer}, Performers: []string{"Player"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	grant := func(set int64) {
		t.Helper()
		if err := store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: uid, Role: model.RoleViewer}); err != nil {
			t.Fatal(err)
		}
	}
	grant(sets[0])
	svc := service.NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	server := &Server{media: MediaServices{Browse: svc}, logger: slog.Default()}
	request := func(key, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/music/artists"+query, nil)
		r.SetPathValue("artist_key", key)
		r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: uid}))
		w := httptest.NewRecorder()
		server.handleMusicArtists(w, r)
		return w
	}
	read := func(role string) []service.Artist {
		t.Helper()
		w := request("", "?role="+role)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var people []service.Artist
		if err := json.Unmarshal(w.Body.Bytes(), &people); err != nil {
			t.Fatal(err)
		}
		return people
	}
	if people := read("artists"); len(people) != 2 {
		t.Fatalf("default leaked composer/performer %+v", people)
	}
	composers := read("composer")
	if len(composers) != 1 || composers[0].Name != "Shared Composer" || composers[0].SongCount != 1 || composers[0].AlbumCount != 1 {
		t.Fatalf("composer ACL %+v", composers)
	}
	for _, query := range []string{"?role=composer&role=performer", "?role=performer&role=composer"} {
		w := request("", query)
		var matches []service.Artist
		if w.Code != 200 {
			t.Fatalf("repeated roles %d %s", w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &matches); err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, person := range matches {
			names[person.Name] = true
		}
		if len(names) != 2 || !names["Shared Composer"] || !names["Player"] {
			t.Fatalf("repeated role OR %s: %+v", query, matches)
		}
	}
	if w := request("", "?role=composer&role=invalid"); w.Code != 400 {
		t.Fatalf("invalid repeated role %d", w.Code)
	}
	if w := request("", "?role=invalid"); w.Code != 400 {
		t.Fatalf("unknown role %d", w.Code)
	}
	grant(sets[1])
	people := read("composer")
	var hidden string
	for _, p := range people {
		if p.Name == "Hidden Composer" {
			hidden = p.Key
		}
	}
	if hidden == "" {
		t.Fatal("hidden credit absent after grant")
	}
	var detail service.Artist
	w := request(hidden, "")
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(detail.Albums) != 1 {
		t.Fatalf("detail %s", w.Body.String())
	}
	if err := store.RevokePermission(ctx, sets[1], uid); err != nil {
		t.Fatal(err)
	}
	denied, missing := request(hidden, ""), request("ar_missing", "")
	if denied.Code != 404 || denied.Body.String() != missing.Body.String() {
		t.Fatal("hidden artist existence leaked")
	}
	if len(read("composer")) != 1 {
		t.Fatal("revoked composer retained")
	}
}

func TestMusicSearchRoutesAndAuth(t *testing.T) {
	env := newIntegrationServer(t)
	_, cookie := bootstrapAdmin(t, env)
	for _, path := range []string{"/api/music/search?q=anything", "/api/v1/music/search?q=anything"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		env.srv.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, response.Code)
		}
		request = httptest.NewRequest("GET", path, nil)
		request.AddCookie(cookie)
		response = httptest.NewRecorder()
		env.srv.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("authenticated %s: %d %s", path, response.Code, response.Body.String())
		}
	}
}
