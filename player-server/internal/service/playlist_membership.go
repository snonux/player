package service

import (
	"codeberg.org/snonux/player/internal/model"
	"context"
)

// Membership returns caller-readable lists with caller-visible song identities.
func (s *PlaylistService) Membership(ctx context.Context, user int64) ([]PlaylistView, error) {
	docs, err := s.List(ctx, user)
	if err != nil {
		return nil, err
	}
	result := make([]PlaylistView, 0, len(docs))
	for _, doc := range docs {
		p, err := s.Get(ctx, user, doc.ID)
		if err != nil {
			return nil, err
		}
		p.Songs = []model.Media{}
		result = append(result, *p)
	}
	return result, nil
}

// AddToPlaylists validates every target and song before a single atomic append.
func (s *PlaylistService) AddToPlaylists(ctx context.Context, user int64, ids []string, media []int64, policy string) error {
	if len(ids) == 0 || len(ids) > 100 || len(media) == 0 || len(media) > 1000 || (policy != "add" && policy != "skip") {
		return &apiError{msg: "Select 1–100 playlists, 1–1000 songs and add or skip duplicates.", status: 400}
	}
	visible, err := s.visibleSongs(ctx, user)
	if err != nil {
		return err
	}
	for _, id := range media {
		if _, ok := visible[id]; !ok {
			return ErrNotFound
		}
	}
	targets := make(map[string][]model.PlaylistEntry, len(ids))
	for _, id := range ids {
		if _, ok := targets[id]; ok {
			continue
		}
		p, err := s.repo.GetPlaylist(ctx, user, id)
		if err != nil {
			return err
		}
		if p == nil {
			return ErrNotFound
		}
		if !p.CanEdit {
			return ErrForbidden
		}
		if p.Source != nil {
			return &apiError{msg: "Stop following the source before editing songs.", status: 409}
		}
		entries := make([]model.PlaylistEntry, 0, len(media))
		for _, mediaID := range media {
			entry, err := playlistID("pe")
			if err != nil {
				return err
			}
			entries = append(entries, model.PlaylistEntry{ID: entry, MediaID: mediaID})
		}
		targets[id] = entries
	}
	ok, err := s.repo.AddPlaylistSongs(ctx, user, targets, policy == "skip")
	if err != nil {
		return err
	}
	if !ok {
		return &apiError{msg: "A playlist changed or is no longer editable. Refresh before retrying.", status: 409}
	}
	return nil
}
