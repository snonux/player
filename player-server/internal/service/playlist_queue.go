package service

import (
	"context"

	"github.com/snonux/player/internal/model"
)

// CreateFromQueue atomically saves a private snapshot without changing playback.
func (s *PlaylistService) CreateFromQueue(ctx context.Context, user int64, name, comment string, visibility *string, mediaIDs []int64) (*PlaylistView, error) {
	if visibility != nil && *visibility != "private" {
		return nil, &apiError{msg: "Queue copies must be private. Sharing can be changed after saving.", status: 400}
	}
	name, err := validatePlaylist(name, comment)
	if err != nil {
		return nil, err
	}
	if len(mediaIDs) < 1 || len(mediaIDs) > 10000 {
		return nil, &apiError{msg: "Save between 1 and 10000 queued songs.", status: 400}
	}
	visible, err := s.visibleSongs(ctx, user)
	if err != nil {
		return nil, err
	}
	id, err := playlistID("pl")
	if err != nil {
		return nil, err
	}
	p := model.Playlist{ID: id, Name: name, Comment: comment, Visibility: "private"}
	for _, mediaID := range mediaIDs {
		if _, ok := visible[mediaID]; !ok {
			return nil, &apiError{msg: "Some queued songs are unavailable. Remove them from the queue and open Save as playlist again.", status: 409}
		}
		entry, err := playlistID("pe")
		if err != nil {
			return nil, err
		}
		p.Entries = append(p.Entries, model.PlaylistEntry{ID: entry, MediaID: mediaID})
	}
	ok, err := s.repo.ImportPlaylist(ctx, user, p, true)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return s.Get(ctx, user, id)
}
