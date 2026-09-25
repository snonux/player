package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"codeberg.org/snonux/player/internal/service"
)

// unsubscribeStub records the unsubscribe call and returns a fixed error.
type unsubscribeStub struct {
	service.PodcastEpisodeService
	err    error
	feedID int64
}

func (s *unsubscribeStub) UnsubscribeFeed(_ context.Context, feedID, _ int64) error {
	s.feedID = feedID
	return s.err
}

// TestHandleUnsubscribePodcast maps path parsing and service errors to the
// documented status codes. Admin gating is covered by scenario S02.
func TestHandleUnsubscribePodcast(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		err    error
		status int
		called bool
	}{
		{"deleted", "7", nil, http.StatusNoContent, true},
		{"not a number", "abc", nil, http.StatusBadRequest, false},
		{"zero id", "0", nil, http.StatusBadRequest, false},
		{"unknown feed", "7", service.ErrNotFound, http.StatusNotFound, true},
		{"not owner", "7", service.ErrForbidden, http.StatusForbidden, true},
		{"storage failure", "7", errors.New("disk"), http.StatusInternalServerError, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &unsubscribeStub{err: tc.err}
			server := &Server{podcastSvc: stub, logger: slog.Default()}
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/podcasts/"+tc.id, nil)
			req.SetPathValue("id", tc.id)
			rec := httptest.NewRecorder()
			server.handleUnsubscribePodcast(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if called := stub.feedID == 7; called != tc.called {
				t.Fatalf("service called = %v, want %v", called, tc.called)
			}
		})
	}
}
