package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"codeberg.org/snonux/player/internal/model"
)

// ImportPlaylist atomically creates an import, or refreshes a still-owned binding.
// Refresh preserves occurrence IDs when membership is unchanged. A transfer or
// detach racing a refresh prevents the stale filesystem authority from writing.
func (s *SQLite) ImportPlaylist(ctx context.Context, user int64, p model.Playlist, create bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	source := ""
	if p.Source != nil {
		encoded, err := json.Marshal(p.Source)
		if err != nil {
			return false, err
		}
		source = string(encoded)
	}
	if create {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlists(id,user_id,name,comment,source) VALUES(?,?,?,?,?)`, p.ID, user, p.Name, p.Comment, source); err != nil {
			return false, err
		}
	} else {
		var old string
		err := tx.QueryRowContext(ctx, `SELECT source FROM playlists WHERE id=? AND user_id=? AND source<>''`, p.ID, user).Scan(&old)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var binding model.PlaylistSource
		if err := json.Unmarshal([]byte(old), &binding); err != nil {
			return false, err
		}
		if p.Source == nil || binding.Path != p.Source.Path || binding.Base != p.Source.Base {
			return false, nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE playlists SET source=? WHERE id=?`, source, p.ID); err != nil {
			return false, err
		}
		// Source failures update status only, preserving the last known sequence.
		if p.Source.Status != "ready" {
			return true, tx.Commit()
		}
		rows, err := tx.QueryContext(ctx, `SELECT media_id FROM playlist_entries WHERE playlist_id=? ORDER BY position`, p.ID)
		if err != nil {
			return false, err
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return false, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return false, err
		}
		equal := len(ids) == len(p.Entries)
		if equal {
			for i, id := range ids {
				if id != p.Entries[i].MediaID {
					equal = false
					break
				}
			}
		}
		if equal {
			return true, tx.Commit()
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_entries WHERE playlist_id=?`, p.ID); err != nil {
			return false, err
		}
	}
	for i, e := range p.Entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_entries(id,playlist_id,media_id,position) VALUES(?,?,?,?)`, e.ID, p.ID, e.MediaID, i+1); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// PlaylistSources returns only internal bindings, never a viewer-facing listing.
func (s *SQLite) PlaylistSources(ctx context.Context) ([]model.Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,source FROM playlists WHERE source<>''`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []model.Playlist{}
	for rows.Next() {
		var p model.Playlist
		var source string
		if err := rows.Scan(&p.ID, &p.OwnerID, &source); err != nil {
			return nil, err
		}
		p.Source = new(model.PlaylistSource)
		if err := json.Unmarshal([]byte(source), p.Source); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DetachPlaylistSource retains membership while restoring manual editing.
func (s *SQLite) DetachPlaylistSource(ctx context.Context, user int64, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE playlists SET source='' WHERE id=? AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))`, id, user, user)
	return playlistAffected(result, err)
}
