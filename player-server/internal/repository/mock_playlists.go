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

// UpdatePlaylistDetails is the missing-document default.
func (s *MockStore) UpdatePlaylistDetails(context.Context, int64, string, string, string, *string, *int64) (bool, error) {
	return false, nil
}

// ImportPlaylist is the missing-document default.
func (s *MockStore) ImportPlaylist(context.Context, int64, model.Playlist, bool) (bool, error) {
	return false, nil
}

// PlaylistSources is the empty default.
func (s *MockStore) PlaylistSources(context.Context) ([]model.Playlist, error) { return nil, nil }

// DetachPlaylistSource is the missing-document default.
func (s *MockStore) DetachPlaylistSource(context.Context, int64, string) (bool, error) {
	return false, nil
}

// AddPlaylistSongs is the missing-document default.
func (s *MockStore) AddPlaylistSongs(context.Context, int64, map[string][]model.PlaylistEntry, bool) (bool, error) {
	return false, nil
}
