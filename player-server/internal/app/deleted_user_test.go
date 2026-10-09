package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// as performs a request with exactly the given credentials (either may be
// empty); e.request would mint a fresh session for the user instead.
func (e *e2e) as(method, path, cookie, bearer, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: cookie})
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	e.server.ServeHTTP(rr, req)
	return rr
}

// signIn gives user a session cookie and an API token.
func (e *e2e) signIn(user string) (cookie, bearer string) {
	e.t.Helper()
	cookie, err := e.deps.SM.CreateSession(context.Background(), e.users[user])
	e.must(err)
	rr := e.as(http.MethodPost, "/api/v1/auth/tokens", cookie, "", `{"name":"phone"}`)
	var created struct {
		Token string `json:"token"`
	}
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &created) != nil || created.Token == "" {
		e.t.Fatalf("create token: %d %s", rr.Code, rr.Body.String())
	}
	return cookie, created.Token
}

// A deleted user must be signed out at once, with the cookie and with the
// API token, and must lose the sets that were granted to them.
func TestDeletedUser_IsSignedOutImmediately(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	aliceCookie, aliceToken := e.signIn("alice")
	bobCookie, bobToken := e.signIn("bob")

	for name, rr := range map[string]*httptest.ResponseRecorder{
		"alice cookie": e.as(http.MethodGet, "/api/v1/sets", aliceCookie, "", ""),
		"alice token":  e.as(http.MethodGet, "/api/v1/sets", "", aliceToken, ""),
	} {
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "set1") {
			t.Fatalf("before delete, %s: %d %s", name, rr.Code, rr.Body.String())
		}
	}

	path := "/api/v1/admin/users/" + strconv.FormatInt(e.users["alice"], 10)
	if rr := e.request(context.Background(), http.MethodDelete, path, "admin", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete user: %d %s", rr.Code, rr.Body.String())
	}

	for name, rr := range map[string]*httptest.ResponseRecorder{
		"alice cookie":            e.as(http.MethodGet, "/api/v1/sets", aliceCookie, "", ""),
		"alice token":             e.as(http.MethodGet, "/api/v1/sets", "", aliceToken, ""),
		"alice cookie on a file":  e.as(http.MethodGet, "/api/v1/media/"+strconv.FormatInt(e.media["movie.mp4"], 10)+"/stream", aliceCookie, "", ""),
		"alice token, new tokens": e.as(http.MethodPost, "/api/v1/auth/tokens", "", aliceToken, `{"name":"again"}`),
	} {
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("after delete, %s: %d %s; want 401", name, rr.Code, rr.Body.String())
		}
	}
	// Another user is not affected.
	for name, rr := range map[string]*httptest.ResponseRecorder{
		"bob cookie": e.as(http.MethodGet, "/api/v1/sets", bobCookie, "", ""),
		"bob token":  e.as(http.MethodGet, "/api/v1/sets", "", bobToken, ""),
	} {
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "set2") {
			t.Errorf("after delete, %s: %d %s; want 200 with set2", name, rr.Code, rr.Body.String())
		}
	}
}
