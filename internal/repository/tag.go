package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/paul/kiss-media-player/internal/model"
)

// CreateTag inserts a tag and returns the generated ID.
func (s *SQLite) CreateTag(ctx context.Context, name string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
	if err != nil {
		return 0, fmt.Errorf("insert tag: %w", err)
	}
	return res.LastInsertId()
}

func scanTag(row sqlScanner) (*model.Tag, error) {
	var t model.Tag
	if err := row.Scan(&t.ID, &t.Name); err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTagByID retrieves a tag by ID.
func (s *SQLite) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name FROM tags WHERE id = ?`, id)
	return scanTag(row)
}

// GetTagByName retrieves a tag by name.
func (s *SQLite) GetTagByName(ctx context.Context, name string) (*model.Tag, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name FROM tags WHERE name = ?`, name)
	return scanTag(row)
}

// ListTags returns all tags ordered by name.
func (s *SQLite) ListTags(ctx context.Context) ([]model.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM tags ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	var tags []model.Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		tags = append(tags, *t)
	}
	return tags, rows.Err()
}

// DeleteTag removes a tag.
func (s *SQLite) DeleteTag(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

// AssignTag links a tag to a media item.
func (s *SQLite) AssignTag(ctx context.Context, mediaID, tagID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO media_tags (media_id, tag_id) VALUES (?, ?)`, mediaID, tagID)
	if err != nil {
		return fmt.Errorf("assign tag: %w", err)
	}
	return nil
}

// RemoveTag removes the link between a tag and a media item.
func (s *SQLite) RemoveTag(ctx context.Context, mediaID, tagID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media_tags WHERE media_id = ? AND tag_id = ?`, mediaID, tagID)
	if err != nil {
		return fmt.Errorf("remove tag: %w", err)
	}
	return nil
}

// ListTagsByMedia returns tags assigned to a media item.
func (s *SQLite) ListTagsByMedia(ctx context.Context, mediaID int64) ([]model.Tag, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name FROM tags t INNER JOIN media_tags mt ON mt.tag_id = t.id WHERE mt.media_id = ? ORDER BY t.name`,
		mediaID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tags by media: %w", err)
	}
	defer rows.Close()
	var tags []model.Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		tags = append(tags, *t)
	}
	return tags, rows.Err()
}

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
