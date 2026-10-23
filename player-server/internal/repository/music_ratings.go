package repository

import (
	"context"
	"fmt"
	"strconv"
)

// MusicRatingRepo stores personal stars independently of hearts.
type MusicRatingRepo interface {
	ListMusicRatings(context.Context, int64, string) (map[string]int, error)
	SetMusicRating(context.Context, int64, string, string, int) error
}

// ListMusicRatings returns saved values; callers apply current permissions.
func (s *SQLite) ListMusicRatings(ctx context.Context, user int64, kind string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entity_key,rating FROM music_ratings WHERE user_id=? AND kind=?`, user, kind)
	if err != nil {
		return nil, fmt.Errorf("list music ratings: %w", err)
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var key string
		var rating int
		if err := rows.Scan(&key, &rating); err != nil {
			return nil, fmt.Errorf("read music rating: %w", err)
		}
		result[key] = rating
	}
	return result, rows.Err()
}

// SetMusicRating sets an idempotent value; zero removes a rating.
func (s *SQLite) SetMusicRating(ctx context.Context, user int64, kind, key string, rating int) error {
	if rating < 0 || rating > 5 {
		return fmt.Errorf("rating must be between zero and five")
	}
	var mediaID any
	if kind == "song" {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != key {
			return fmt.Errorf("invalid song ID")
		}
		mediaID = id
	}
	query := `DELETE FROM music_ratings WHERE user_id=? AND kind=? AND entity_key=?`
	args := []any{user, kind, key}
	if rating > 0 {
		query = `INSERT INTO music_ratings(user_id,kind,entity_key,media_id,rating) VALUES(?,?,?,?,?) ON CONFLICT(user_id,kind,entity_key) DO UPDATE SET rating=excluded.rating`
		args = append(args, mediaID, rating)
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("save music rating: %w", err)
	}
	return nil
}

// MockMusicRatingRepo supplies optional persistence for server fixtures.
type MockMusicRatingRepo struct {
	ListMusicRatingsFunc func(context.Context, int64, string) (map[string]int, error)
	SetMusicRatingFunc   func(context.Context, int64, string, string, int) error
}

// ListMusicRatings calls the fake or returns no stars.
func (m MockMusicRatingRepo) ListMusicRatings(ctx context.Context, u int64, k string) (map[string]int, error) {
	if m.ListMusicRatingsFunc != nil {
		return m.ListMusicRatingsFunc(ctx, u, k)
	}
	return map[string]int{}, nil
}

// SetMusicRating calls the fake or succeeds.
func (m MockMusicRatingRepo) SetMusicRating(ctx context.Context, u int64, k, key string, v int) error {
	if m.SetMusicRatingFunc != nil {
		return m.SetMusicRatingFunc(ctx, u, k, key, v)
	}
	return nil
}
