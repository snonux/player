package repository

import (
	"github.com/snonux/player/internal/model"
	"context"
	"database/sql"
	"errors"
)

// AddPlaylistSongs appends all targets atomically. Duplicate decisions are made
// inside the write transaction against saved occurrences and earlier input songs.
func (s *SQLite) AddPlaylistSongs(ctx context.Context, user int64, targets map[string][]model.PlaylistEntry, skip bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	for id, entries := range targets {
		// Obtain the write lock before inspecting membership, including with multiple connections.
		result, err := tx.ExecContext(ctx, `UPDATE playlists SET name=name WHERE id=? AND source='' AND rules='' AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))`, id, user, user)
		if err != nil {
			return false, err
		}
		ok, err := playlistAffected(result, nil)
		if err != nil || !ok {
			return false, err
		}
		var position int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0) FROM playlist_entries WHERE playlist_id=?`, id).Scan(&position); err != nil {
			return false, err
		}
		for _, entry := range entries {
			if skip {
				var exists int
				err := tx.QueryRowContext(ctx, `SELECT 1 FROM playlist_entries WHERE playlist_id=? AND media_id=? LIMIT 1`, id, entry.MediaID).Scan(&exists)
				if err == nil {
					continue
				}
				if !errors.Is(err, sql.ErrNoRows) {
					return false, err
				}
			}
			position++
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_entries(id,playlist_id,media_id,position) VALUES(?,?,?,?)`, entry.ID, id, entry.MediaID, position); err != nil {
				return false, err
			}
		}
	}
	return true, tx.Commit()
}
