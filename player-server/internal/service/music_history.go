package service

import (
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"context"
	"fmt"
)

// MusicHistoryResult includes the next cursor only when another page may exist.
type MusicHistoryResult struct {
	Settings     model.MusicHistorySettings `json:"settings"`
	Events       []model.MusicHistoryEvent  `json:"events"`
	NextBeforeID int64                      `json:"next_before_id,omitempty"`
	NextBeforeMS int64                      `json:"next_before_ms,omitempty"`
}

// MusicHistory decorates authorized events using fresh caller-visible metadata.
func MusicHistory(ctx context.Context, repo repository.MusicHistoryRepo, source CatalogSource, user, beforeMS, beforeID int64) (MusicHistoryResult, error) {
	result := MusicHistoryResult{Events: []model.MusicHistoryEvent{}}
	settings, err := repo.MusicHistorySettings(ctx, user)
	if err != nil {
		return result, err
	}
	result.Settings = settings
	albums, err := MusicAlbums(ctx, source, user)
	if err != nil {
		return result, err
	}
	songs := map[int64]model.Media{}
	for _, album := range albums {
		for _, song := range album.Songs {
			songs[song.ID] = song
		}
	}
	events, err := repo.ListMusicHistory(ctx, user, beforeMS, beforeID, 50)
	if err != nil {
		return result, err
	}
	for _, event := range events {
		song, ok := songs[event.MediaID]
		if !ok {
			continue
		}
		event.Title = song.FileName
		if song.Music != nil {
			if song.Music.Title != "" {
				event.Title = song.Music.Title
			}
			event.Artist = song.Music.Artist
		}
		result.Events = append(result.Events, event)
	}
	if len(events) == 50 {
		last := events[len(events)-1]
		result.NextBeforeID = last.ID
		result.NextBeforeMS = last.PlayedAt.UnixMilli()
	}
	return result, nil
}

// ConfigureMusicHistory validates the small, documented retention choice set.
func ConfigureMusicHistory(ctx context.Context, repo repository.MusicHistoryRepo, user int64, settings model.MusicHistorySettings) error {
	switch settings.RetentionDays {
	case 0, 30, 90, 365:
	default:
		return &apiError{msg: "Choose 30, 90, 365 days or forever.", status: 400}
	}
	if err := repo.SetMusicHistorySettings(ctx, user, settings); err != nil {
		return fmt.Errorf("save listening history settings: %w", err)
	}
	return nil
}
