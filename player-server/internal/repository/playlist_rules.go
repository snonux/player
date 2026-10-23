package repository

import (
	"context"
	"encoding/json"

	"codeberg.org/snonux/player/internal/model"
)

// PlaylistRulesRepo provides a narrow capability for dynamic selection writes.
type PlaylistRulesRepo interface {
	SetPlaylistRules(context.Context, int64, string, *model.PlaylistRules) (bool, error)
}

// SetPlaylistRules updates only editable non-filesystem-bound documents.
// Manual membership is retained so disabling rules is reversible.
func (s *SQLite) SetPlaylistRules(ctx context.Context, user int64, id string, rules *model.PlaylistRules) (bool, error) {
	encoded := ""
	if rules != nil {
		data, err := json.Marshal(rules)
		if err != nil {
			return false, err
		}
		encoded = string(data)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE playlists SET rules=? WHERE id=? AND source='' AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))`, encoded, id, user, user)
	return playlistAffected(result, err)
}

// SetPlaylistRules supplies the missing-document default for unrelated tests.
func (s *MockStore) SetPlaylistRules(context.Context, int64, string, *model.PlaylistRules) (bool, error) {
	return false, nil
}
