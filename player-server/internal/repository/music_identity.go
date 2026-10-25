package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/snonux/player/internal/model"
)

// MusicIdentityRepo stores ambiguous music move/retag conflicts for admin recovery.
type MusicIdentityRepo interface {
	ReplaceMusicIdentityConflicts(ctx context.Context, conflicts []model.MusicIdentityConflict) error
	ListMusicIdentityConflicts(ctx context.Context) ([]model.MusicIdentityConflict, error)
	GetMusicIdentityConflict(ctx context.Context, id int64) (*model.MusicIdentityConflict, error)
	ResolveMusicIdentityConflict(ctx context.Context, id int64, resolution string, at time.Time) error
	GetMediaByIDAny(ctx context.Context, id int64) (*model.Media, error)
}

// ReplaceMusicIdentityConflicts replaces unresolved conflicts with the latest scan results.
func (s *SQLite) ReplaceMusicIdentityConflicts(ctx context.Context, conflicts []model.MusicIdentityConflict) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM music_identity_conflicts WHERE resolved_at IS NULL`); err != nil {
		return fmt.Errorf("clear unresolved identity conflicts: %w", err)
	}
	return s.RecordMusicIdentityConflicts(ctx, conflicts)
}

// RecordMusicIdentityConflicts appends inventory conflicts without clearing
// unrelated unresolved choices. Repeated inventory observations are idempotent.
func (s *SQLite) RecordMusicIdentityConflicts(ctx context.Context, conflicts []model.MusicIdentityConflict) error {
	for _, c := range conflicts {
		ids := c.CandidateMediaIDs
		if ids == "" {
			raw, err := json.Marshal([]int64{})
			if err != nil {
				return err
			}
			ids = string(raw)
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO music_identity_conflicts (library_id, missing_media_id, candidate_media_ids, missing_path, reason, created_at)
			 SELECT ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (
			 SELECT 1 FROM music_identity_conflicts WHERE resolved_at IS NULL AND library_id = ? AND missing_media_id = ? AND candidate_media_ids = ?)`,
			c.LibraryID, c.MissingMediaID, ids, c.MissingPath, c.Reason, c.CreatedAt, c.LibraryID, c.MissingMediaID, ids,
		); err != nil {
			return fmt.Errorf("insert identity conflict: %w", err)
		}
	}
	return nil
}

// ListMusicIdentityConflicts returns unresolved conflicts, newest first.
func (s *SQLite) ListMusicIdentityConflicts(ctx context.Context) ([]model.MusicIdentityConflict, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, library_id, missing_media_id, candidate_media_ids, missing_path, reason, created_at, resolved_at, resolution
		 FROM music_identity_conflicts WHERE resolved_at IS NULL ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list identity conflicts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.MusicIdentityConflict
	for rows.Next() {
		var c model.MusicIdentityConflict
		var resolved sql.NullTime
		var resolution sql.NullString
		if err := rows.Scan(&c.ID, &c.LibraryID, &c.MissingMediaID, &c.CandidateMediaIDs, &c.MissingPath, &c.Reason, &c.CreatedAt, &resolved, &resolution); err != nil {
			return nil, err
		}
		if resolved.Valid {
			t := resolved.Time
			c.ResolvedAt = &t
		}
		if resolution.Valid {
			c.Resolution = resolution.String
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Release the sole SQLite connection before looking up candidate media.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		var ids []int64
		_ = json.Unmarshal([]byte(out[i].CandidateMediaIDs), &ids)
		for _, id := range ids {
			if m, err := s.GetMediaByID(ctx, id); err == nil && m != nil {
				out[i].CandidatePaths = append(out[i].CandidatePaths, m.RelPath)
			}
		}
	}
	return out, nil
}

// GetMusicIdentityConflict returns one conflict by id (resolved or not).
func (s *SQLite) GetMusicIdentityConflict(ctx context.Context, id int64) (*model.MusicIdentityConflict, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, library_id, missing_media_id, candidate_media_ids, missing_path, reason, created_at, resolved_at, resolution
		 FROM music_identity_conflicts WHERE id = ?`, id)
	var c model.MusicIdentityConflict
	var resolved sql.NullTime
	var resolution sql.NullString
	err := row.Scan(&c.ID, &c.LibraryID, &c.MissingMediaID, &c.CandidateMediaIDs, &c.MissingPath, &c.Reason, &c.CreatedAt, &resolved, &resolution)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if resolved.Valid {
		t := resolved.Time
		c.ResolvedAt = &t
	}
	if resolution.Valid {
		c.Resolution = resolution.String
	}
	return &c, nil
}

// ResolveMusicIdentityConflict marks a conflict resolved with a short resolution label.
func (s *SQLite) ResolveMusicIdentityConflict(ctx context.Context, id int64, resolution string, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE music_identity_conflicts SET resolved_at = ?, resolution = ? WHERE id = ? AND resolved_at IS NULL`,
		at, resolution, id)
	if err != nil {
		return fmt.Errorf("resolve identity conflict: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("identity conflict %d not found or already resolved", id)
	}
	return nil
}

// migrateMusicIdentityConflicts creates the A06a conflict table.
func migrateMusicIdentityConflicts(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS music_identity_conflicts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		library_id INTEGER NOT NULL DEFAULT 0,
		missing_media_id INTEGER NOT NULL,
		candidate_media_ids TEXT NOT NULL,
		missing_path TEXT NOT NULL DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		resolved_at DATETIME,
		resolution TEXT
	)`)
	if err != nil {
		return fmt.Errorf("create music_identity_conflicts: %w", err)
	}
	return nil
}
