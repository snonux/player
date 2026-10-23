package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

func TestShareEditStrictFields(t *testing.T) {
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"description":"plain <b>opaque</b>"}`, 200},
		{`{"expires_at":"2030-01-02T00:00:00Z"}`, 200},
		{`{"description":""}`, 200}, {`{}`, 400}, {`null`, 400},
		{`{"description":null}`, 400}, {`{"expires_at":null}`, 400},
		{`{"max_uses":5}`, 400}, {`{"media_ids":[1]}`, 400},
		{`{"description":5}`, 400}, {`{"expires_at":"bad"}`, 400},
		{`{"description":"a"} {}`, 400},
	} {
		t.Run(test.body, func(t *testing.T) {
			called := false
			ms := &service.MockMediaService{UpdateShareFunc: func(_ context.Context, token string, _ int64, description *string, expiresAt *time.Time) (*model.Share, error) {
				called = true
				if token != "same-token" {
					t.Fatalf("wrong token: %s", token)
				}
				desc := ""
				if description != nil {
					desc = *description
				}
				return &model.Share{Token: token, Description: desc}, nil
			}}
			srv := &Server{media: MediaServices{Share: ms}}
			request := httptest.NewRequest(http.MethodPatch, "/api/shares/same-token", strings.NewReader(test.body))
			request.SetPathValue("token", "same-token")
			response := httptest.NewRecorder()
			srv.handleUpdateShare(response, request)
			if response.Code != test.status || called != (test.status == 200) {
				t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
			}
			// Cleared descriptions must stay in JSON so My Shares can drop stale text.
			if test.body == `{"description":""}` && !strings.Contains(response.Body.String(), `"description":""`) {
				t.Fatalf("empty description omitted: %s", response.Body.String())
			}
		})
	}
}

// UpdateShare keeps the health-check test store aligned with Store.
func (m *mockPingStore) UpdateShare(ctx context.Context, token string, description *string, expiresAt *time.Time) (bool, error) {
	return m.store.UpdateShare(ctx, token, description, expiresAt)
}
