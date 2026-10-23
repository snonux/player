package repository

import (
	"context"
	"fmt"
)

// MusicFavoriteRepo stores entity hearts independently of file favorites.
type MusicFavoriteRepo interface {
	ListMusicFavorites(context.Context, int64, string) (map[string]bool, error)
	SetMusicFavorite(context.Context, int64, string, string, bool) error
}

// ListMusicFavorites returns the account's saved keys; callers apply current ACLs.
func (s *SQLite) ListMusicFavorites(ctx context.Context, user int64, kind string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entity_key FROM music_entity_favorites WHERE user_id=? AND kind=?`, user, kind)
	if err != nil {
		return nil, fmt.Errorf("list music favorites: %w", err)
	}
	defer rows.Close()
	keys := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("read music favorite: %w", err)
		}
		keys[key] = true
	}
	return keys, rows.Err()
}

// SetMusicFavorite is idempotent so retries cannot invert a saved heart.
func (s *SQLite) SetMusicFavorite(ctx context.Context, user int64, kind, key string, favorite bool) error {
	query := `DELETE FROM music_entity_favorites WHERE user_id=? AND kind=? AND entity_key=?`
	if favorite {
		query = `INSERT INTO music_entity_favorites(user_id,kind,entity_key) VALUES(?,?,?) ON CONFLICT DO NOTHING`
	}
	if _, err := s.db.ExecContext(ctx, query, user, kind, key); err != nil {
		return fmt.Errorf("save music favorite: %w", err)
	}
	return nil
}

// MockMusicFavoriteRepo provides no-op defaults for existing service tests.
type MockMusicFavoriteRepo struct {
	ListMusicFavoritesFunc func(context.Context, int64, string) (map[string]bool, error)
	SetMusicFavoriteFunc   func(context.Context, int64, string, string, bool) error
}

// ListMusicFavorites calls the configured fake, or returns no favorites.
func (m MockMusicFavoriteRepo) ListMusicFavorites(ctx context.Context, u int64, k string) (map[string]bool, error) {
	if m.ListMusicFavoritesFunc != nil {
		return m.ListMusicFavoritesFunc(ctx, u, k)
	}
	return map[string]bool{}, nil
}

// SetMusicFavorite calls the configured fake, or succeeds.
func (m MockMusicFavoriteRepo) SetMusicFavorite(ctx context.Context, u int64, k, id string, v bool) error {
	if m.SetMusicFavoriteFunc != nil {
		return m.SetMusicFavoriteFunc(ctx, u, k, id, v)
	}
	return nil
}
