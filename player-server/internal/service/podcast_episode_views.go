package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// Episode views of the web (docs/podcast-sync-contract.md §6.3). Task 0i3
// defines their routes; this is the query they serve: the caller's
// subscribed feeds (one, or all), filtered by completion and paged.

const (
	// DefaultPodcastEpisodeViewLimit is the page size of a view request
	// without a limit.
	DefaultPodcastEpisodeViewLimit = 100
	// MaxPodcastEpisodeViewLimit is the largest page a view may ask for.
	MaxPodcastEpisodeViewLimit = 500
)

// ErrInvalidEpisodeView rejects an unknown filter or a bad page (HTTP 400).
var ErrInvalidEpisodeView = &apiError{msg: "invalid episode view", status: http.StatusBadRequest}

// PodcastEpisodeViewer lists the episode views of one user.
type PodcastEpisodeViewer interface {
	ListSubscribedEpisodes(ctx context.Context, userID int64, q repository.PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error)
}

var _ PodcastEpisodeViewer = (*podcastService)(nil)

// ListSubscribedEpisodes returns one page of the episodes of the user's
// subscribed feeds with the user's episode state. An empty filter means
// uncompleted, a zero limit the default page. A server copy is reported
// only when the user may read the podcast set it lies in; subscribing
// itself grants no set permission (contract §7.3).
func (s *podcastService) ListSubscribedEpisodes(ctx context.Context, userID int64, q repository.PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error) {
	q, err := normalizeEpisodeQuery(q)
	if err != nil {
		return nil, err
	}
	views, err := s.store.ListSubscribedEpisodes(ctx, userID, q)
	if err != nil {
		return nil, fmt.Errorf("list subscribed episodes: %w", err)
	}
	readable := map[int64]bool{}
	for i := range views {
		if views[i].ServerCopy == nil {
			continue
		}
		ok, seen := readable[views[i].SetID]
		if !seen {
			if ok, err = s.canReadSet(ctx, views[i].SetID, userID); err != nil {
				return nil, err
			}
			readable[views[i].SetID] = ok
		}
		if !ok {
			views[i].ServerCopy = nil
		}
	}
	return views, nil
}

// canReadSet reports whether the user may read a set; only a store error
// is an error.
func (s *podcastService) canReadSet(ctx context.Context, setID, userID int64) (bool, error) {
	err := s.helper.checkSetPermission(ctx, setID, userID, "")
	if errors.Is(err, ErrForbidden) {
		return false, nil
	}
	return err == nil, err
}

// normalizeEpisodeQuery fills defaults and rejects an unknown filter, a
// negative offset and a limit outside 1 … MaxPodcastEpisodeViewLimit.
func normalizeEpisodeQuery(q repository.PodcastEpisodeQuery) (repository.PodcastEpisodeQuery, error) {
	switch q.Filter {
	case "":
		q.Filter = model.PodcastFilterUncompleted
	case model.PodcastFilterUncompleted, model.PodcastFilterCompleted, model.PodcastFilterAll:
	default:
		return q, ErrInvalidEpisodeView
	}
	if q.Limit == 0 {
		q.Limit = DefaultPodcastEpisodeViewLimit
	}
	if q.Limit < 0 || q.Limit > MaxPodcastEpisodeViewLimit || q.Offset < 0 {
		return q, ErrInvalidEpisodeView
	}
	return q, nil
}
