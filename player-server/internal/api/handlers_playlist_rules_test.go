package api

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

func TestSmartPlaylistAPIValidationAndSavedRules(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	user, err := store.CreateUser(ctx, &model.User{Username: "rules", PasswordHash: "x", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewPlaylistService(store, service.NewMediaService(store, &clock.MockClock{T: time.Now()}, "", nil, nil))
	p, err := svc.Create(ctx, user, "Smart", "")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{media: MediaServices{Playlists: svc}, logger: slog.Default()}
	request := func(body string, caller int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "/api/v1/playlists/"+p.ID+"/rules", strings.NewReader(body))
		r.SetPathValue("playlist_id", p.ID)
		r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: caller}))
		w := httptest.NewRecorder()
		server.handlePlaylistRules(w, r)
		return w
	}
	for _, body := range []string{`{}`, `{"rules":{}}`, `{"rules":null} {}`, `{"rules":{"extra":1}}`, `{"rules":{"match":"all","conditions":[{"field":"year","operator":"gte","value":"1","extra":1}],"sort":"title","limit":10}}`} {
		if w := request(body, user); w.Code != 400 {
			t.Fatalf("invalid %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	valid := `{"rules":{"match":"all","conditions":[{"field":"rating","operator":"gte","value":"4"}],"sort":"title","descending":false,"limit":10}}`
	if w := request(valid, user+1); w.Code != 404 {
		t.Fatalf("foreign %d %s", w.Code, w.Body.String())
	}
	if w := request(valid, user); w.Code != 200 || !strings.Contains(w.Body.String(), `"rating"`) {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	stored, err := store.GetPlaylist(ctx, user, p.ID)
	if err != nil || stored.Rules == nil || stored.Rules.Limit != 10 {
		t.Fatalf("stored %+v %v", stored, err)
	}
	if w := request(`{"rules":null}`, user); w.Code != 200 || strings.Contains(w.Body.String(), `"rules":`) {
		t.Fatalf("disable %d %s", w.Code, w.Body.String())
	}
}
