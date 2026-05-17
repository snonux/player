package repository

import (
	"context"
	"database/sql"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// CreateSet inserts a new set and returns the generated ID.
func (s *SQLite) CreateSet(ctx context.Context, set *model.Set) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sets (name, root_path, cover_thumbnail_path, is_podcast, created_at) VALUES (?, ?, ?, ?, ?)`,
		set.Name, set.RootPath, sqlNullString(set.CoverThumbnailPath), boolToInt(set.IsPodcast), set.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert set: %w", err)
	}
	return res.LastInsertId()
}

func scanSet(row sqlScanner) (*model.Set, error) {
	var st model.Set
	var cover sql.NullString
	var isPodcast int
	err := row.Scan(&st.ID, &st.Name, &st.RootPath, &cover, &isPodcast, &st.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.CoverThumbnailPath = cover.String
	st.IsPodcast = isPodcast != 0
	return &st, nil
}

// GetSetByID retrieves a set by ID.
func (s *SQLite) GetSetByID(ctx context.Context, id int64) (*model.Set, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, root_path, cover_thumbnail_path, is_podcast, created_at FROM sets WHERE id = ?`, id)
	return scanSet(row)
}

// ListSets returns all sets ordered by name.
func (s *SQLite) ListSets(ctx context.Context) ([]model.Set, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, root_path, cover_thumbnail_path, is_podcast, created_at FROM sets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list sets: %w", err)
	}
	defer rows.Close()
	var sets []model.Set
	for rows.Next() {
		st, err := scanSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, *st)
	}
	return sets, rows.Err()
}

// UpdateSet modifies a set's fields.
func (s *SQLite) UpdateSet(ctx context.Context, set *model.Set) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sets SET name = ?, root_path = ?, cover_thumbnail_path = ?, is_podcast = ? WHERE id = ?`,
		set.Name, set.RootPath, sqlNullString(set.CoverThumbnailPath), boolToInt(set.IsPodcast), set.ID,
	)
	if err != nil {
		return fmt.Errorf("update set: %w", err)
	}
	return nil
}

// DeleteSet removes a set by ID.
func (s *SQLite) DeleteSet(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete set: %w", err)
	}
	return nil
}
