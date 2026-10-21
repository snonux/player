package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
)

// PlaylistChange describes one occurrence edit without replacing saved membership.
type PlaylistChange struct {
	RemoveID   string
	MoveID     string
	NeighborID string
	BeforeID   string
	Entries    []model.PlaylistEntry
}

// PlaylistRepo stores account-owned playlist documents and ordered occurrences.
type PlaylistRepo interface {
	ListPlaylists(context.Context, int64) ([]model.Playlist, error)
	GetPlaylist(context.Context, int64, string) (*model.Playlist, error)
	CreatePlaylist(context.Context, int64, model.Playlist) error
	UpdatePlaylist(context.Context, int64, string, string, string) (bool, error)
	DeletePlaylist(context.Context, int64, string) (bool, error)
	AppendPlaylist(context.Context, int64, string, []model.PlaylistEntry) (bool, error)
	EditPlaylist(context.Context, int64, string, PlaylistChange) (bool, error)
}

// ListPlaylists returns only the named owner's documents without entries.
func (s *SQLite) ListPlaylists(ctx context.Context, user int64) ([]model.Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,comment FROM playlists WHERE user_id=? ORDER BY name COLLATE NOCASE,id`, user)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []model.Playlist{}
	for rows.Next() {
		var p model.Playlist
		if err := rows.Scan(&p.ID, &p.Name, &p.Comment); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// GetPlaylist returns nil when the owner cannot resolve the playlist.
func (s *SQLite) GetPlaylist(ctx context.Context, user int64, id string) (*model.Playlist, error) {
	p := model.Playlist{Entries: []model.PlaylistEntry{}}
	err := s.db.QueryRowContext(ctx, `SELECT id,name,comment FROM playlists WHERE id=? AND user_id=?`, id, user).Scan(&p.ID, &p.Name, &p.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,media_id FROM playlist_entries WHERE playlist_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e model.PlaylistEntry
		if err := rows.Scan(&e.ID, &e.MediaID); err != nil {
			return nil, err
		}
		p.Entries = append(p.Entries, e)
	}
	return &p, rows.Err()
}

// CreatePlaylist inserts a new owner-scoped document.
func (s *SQLite) CreatePlaylist(ctx context.Context, user int64, p model.Playlist) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO playlists(id,user_id,name,comment) VALUES(?,?,?,?)`, p.ID, user, p.Name, p.Comment)
	return err
}

// UpdatePlaylist changes metadata only if the authenticated owner matches.
func (s *SQLite) UpdatePlaylist(ctx context.Context, user int64, id, name, comment string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE playlists SET name=?,comment=? WHERE id=? AND user_id=?`, name, comment, id, user)
	return playlistAffected(result, err)
}

// DeletePlaylist removes one owner's document and cascades its entries.
func (s *SQLite) DeletePlaylist(ctx context.Context, user int64, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM playlists WHERE id=? AND user_id=?`, id, user)
	return playlistAffected(result, err)
}

// AppendPlaylist atomically appends a complete ordered block. Owner lookup and
// maximum position happen in the same transaction, so concurrent adds serialize.
func (s *SQLite) AppendPlaylist(ctx context.Context, user int64, id string, entries []model.PlaylistEntry) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var owner int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM playlists WHERE id=? AND user_id=?`, id, user).Scan(&owner); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0) FROM playlist_entries WHERE playlist_id=?`, id).Scan(&position); err != nil {
		return false, err
	}
	for _, e := range entries {
		position++
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_entries(id,playlist_id,media_id,position) VALUES(?,?,?,?)`, e.ID, id, e.MediaID, position); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func playlistAffected(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// EditPlaylist applies one occurrence change to the latest complete membership.
// Hidden entries survive, and invalid occurrence identities roll back atomically.
func (s *SQLite) EditPlaylist(ctx context.Context, user int64, id string, change PlaylistChange) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var owner int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM playlists WHERE id=? AND user_id=?`, id, user).Scan(&owner); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM playlist_entries WHERE playlist_id=? ORDER BY position`, id)
	if err != nil {
		return false, err
	}
	ids := []string{}
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			_ = rows.Close()
			return false, err
		}
		ids = append(ids, entry)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return false, err
	}
	index := func(target string) int {
		for i, value := range ids {
			if value == target {
				return i
			}
		}
		return -1
	}
	if change.RemoveID != "" {
		at := index(change.RemoveID)
		if at < 0 {
			return false, nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_entries WHERE playlist_id=? AND id=?`, id, change.RemoveID); err != nil {
			return false, err
		}
		ids = append(ids[:at], ids[at+1:]...)
	}
	if change.MoveID != "" {
		a, b := index(change.MoveID), index(change.NeighborID)
		if a < 0 || b < 0 {
			return false, nil
		}
		ids[a], ids[b] = ids[b], ids[a]
	}
	at := index(change.BeforeID)
	if at < 0 {
		at = len(ids)
	}
	newIDs := make([]string, 0, len(change.Entries))
	for _, entry := range change.Entries {
		newIDs = append(newIDs, entry.ID)
	}
	ids = append(ids[:at], append(newIDs, ids[at:]...)...)
	// Move existing positions outside the positive range before assigning order,
	// preserving entry IDs and the unique playlist/position constraint.
	if _, err := tx.ExecContext(ctx, `UPDATE playlist_entries SET position=-position WHERE playlist_id=?`, id); err != nil {
		return false, err
	}
	for i, entry := range change.Entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_entries(id,playlist_id,media_id,position) VALUES(?,?,?,?)`, entry.ID, id, entry.MediaID, len(ids)+i+1); err != nil {
			return false, err
		}
	}
	for i, entry := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE playlist_entries SET position=? WHERE playlist_id=? AND id=?`, i+1, id, entry); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}
