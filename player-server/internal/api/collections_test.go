package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

// Exercise the response boundary with nil collections from services. Repositories
// may naturally return nil for no rows, but clients must receive iterable arrays.
func TestEmptyCollectionResponses(t *testing.T) {
	media := &service.MockMediaService{
		BrowseSetFunc: func(context.Context, int64, int64, string) (*service.BrowseResult, error) {
			return &service.BrowseResult{}, nil
		},
		GetMediaDetailFunc: func(context.Context, int64, int64) (*service.MediaDetail, error) {
			return &service.MediaDetail{Media: &model.Media{ID: 1}}, nil
		},
	}
	admin := &service.MockAdminService{ListPermissionsFunc: func(context.Context) (*service.PermissionsMatrix, error) { return &service.PermissionsMatrix{}, nil }}
	server := &Server{
		media:      MediaServices{Browse: media, Tag: media, Share: media, Progress: &service.MockProgressService{}},
		adminSvc:   admin,
		podcastSvc: nilPodcasts{},
		logger:     slog.Default(),
	}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		fields  []string
	}{
		{"sets", server.handleListSets, nil},
		{"media", server.handleListMedia, nil},
		{"tags", server.handleListTags, nil},
		{"trash", server.handleListTrash, nil},
		{"users", server.handleListUsers, nil},
		{"shares", server.handleListShares, nil},
		{"my shares", server.handleMyShares, nil},
		{"in progress", server.handleInProgress, nil},
		{"podcasts", server.handleListPodcasts, nil},
		{"episodes", server.handleListEpisodes, nil},
		{"browse", server.handleBrowseSet, []string{"folders", "media"}},
		{"detail", server.handleGetMedia, []string{"tags"}},
		{"permissions", server.handleListPermissions, []string{"sets", "users", "permissions"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/media/1", nil)
			request.SetPathValue("id", "1")
			response := httptest.NewRecorder()
			tc.handler(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if len(tc.fields) == 0 {
				if response.Body.String() != "[]\n" {
					t.Fatalf("expected [], got %s", response.Body.String())
				}
				return
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil {
				t.Fatal(err)
			}
			for _, field := range tc.fields {
				if string(object[field]) != "[]" {
					t.Errorf("%s: expected [], got %s", field, object[field])
				}
			}
		})
	}
}

// nilPodcasts returns nil lists; unused methods panic via the nil embedded
// interface so an unexpected call fails the test loudly.
type nilPodcasts struct{ service.PodcastEpisodeService }

func (nilPodcasts) ListFeeds(context.Context, int64) ([]model.PodcastFeed, error) { return nil, nil }

func (nilPodcasts) ListEpisodes(context.Context, int64, int64, int, int) ([]model.PodcastEpisodeWithStatus, error) {
	return nil, nil
}
