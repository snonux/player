package service

import (
	"context"
	"errors"
	"testing"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func TestListSubscribedEpisodesValidatesQuery(t *testing.T) {
	cases := []struct {
		name string
		in   repository.PodcastEpisodeQuery
		want repository.PodcastEpisodeQuery
		err  error
	}{
		{"defaults", repository.PodcastEpisodeQuery{}, repository.PodcastEpisodeQuery{Filter: "uncompleted", Limit: DefaultPodcastEpisodeViewLimit}, nil},
		{"all, explicit page", repository.PodcastEpisodeQuery{FeedKey: "feed:a/", Filter: "all", Limit: 5, Offset: 10},
			repository.PodcastEpisodeQuery{FeedKey: "feed:a/", Filter: "all", Limit: 5, Offset: 10}, nil},
		{"unknown filter", repository.PodcastEpisodeQuery{Filter: "started"}, repository.PodcastEpisodeQuery{}, ErrInvalidEpisodeView},
		{"limit too large", repository.PodcastEpisodeQuery{Limit: MaxPodcastEpisodeViewLimit + 1}, repository.PodcastEpisodeQuery{}, ErrInvalidEpisodeView},
		{"negative limit", repository.PodcastEpisodeQuery{Limit: -1}, repository.PodcastEpisodeQuery{}, ErrInvalidEpisodeView},
		{"negative offset", repository.PodcastEpisodeQuery{Offset: -1}, repository.PodcastEpisodeQuery{}, ErrInvalidEpisodeView},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := setupPodcastService(t)
			var got repository.PodcastEpisodeQuery
			store.PodcastSyncRepo.ListSubscribedEpisodesFunc = func(_ context.Context, _ int64, q repository.PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error) {
				got = q
				return nil, nil
			}
			_, err := svc.ListSubscribedEpisodes(context.Background(), 1, tc.in)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && got != tc.want {
				t.Fatalf("store query = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestListSubscribedEpisodesHidesUnreadableServerCopies(t *testing.T) {
	svc, store := setupPodcastService(t)
	store.PodcastSyncRepo.ListSubscribedEpisodesFunc = func(context.Context, int64, repository.PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error) {
		return []model.PodcastEpisodeView{
			{Title: "readable", SetID: 1, ServerCopy: &model.PodcastServerCopy{MediaID: 10}},
			{Title: "hidden", SetID: 2, ServerCopy: &model.PodcastServerCopy{MediaID: 20}},
			{Title: "hidden too", SetID: 2, ServerCopy: &model.PodcastServerCopy{MediaID: 21}},
			{Title: "no copy", SetID: 2},
		}, nil
	}
	lookups := 0
	store.SetPermissionRepo.GetPermissionFunc = func(_ context.Context, setID, userID int64) (*model.SetPermission, error) {
		lookups++
		if setID == 1 {
			return &model.SetPermission{SetID: 1, UserID: userID, Role: model.RoleViewer}, nil
		}
		return nil, nil
	}
	views, err := svc.ListSubscribedEpisodes(context.Background(), 7, repository.PodcastEpisodeQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if views[0].ServerCopy == nil || views[1].ServerCopy != nil || views[2].ServerCopy != nil || views[3].ServerCopy != nil {
		t.Fatalf("server copies: %+v", views)
	}
	if lookups != 2 {
		t.Fatalf("permission looked up %d times, want once per set", lookups)
	}
	// A store error while checking is not taken for "no permission".
	boom := errors.New("boom")
	store.SetPermissionRepo.GetPermissionFunc = func(context.Context, int64, int64) (*model.SetPermission, error) { return nil, boom }
	if _, err := svc.ListSubscribedEpisodes(context.Background(), 7, repository.PodcastEpisodeQuery{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	store.PodcastSyncRepo.ListSubscribedEpisodesFunc = func(context.Context, int64, repository.PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error) {
		return nil, boom
	}
	if _, err := svc.ListSubscribedEpisodes(context.Background(), 7, repository.PodcastEpisodeQuery{}); !errors.Is(err, boom) {
		t.Fatalf("store err = %v", err)
	}
}
