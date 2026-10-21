package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// TestToggleEpisodeCompleteUsesEpisodeRegisters checks that the legacy
// toggle checks access and then hands the flip, at server time, to the
// store's episode registers.
func TestToggleEpisodeCompleteUsesEpisodeRegisters(t *testing.T) {
	svc, store := setupPodcastService(t)
	store.PodcastRepo.GetEpisodeByIDFunc = func(_ context.Context, id int64) (*model.PodcastEpisode, error) {
		if id == 404 {
			return nil, nil
		}
		return &model.PodcastEpisode{ID: id, FeedID: 3}, nil
	}
	store.PodcastRepo.GetFeedByIDFunc = func(context.Context, int64) (*model.PodcastFeed, error) {
		return &model.PodcastFeed{ID: 3, SetID: 1}, nil
	}
	store.SetPermissionRepo.GetPermissionFunc = func(_ context.Context, _, userID int64) (*model.SetPermission, error) {
		if userID == 1 {
			return &model.SetPermission{Role: model.RoleViewer}, nil
		}
		return nil, nil
	}
	var calls []int64
	store.PodcastSyncRepo.ToggleEpisodeCompletionFunc = func(_ context.Context, userID, episodeID int64, now time.Time) (bool, error) {
		if !now.Equal(svc.clock.Now()) {
			t.Errorf("toggle at %v, want the server time", now)
		}
		calls = append(calls, episodeID)
		if episodeID == 5 {
			return false, fmt.Errorf("gone: %w", sql.ErrNoRows)
		}
		return true, nil
	}
	ctx := context.Background()
	if err := svc.ToggleEpisodeComplete(ctx, 4, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.ToggleEpisodeComplete(ctx, 4, 2); !errors.Is(err, ErrForbidden) {
		t.Fatalf("no permission: %v", err)
	}
	if err := svc.ToggleEpisodeComplete(ctx, 404, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing episode: %v", err)
	}
	if err := svc.ToggleEpisodeComplete(ctx, 5, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("episode removed meanwhile: %v", err)
	}
	if fmt.Sprint(calls) != "[4 5]" {
		t.Fatalf("store toggles: %v", calls)
	}
}
