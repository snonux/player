package api

import (
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRadioAdminAndValidation(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	admin, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "x", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store}
	call := func(method, body, id string, user int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/radio", strings.NewReader(body))
		r.SetPathValue("station_id", id)
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: user}))
		response := httptest.NewRecorder()
		server.handleRadio(response, r)
		return response
	}
	valid := `{"name":"Example","stream_url":"https://example.com/live","homepage_url":"https://example.com","artwork_url":"","local_network":false}`
	if response := call(http.MethodPost, valid, "", viewer); response.Code != http.StatusForbidden {
		t.Fatalf("viewer create %d", response.Code)
	}
	if response := call(http.MethodPost, `{"name":"Unsafe","stream_url":"file:///etc/passwd"}`, "", admin); response.Code != http.StatusBadRequest {
		t.Fatalf("unsafe create %d", response.Code)
	}
	if response := call(http.MethodPost, valid, "", admin); response.Code != http.StatusCreated {
		t.Fatalf("create %d %s", response.Code, response.Body)
	}
	if response := call(http.MethodGet, "", "", viewer); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Example") {
		t.Fatalf("browse %d %s", response.Code, response.Body)
	}
	if response := call(http.MethodDelete, "", "1", viewer); response.Code != http.StatusForbidden {
		t.Fatalf("viewer delete %d", response.Code)
	}
	if response := call(http.MethodPut, strings.Replace(valid, "Example", "Edited", 1), "1", admin); response.Code != http.StatusOK {
		t.Fatalf("edit %d", response.Code)
	}
	if response := call(http.MethodDelete, "", "1", admin); response.Code != http.StatusNoContent {
		t.Fatalf("delete %d", response.Code)
	}
	if response := call(http.MethodPut, valid, "1", admin); response.Code != http.StatusNotFound {
		t.Fatalf("removed edit %d", response.Code)
	}
}
