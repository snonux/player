package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

type subsonicBrowseStub struct {
	sets  []model.Set
	songs []model.Media
}

func (s subsonicBrowseStub) ListSets(context.Context, int64) ([]model.Set, error) {
	return s.sets, nil
}
func (s subsonicBrowseStub) ListMedia(context.Context, int64, service.MediaQueryFilter) ([]model.Media, error) {
	return s.songs, nil
}
func (s subsonicBrowseStub) GetLyrics(context.Context, int64, int64) (*service.Lyrics, error) {
	return nil, nil
}

func (s subsonicBrowseStub) GetAudioFormat(context.Context, int64, int64) (*service.AudioFormat, error) {
	return nil, nil
}
func (s subsonicBrowseStub) GetMediaDetail(context.Context, int64, int64) (*service.MediaDetail, error) {
	return nil, nil
}
func (s subsonicBrowseStub) StreamMedia(context.Context, int64, int64) (*service.FileResult, error) {
	return nil, nil
}
func (s subsonicBrowseStub) DownloadMedia(context.Context, int64, int64) (*service.FileResult, error) {
	return nil, nil
}
func (s subsonicBrowseStub) GetThumbnail(context.Context, int64, int64) (*service.FileResult, error) {
	return nil, nil
}
func (s subsonicBrowseStub) BrowseSet(context.Context, int64, int64, string) (*service.BrowseResult, error) {
	return nil, nil
}
func (s subsonicBrowseStub) GetSetCover(context.Context, int64, string, int64) (*service.FileResult, error) {
	return nil, nil
}

func TestSubsonicAuthBrowseSearch(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	song := model.Media{
		ID: 7, SetID: 1, Type: model.MediaTypeAudio, FileName: "a.flac", RelPath: "A/Album/a.flac",
		Duration: 90, CreatedAt: now,
		Music: &model.MusicMetadata{Title: "Wave", Artist: "Ada", Album: "Tides", AlbumArtist: "Ada"},
	}
	browse := subsonicBrowseStub{
		sets:  []model.Set{{ID: 1, Name: "Library"}},
		songs: []model.Media{song},
	}
	authSvc := &service.MockAuthService{
		CountUsersFunc: func(context.Context) (int, error) { return 1, nil },
		VerifyPasswordFunc: func(_ context.Context, username, password string) (*model.User, error) {
			if username == "viewer" && password == "good" {
				return &model.User{ID: 2, Username: "viewer"}, nil
			}
			return nil, service.ErrInvalidCredentials
		},
		AuthenticateBearerFunc: func(_ context.Context, plaintext string) (*model.Session, error) {
			if plaintext == "pt_good" {
				return &model.Session{ID: "api-token:9", UserID: 2}, nil
			}
			return nil, service.ErrInvalidCredentials
		},
		GetUserByIDFunc: func(_ context.Context, id int64) (*model.User, error) {
			if id == 2 {
				return &model.User{ID: 2, Username: "viewer"}, nil
			}
			return nil, nil
		},
	}
	cfg := &internal.Config{SubsonicEnabled: true, Port: 0}
	srv := newTestServer(t, nil, nil, nil, cfg, browse, nil, nil, nil, nil, nil, nil, nil, authSvc, nil)

	t.Run("disabled rejects", func(t *testing.T) {
		off := newTestServer(t, nil, nil, nil, &internal.Config{SubsonicEnabled: false}, browse, nil, nil, nil, nil, nil, nil, nil, authSvc, nil)
		body := subsonicJSON(t, off, "/rest/ping.view?u=viewer&p=good&v=1.16.1&c=Test&f=json")
		if status(body) != "failed" {
			t.Fatalf("expected failed when disabled: %v", body)
		}
	})

	t.Run("bad credentials", func(t *testing.T) {
		body := subsonicJSON(t, srv, "/rest/ping.view?u=viewer&p=bad&v=1.16.1&c=Test&f=json")
		if status(body) != "failed" || errorCode(body) != 40 {
			t.Fatalf("expected 40: %v", body)
		}
	})

	t.Run("token auth unsupported", func(t *testing.T) {
		body := subsonicJSON(t, srv, "/rest/ping.view?u=viewer&t=deadbeef&s=salt&v=1.16.1&c=Test&f=json")
		if status(body) != "failed" || errorCode(body) != 41 {
			t.Fatalf("expected 41: %v", body)
		}
	})

	t.Run("password ping and extensions", func(t *testing.T) {
		body := subsonicJSON(t, srv, "/rest/ping.view?u=viewer&p=good&v=1.16.1&c=Test&f=json")
		if status(body) != "ok" || body["openSubsonic"] != true {
			t.Fatalf("ping: %v", body)
		}
		ext := subsonicJSON(t, srv, "/rest/getOpenSubsonicExtensions.view?f=json&v=1.16.1&c=Test")
		if status(ext) != "ok" {
			t.Fatalf("extensions without auth should work: %v", ext)
		}
	})

	t.Run("apiKey browse search", func(t *testing.T) {
		folders := subsonicJSON(t, srv, "/rest/getMusicFolders.view?apiKey=pt_good&v=1.16.1&c=Test&f=json")
		if status(folders) != "ok" {
			t.Fatalf("folders: %v", folders)
		}
		mf := folders["musicFolders"].(map[string]any)
		list := mf["musicFolder"].([]any)
		if len(list) != 1 {
			t.Fatalf("expected 1 folder: %v", list)
		}
		artists := subsonicJSON(t, srv, "/rest/getArtists.view?apiKey=pt_good&v=1.16.1&c=Test&f=json")
		if status(artists) != "ok" {
			t.Fatalf("artists: %v", artists)
		}
		search := subsonicJSON(t, srv, "/rest/search3.view?apiKey=pt_good&v=1.16.1&c=Test&f=json&query=Wave")
		if status(search) != "ok" {
			t.Fatalf("search: %v", search)
		}
		result := search["searchResult3"].(map[string]any)
		songs := result["song"].([]any)
		if len(songs) != 1 {
			t.Fatalf("expected Wave song: %v", result)
		}
	})

	t.Run("config status surface", func(t *testing.T) {
		info := srv.subsonicStatusInfo()
		if info["enabled"] != true {
			t.Fatalf("status: %v", info)
		}
		off := (&Server{cfg: &internal.Config{SubsonicEnabled: false}}).subsonicStatusInfo()
		if off["enabled"] != false {
			t.Fatalf("disabled status: %v", off)
		}
	})
}

func subsonicJSON(t *testing.T, srv *Server, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d body %s", path, rec.Code, rec.Body.String())
	}
	var wrap map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wrap); err != nil {
		t.Fatalf("decode %s: %v (%s)", path, err, rec.Body.String())
	}
	body := wrap["subsonic-response"]
	if body == nil {
		t.Fatalf("missing subsonic-response in %s", rec.Body.String())
	}
	return body
}

func status(body map[string]any) string {
	s, _ := body["status"].(string)
	return s
}

func errorCode(body map[string]any) int {
	errObj, _ := body["error"].(map[string]any)
	code, _ := errObj["code"].(float64)
	return int(code)
}
