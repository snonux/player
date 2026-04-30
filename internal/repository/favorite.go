package repository

import (
	"context"
	"fmt"

	"codeberg.org/snonux/play/internal/model"
)

// ToggleFavorite inserts or deletes a favorite row, returning whether it is now favorited.
func (s *SQLite) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM favorites WHERE user_id = ? AND media_id = ?`, userID, mediaID)
	var dummy int
	err := row.Scan(&dummy)
	if err != nil {
		// Insert new favorite
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO favorites (user_id, media_id) VALUES (?, ?)`, userID, mediaID)
		if err != nil {
			return false, fmt.Errorf("insert favorite: %w", err)
		}
		return true, nil
	}
	// Delete existing favorite
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM favorites WHERE user_id = ? AND media_id = ?`, userID, mediaID)
	if err != nil {
		return false, fmt.Errorf("delete favorite: %w", err)
	}
	return false, nil
}

// IsFavorite returns true if the user has favorited the media.
func (s *SQLite) IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM favorites WHERE user_id = ? AND media_id = ?`, userID, mediaID)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		return false, nil
	}
	return true, nil
}

// ListFavoritesByUser returns all favorites for a user.
func (s *SQLite) ListFavoritesByUser(ctx context.Context, userID int64) ([]model.Favorite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, media_id, created_at FROM favorites WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list favorites: %w", err)
	}
	defer rows.Close()
	var favs []model.Favorite
	for rows.Next() {
		var f model.Favorite
		if err := rows.Scan(&f.UserID, &f.MediaID, &f.CreatedAt); err != nil {
			return nil, err
		}
		favs = append(favs, f)
	}
	return favs, rows.Err()
}
