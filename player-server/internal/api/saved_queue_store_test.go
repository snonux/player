package api

import (
	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (m *mockPingStore) GetSavedQueue(ctx context.Context, userID int64) (*model.SavedQueue, error) {
	return m.store.GetSavedQueue(ctx, userID)
}
func (m *mockPingStore) PutSavedQueue(ctx context.Context, userID, revision int64, q model.SavedQueue) (*model.SavedQueue, bool, error) {
	return m.store.PutSavedQueue(ctx, userID, revision, q)
}

func TestSavedQueueRoutesPreconditionConflictBothPrefixes(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clk := &clock.MockClock{T: time.Now().UTC()}
	user, err := store.CreateUser(context.Background(), &model.User{Username: "queue", PasswordHash: "x", CreatedAt: clk.T})
	if err != nil {
		t.Fatal(err)
	}
	sm := auth.NewSessionManager(store, clk, time.Hour)
	srv := newTestServer(t, store, nil, sm, &internal.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cookie := addSessionCookie(t, store, sm, user)
	request := func(method, path, match, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if match != "" {
			r.Header.Set("If-Match", match)
		}
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, method := range []string{"GET", "PUT"} {
			if w := request(method, prefix+"/queue", "", "", false); w.Code != 401 {
				t.Fatalf("anonymous %s %s: %d", method, prefix, w.Code)
			}
		}
		if w := request("GET", prefix+"/queue", "", "", true); w.Code != 200 || w.Header().Get("ETag") != `"0"` {
			t.Fatalf("empty queue: %d %s", w.Code, w.Body)
		}
	}
	body := `{"mutation_id":"one","entries":[],"current_id":0,"position_seconds":0,"label":"Empty","repeat":"off","shuffle":false,"canonical":[],"up_next":[]}`
	if w := request("PUT", "/api/v1/queue", "", body, true); w.Code != 400 {
		t.Fatalf("missing condition: %d", w.Code)
	}
	if w := request("PUT", "/api/v1/queue", `"0"`, body, true); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	stale := strings.Replace(body, `"one"`, `"two"`, 1)
	if w := request("PUT", "/api/queue", `"0"`, stale, true); w.Code != 409 || w.Header().Get("ETag") != `"1"` {
		t.Fatalf("conflict: %d %s", w.Code, w.Body)
	}
	if w := request("GET", "/api/queue", "", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"mutation_id":"one"`) {
		t.Fatalf("legacy read: %d %s", w.Code, w.Body)
	}
}
