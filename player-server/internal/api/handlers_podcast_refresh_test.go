package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

type refreshStub struct {
	service.PodcastEpisodeService
	err  error
	user int64
	key  string
}

func (s *refreshStub) RefreshSubscribedFeed(_ context.Context, user int64, key string) (*service.PodcastRefreshResult, error) {
	s.user = user
	s.key = key
	return &service.PodcastRefreshResult{FeedKey: key, Status: "refreshed"}, s.err
}
func TestPodcastRefreshHandler(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{nil, 200}, {service.ErrPodcastRefreshInvalid, 400}, {service.ErrPodcastRefreshMissing, 404}, {service.ErrPodcastRefreshBusy, 429}, {service.ErrPodcastRefreshFailed, 502}, {errors.New("db"), 500}} {
		stub := &refreshStub{err: tc.err}
		srv := &Server{podcastSvc: stub}
		req := httptest.NewRequest("POST", "/api/podcasts/refresh", strings.NewReader(`{"feed_key":"feed:example.com/rss"}`))
		req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey, &model.Session{UserID: 42}))
		rec := httptest.NewRecorder()
		srv.handlePodcastRefresh(rec, req)
		if rec.Code != tc.code || stub.user != 42 || stub.key != "feed:example.com/rss" {
			t.Fatalf("status=%d user=%d key=%s", rec.Code, stub.user, stub.key)
		}
		if tc.code == 429 && rec.Header().Get("Retry-After") == "" {
			t.Fatal("missing Retry-After")
		}
	}
}
func TestPodcastRefreshRouteAliasesRequireSession(t *testing.T) {
	srv, store, sm, admin, cleanup := setupPodcastE2E(t)
	defer cleanup()
	cookie := addSessionCookie(t, store, sm, admin)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, signed := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodPost, prefix+"/podcasts/refresh", strings.NewReader(`{"feed_key":"broken"}`))
			want := 401
			if signed {
				req.AddCookie(cookie)
				want = 400
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Fatalf("%s signed=%v status=%d body=%s", prefix, signed, rec.Code, rec.Body.String())
			}
		}
	}
}
