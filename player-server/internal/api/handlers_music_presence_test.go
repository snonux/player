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

func TestMusicPresenceHandlerAccountAndMalformedRequests(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clk := &clock.MockClock{T: time.Now().UTC()}
	browse := service.NewMediaService(store, clk, "", nil, nil)
	server := &Server{media: MediaServices{Presence: service.NewMusicPresenceService(store, browse, clk)}}
	for _, tc := range []struct {
		method, query, body string
		status              int
	}{
		{"GET", "", "", 409},
		{"GET", "?expected_user_id=2", "", 409},
		{"DELETE", "?expected_user_id=1", `{"client_id":"client-1234567890","sequence":1}`, 204},
		{"DELETE", "?expected_user_id=1", `{"client_id":"short","sequence":1}`, 400},
		{"PUT", "?expected_user_id=1", `{"client_id":"client-1234567890","sequence":1,"position_seconds":-1}`, 400},
		{"DELETE", "?expected_user_id=1", `{"client_id":"client-1234567890","sequence":2} {}`, 400},
		{"DELETE", "?expected_user_id=1", `{"client_id":"client-1234567890","sequence":2,"secret":"x"}`, 400},
	} {
		request := httptest.NewRequest(tc.method, "/api/music/presence"+tc.query, strings.NewReader(tc.body))
		request = request.WithContext(context.WithValue(request.Context(), sessionCtxKey, &model.Session{ID: "session", UserID: 1}))
		recorder := httptest.NewRecorder()
		server.handleMusicPresence(recorder, request)
		if recorder.Code != tc.status || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s got%d %s", tc.method, tc.query, recorder.Code, recorder.Body)
		}
	}
}
