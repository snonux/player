package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

// subscribedEpisodesStub records the §6.3 query and returns a fixed page.
type subscribedEpisodesStub struct {
	service.PodcastEpisodeService
	q      repository.PodcastEpisodeQuery
	views  []model.PodcastEpisodeView
	err    error
	called bool
}

func (s *subscribedEpisodesStub) ListSubscribedEpisodes(_ context.Context, _ int64, q repository.PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error) {
	s.called = true
	s.q = q
	return s.views, s.err
}

func TestHandleListSubscribedEpisodes(t *testing.T) {
	views := []model.PodcastEpisodeView{{
		ID: 9, FeedKey: "feed:a.example.com/rss", EpisodeKey: "guid:1",
		FeedTitle: "A", Title: "Ep", Completed: false,
	}}
	cases := []struct {
		name   string
		url    string
		err    error
		status int
		want   repository.PodcastEpisodeQuery
	}{
		{
			name:   "defaults",
			url:    "/api/v1/podcasts/episodes",
			status: http.StatusOK,
			want:   repository.PodcastEpisodeQuery{},
		},
		{
			name:   "filter feed and page",
			url:    "/api/v1/podcasts/episodes?feed_key=feed:a.example.com/rss&filter=completed&limit=25&offset=50",
			status: http.StatusOK,
			want: repository.PodcastEpisodeQuery{
				FeedKey: "feed:a.example.com/rss", Filter: "completed", Limit: 25, Offset: 50,
			},
		},
		{
			name:   "invalid filter",
			url:    "/api/v1/podcasts/episodes?filter=started",
			err:    service.ErrInvalidEpisodeView,
			status: http.StatusBadRequest,
			want:   repository.PodcastEpisodeQuery{Filter: "started"},
		},
		{
			name:   "store failure",
			url:    "/api/v1/podcasts/episodes",
			err:    errors.New("db"),
			status: http.StatusInternalServerError,
			want:   repository.PodcastEpisodeQuery{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &subscribedEpisodesStub{views: views, err: tc.err}
			server := &Server{podcastSvc: stub, logger: slog.Default()}
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			server.handleListSubscribedEpisodes(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if !stub.called {
				t.Fatal("service not called")
			}
			if stub.q != tc.want {
				t.Fatalf("query %+v, want %+v", stub.q, tc.want)
			}
			if tc.status != http.StatusOK {
				return
			}
			var got []model.PodcastEpisodeView
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(got) != 1 || got[0].FeedKey != views[0].FeedKey || got[0].EpisodeKey != views[0].EpisodeKey {
				t.Fatalf("unexpected body %s", rec.Body.String())
			}
		})
	}
}
