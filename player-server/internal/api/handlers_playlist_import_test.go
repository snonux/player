package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestPlaylistImportRoutes(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	clk := &clock.MockClock{T: time.Now().UTC()}
	user, err := store.CreateUser(ctx, &model.User{Username: "import", PasswordHash: "x", CreatedAt: clk.T})
	must(err)
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: clk.T})
	must(err)
	must(store.GrantPermission(ctx, &model.SetPermission{UserID: user, SetID: set, Role: model.RoleViewer}))
	root := t.TempDir()
	must(os.MkdirAll(filepath.Join(root, "Music"), 0755))
	must(os.WriteFile(filepath.Join(root, "Music", "empty.m3u8"), []byte("#PLAYLIST:Empty import\n"), 0644))
	sm := auth.NewSessionManager(store, clk, time.Hour)
	browse := service.NewMediaService(store, clk, root, nil, nil)
	srv := newTestServer(t, store, nil, sm, &internal.Config{}, browse, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	srv.media.Playlists = service.NewPlaylistService(store, browse).WithImportRoot(root)
	cookie := addSessionCookie(t, store, sm, user)
	for _, prefix := range []string{"/api", "/api/v1"} {
		request := func(method, url, body string, authorized bool) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, prefix+url, strings.NewReader(body))
			if authorized {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			return w
		}
		for _, route := range []string{"/playlists/import", "/playlists/missing/source"} {
			if w := request("POST", route, `{}`, false); w.Code != 401 {
				t.Fatalf("auth %d", w.Code)
			}
		}
		w := request("POST", "/playlists/import", `{"path":"Music/empty.m3u8","base":"folder","preview":true,"follow":true}`, true)
		if w.Code != 200 {
			t.Fatalf("preview %d %s", w.Code, w.Body)
		}
		var preview service.PlaylistImportPreview
		must(json.Unmarshal(w.Body.Bytes(), &preview))
		body, _ := json.Marshal(service.PlaylistImportRequest{Path: "Music/empty.m3u8", Base: "folder", Follow: true, Fingerprint: preview.Fingerprint})
		w = request("POST", "/playlists/import", string(body), true)
		if w.Code != 200 {
			t.Fatalf("import %d %s", w.Code, w.Body)
		}
		var result service.PlaylistImportPreview
		must(json.Unmarshal(w.Body.Bytes(), &result))
		id := result.Playlist.ID
		for _, method := range []string{"POST", "DELETE"} {
			w = request(method, "/playlists/"+id+"/source", ``, true)
			if w.Code != 200 {
				t.Fatalf("source %s %d %s", method, w.Code, w.Body)
			}
		}
		w = request("POST", "/playlists/import", `{"path":"/etc/passwd","base":"folder","preview":true}`, true)
		if w.Code != 400 || strings.Contains(w.Body.String(), root) {
			t.Fatalf("unsafe source %d %s", w.Code, w.Body)
		}
	}
}

func (m *mockPingStore) ImportPlaylist(ctx context.Context, user int64, p model.Playlist, create bool) (bool, error) {
	return m.store.ImportPlaylist(ctx, user, p, create)
}
func (m *mockPingStore) PlaylistSources(ctx context.Context) ([]model.Playlist, error) {
	return m.store.PlaylistSources(ctx)
}
func (m *mockPingStore) DetachPlaylistSource(ctx context.Context, user int64, id string) (bool, error) {
	return m.store.DetachPlaylistSource(ctx, user, id)
}
