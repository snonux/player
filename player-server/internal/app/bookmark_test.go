package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestBookmarkAPI(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	mid := e.media["movie.mp4"]
	m, err := e.store.GetMediaByID(ctx, mid)
	e.must(err)
	m.Duration = 120
	e.must(e.store.UpdateMedia(ctx, m))
	e.must(e.store.GrantPermission(ctx, &model.SetPermission{UserID: e.users["bob"], SetID: m.SetID, Role: model.RoleViewer, CreatedAt: time.Now()}))
	path := "/api/media/" + strconv.FormatInt(mid, 10) + "/bookmarks"
	v1path := strings.Replace(path, "/api/", "/api/v1/", 1)
	create := func(path, user string, position float64, label string) model.Bookmark {
		t.Helper()
		body, err := json.Marshal(map[string]any{"position_seconds": position, "label": label})
		e.must(err)
		rr := e.request(ctx, "POST", path, user, string(body))
		if rr.Code != http.StatusCreated {
			t.Fatalf("create as %s = %d %s", user, rr.Code, rr.Body)
		}
		var b model.Bookmark
		e.must(json.Unmarshal(rr.Body.Bytes(), &b))
		if b.ID <= 0 || b.MediaID != mid || b.PositionSeconds != position || b.Label != strings.TrimSpace(label) || b.CreatedAt.IsZero() || strings.Contains(rr.Body.String(), "user_id") {
			t.Fatalf("create result = %s", rr.Body)
		}
		return b
	}
	list := func(path, user string) []model.Bookmark {
		t.Helper()
		rr := e.get(path, user)
		if rr.Code != 200 {
			t.Fatalf("list as %s = %d %s", user, rr.Code, rr.Body)
		}
		var items []model.Bookmark
		e.must(json.Unmarshal(rr.Body.Bytes(), &items))
		if items == nil {
			t.Fatal("list must return an array")
		}
		return items
	}
	if len(list(path, "alice")) != 0 {
		t.Fatal("new media has bookmarks")
	}
	// Preserve the existing personal note and resume position.
	e.must(e.store.UpsertNote(ctx, &model.Note{UserID: e.users["alice"], MediaID: mid, Content: "my note", CreatedAt: time.Now(), UpdatedAt: time.Now()}))
	e.must(e.store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: e.users["alice"], MediaID: mid, PositionSeconds: 42, UpdatedAt: time.Now()}))
	last := create(path, "alice", 120, "  end  ")
	first := create(v1path, "alice", 0, "")
	second := create(path, "alice", 0, strings.Repeat("章", 200))
	other := create(v1path, "bob", 12.5, "Bob's chapter")
	for _, route := range []string{path, v1path} {
		items := list(route, "alice")
		if len(items) != 3 || items[0].ID != first.ID || items[1].ID != second.ID || items[2].ID != last.ID {
			t.Fatalf("ordered list = %+v", items)
		}
		if items := list(route, "bob"); len(items) != 1 || items[0].ID != other.ID {
			t.Fatalf("Bob's list = %+v", items)
		}
		if len(list(route, "admin")) != 0 {
			t.Fatal("admin sees another user's bookmarks")
		}
		for _, user := range []string{"bob", "admin"} {
			if rr := e.request(ctx, "DELETE", route+"/"+strconv.FormatInt(first.ID, 10), user, ""); rr.Code != 404 {
				t.Fatalf("other user delete = %d %s", rr.Code, rr.Body)
			}
		}
	}
	wrongMedia := fmt.Sprintf("/api/media/%d/bookmarks/%d", e.media["clip.avi"], first.ID)
	if rr := e.request(ctx, "DELETE", wrongMedia, "alice", ""); rr.Code != 404 {
		t.Fatalf("wrong media delete = %d", rr.Code)
	}
	// The Bearer route uses the same user scope as the session cookie route.
	token, err := e.deps.AuthSvc.CreateAPIToken(ctx, e.users["alice"], "bookmarks", nil)
	e.must(err)
	req := httptest.NewRequest("GET", v1path, nil)
	req.Header.Set("Authorization", "Bearer "+token.Plaintext)
	rr := httptest.NewRecorder()
	e.server.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"label":"end"`) {
		t.Fatalf("Bearer list = %d %s", rr.Code, rr.Body)
	}
	for _, route := range []string{path, v1path} {
		id := first.ID
		if route == v1path {
			id = second.ID
		}
		deletePath := route + "/" + strconv.FormatInt(id, 10)
		if rr := e.request(ctx, "DELETE", deletePath, "alice", ""); rr.Code != 204 || rr.Body.Len() != 0 {
			t.Fatalf("delete = %d %s", rr.Code, rr.Body)
		}
		if rr := e.request(ctx, "DELETE", deletePath, "alice", ""); rr.Code != 404 {
			t.Fatalf("delete absent = %d", rr.Code)
		}
	}
	if items := list(path, "alice"); len(items) != 1 || items[0].ID != last.ID {
		t.Fatalf("remaining bookmarks = %+v", items)
	}
	progress, err := e.store.GetProgress(ctx, e.users["alice"], mid)
	e.must(err)
	note, err := e.store.GetNote(ctx, mid, e.users["alice"])
	e.must(err)
	if progress.PositionSeconds != 42 || note.Content != "my note" {
		t.Fatal("bookmarks changed notes or progress")
	}
	// Revocation immediately blocks all operations, even on an owned bookmark.
	e.must(e.store.RevokePermission(ctx, m.SetID, e.users["alice"]))
	for _, method := range []string{"GET", "POST", "DELETE"} {
		route := path
		if method == "DELETE" {
			route += "/" + strconv.FormatInt(last.ID, 10)
		}
		if rr := e.request(ctx, method, route, "alice", `{"position_seconds":1}`); rr.Code != 403 {
			t.Fatalf("revoked %s = %d %s", method, rr.Code, rr.Body)
		}
	}
}

func TestBookmarkAPIValidation(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	mid := e.media["movie.mp4"]
	m, err := e.store.GetMediaByID(ctx, mid)
	e.must(err)
	m.Duration = 60
	e.must(e.store.UpdateMedia(ctx, m))
	for _, prefix := range []string{"/api", "/api/v1"} {
		path := fmt.Sprintf("%s/media/%d/bookmarks", prefix, mid)
		for _, body := range []string{
			``, `{`, `null`, `[]`, `{}`, `{"position_seconds":null}`, `{"label":"missing position"}`,
			`{"position_seconds":"1"}`, `{"position_seconds":true}`, `{"position_seconds":NaN}`, `{"position_seconds":1e999}`,
			`{"position_seconds":-1}`, `{"position_seconds":60.001}`, `{"position_seconds":1,"label":12}`,
			`{"position_seconds":1,"user_id":1}`, `{"position_seconds":1} {}`, `{"position_seconds":1} trailing`,
			`{"position_seconds":1,"label":"` + strings.Repeat("章", 201) + `"}`,
			`{"position_seconds":1,"label":"` + strings.Repeat("a", 5000) + `"}`,
		} {
			if rr := e.request(ctx, "POST", path, "alice", body); rr.Code != 400 {
				t.Fatalf("invalid body %.100s = %d %s", body, rr.Code, rr.Body)
			}
		}
		for _, tc := range []struct {
			id   string
			user string
			code int
		}{{strconv.FormatInt(mid, 10), "", 401}, {strconv.FormatInt(mid, 10), "bob", 403}, {"999999", "alice", 404}, {"bad", "alice", 400}, {"0", "alice", 400}, {"-1", "alice", 400}, {strconv.FormatInt(e.media["pic.png"], 10), "alice", 400}} {
			for _, method := range []string{"GET", "POST", "DELETE"} {
				route := prefix + "/media/" + tc.id + "/bookmarks"
				if method == "DELETE" {
					route += "/1"
				}
				if rr := e.request(ctx, method, route, tc.user, `{"position_seconds":1}`); rr.Code != tc.code {
					t.Fatalf("%s %s as %q = %d want %d: %s", method, route, tc.user, rr.Code, tc.code, rr.Body)
				}
			}
		}
		for _, id := range []string{"0", "-1", "bad", "99999999999999999999999999"} {
			if rr := e.request(ctx, "DELETE", path+"/"+id, "alice", ""); rr.Code != 400 {
				t.Fatalf("malformed bookmark ID = %d", rr.Code)
			}
		}
	}
	items, err := e.store.ListBookmarks(ctx, mid, e.users["alice"])
	e.must(err)
	if len(items) != 0 {
		t.Fatal("invalid requests persisted bookmarks")
	}
	e.must(e.store.SoftDeleteMedia(ctx, mid))
	for _, method := range []string{"GET", "POST", "DELETE"} {
		path := fmt.Sprintf("/api/media/%d/bookmarks", mid)
		if method == "DELETE" {
			path += "/1"
		}
		if rr := e.request(ctx, method, path, "admin", `{"position_seconds":0}`); rr.Code != 404 {
			t.Fatalf("trashed %s = %d", method, rr.Code)
		}
	}
	// Unknown duration still supports bookmarks for audio.
	audioID, err := e.store.CreateMedia(ctx, &model.Media{SetID: m.SetID, RelPath: "book.mp3", FileName: "book.mp3", AbsPath: "/book.mp3", Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	e.must(err)
	path := fmt.Sprintf("/api/v1/media/%d/bookmarks", audioID)
	if rr := e.request(ctx, "POST", path, "alice", `{"position_seconds":123.25}`); rr.Code != 201 {
		t.Fatalf("audio with unknown duration = %d %s", rr.Code, rr.Body)
	}
}
