package repository

import (
	"codeberg.org/snonux/player/internal/model"
	"context"
)

// ListPlaylists is the empty default for tests unrelated to playlists.
func (s *MockStore) ListPlaylists(context.Context, int64) ([]model.Playlist, error) {
	return []model.Playlist{}, nil
}

// GetPlaylist is the missing-document default.
func (s *MockStore) GetPlaylist(context.Context, int64, string) (*model.Playlist, error) {
	return nil, nil
}

// CreatePlaylist is the no-op metadata default.
func (s *MockStore) CreatePlaylist(context.Context, int64, model.Playlist) error { return nil }

// UpdatePlaylist is the missing-document default.
func (s *MockStore) UpdatePlaylist(context.Context, int64, string, string, string) (bool, error) {
	return false, nil
}

// DeletePlaylist is the missing-document default.
func (s *MockStore) DeletePlaylist(context.Context, int64, string) (bool, error) { return false, nil }

// AppendPlaylist is the missing-document default.
func (s *MockStore) AppendPlaylist(context.Context, int64, string, []model.PlaylistEntry) (bool, error) {
	return false, nil
}

// EditPlaylist is the missing-document default.
func (s *MockStore) EditPlaylist(context.Context, int64, string, PlaylistChange) (bool, error) {
	return false, nil
}
