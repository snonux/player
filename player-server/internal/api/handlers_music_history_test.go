package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

func TestMusicHistoryContractAndAccountGuard(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	user, err := store.CreateUser(context.Background(), &model.User{Username: "history", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store, media: MediaServices{Browse: service.NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)}}
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/music/history", "", 200},
		{"GET", "/api/v1/music/history?before_id=1", "", 400},
		{"GET", "/api/music/history?before_ms=bad&before_id=1", "", 400},
		{"PUT", "/api/music/history", `{"recording":false,"retention_days":90}`, 200},
		{"PUT", "/api/music/history", `{"recording":false}`, 400},
		{"PUT", "/api/music/history", `{"recording":true,"retention_days":1}`, 400},
		{"DELETE", "/api/music/history?expected_user_id=999", "", 409},
		{"PUT", "/api/music/history?expected_user_id=999", `{"recording":true,"retention_days":0}`, 409},
		{"DELETE", "/api/music/history", "", 204},
	} {
		t.Run(test.method+test.path+test.body, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey, &model.Session{UserID: user}))
			response := httptest.NewRecorder()
			server.handleMusicHistory(response, req)
			if response.Code != test.status {
				t.Fatalf("status %d body %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response can be cached")
			}
		})
	}
	settings, err := store.MusicHistorySettings(context.Background(), user)
	if err != nil || settings.Recording || settings.RetentionDays != 90 {
		t.Fatalf("stale account confirmation mutated settings %+v %v", settings, err)
	}
}
