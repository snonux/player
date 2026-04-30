package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/paul/kiss-media-player/internal/model"
)

// UpsertNote inserts or replaces a note for a user and media.
func (s *SQLite) UpsertNote(ctx context.Context, note *model.Note) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO media_notes (media_id, user_id, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(media_id, user_id) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		note.MediaID, note.UserID, note.Content, note.CreatedAt, note.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert note: %w", err)
	}
	return nil
}

// GetNote retrieves a note for a user and media.
func (s *SQLite) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, media_id, user_id, content, created_at, updated_at FROM media_notes WHERE media_id = ? AND user_id = ?`,
		mediaID, userID,
	)
	var n model.Note
	if err := row.Scan(&n.ID, &n.MediaID, &n.UserID, &n.Content, &n.CreatedAt, &n.UpdatedAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &n, nil
}

// DeleteNote removes a note for a user and media.
func (s *SQLite) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM media_notes WHERE media_id = ? AND user_id = ?`, mediaID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	return nil
}
