package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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
	ImportPlaylist(context.Context, int64, model.Playlist, bool) (bool, error)
	PlaylistSources(context.Context) ([]model.Playlist, error)
	DetachPlaylistSource(context.Context, int64, string) (bool, error)
	UpdatePlaylistDetails(context.Context, int64, string, string, string, *string, *int64) (bool, error)
	ListPlaylists(context.Context, int64) ([]model.Playlist, error)
	GetPlaylist(context.Context, int64, string) (*model.Playlist, error)
	CreatePlaylist(context.Context, int64, model.Playlist) error
	UpdatePlaylist(context.Context, int64, string, string, string) (bool, error)
	DeletePlaylist(context.Context, int64, string) (bool, error)
	AppendPlaylist(context.Context, int64, string, []model.PlaylistEntry) (bool, error)
	EditPlaylist(context.Context, int64, string, PlaylistChange) (bool, error)
}

// ListPlaylists returns owned/shared documents and all documents for administrators.
func (s *SQLite) ListPlaylists(ctx context.Context, user int64) ([]model.Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,p.comment,p.user_id,u.username,p.visibility,(p.user_id=? OR a.is_admin),a.is_admin FROM playlists p JOIN users u ON u.id=p.user_id JOIN users a ON a.id=? WHERE p.user_id=? OR p.visibility='shared' OR a.is_admin ORDER BY p.name COLLATE NOCASE,p.id`, user, user, user)
	if err != nil {
		return nil, fmt.Errorf("list playlists: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []model.Playlist{}
	for rows.Next() {
		var p model.Playlist
		if err := rows.Scan(&p.ID, &p.Name, &p.Comment, &p.OwnerID, &p.OwnerName, &p.Visibility, &p.CanEdit, &p.CanTransfer); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// GetPlaylist returns nil when the caller cannot read the playlist.
func (s *SQLite) GetPlaylist(ctx context.Context, user int64, id string) (*model.Playlist, error) {
	p := model.Playlist{Entries: []model.PlaylistEntry{}}
	var source string
	err := s.db.QueryRowContext(ctx, `SELECT p.id,p.name,p.comment,p.user_id,u.username,p.visibility,(p.user_id=? OR a.is_admin),a.is_admin,p.source FROM playlists p JOIN users u ON u.id=p.user_id JOIN users a ON a.id=? WHERE p.id=? AND (p.user_id=? OR p.visibility='shared' OR a.is_admin)`, user, user, id, user).Scan(&p.ID, &p.Name, &p.Comment, &p.OwnerID, &p.OwnerName, &p.Visibility, &p.CanEdit, &p.CanTransfer, &source)
	if source != "" {
		p.Source = new(model.PlaylistSource)
		if err := json.Unmarshal([]byte(source), p.Source); err != nil {
			return nil, err
		}
	}
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO playlists(id,user_id,name,comment,visibility) VALUES(?,?,?,?,COALESCE(NULLIF(?,''),'private'))`, p.ID, user, p.Name, p.Comment, p.Visibility)
	return err
}

// UpdatePlaylist changes metadata for an owner or administrator.
func (s *SQLite) UpdatePlaylist(ctx context.Context, user int64, id, name, comment string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE playlists SET name=?,comment=? WHERE id=? AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))`, name, comment, id, user, user)
	return playlistAffected(result, err)
}

// DeletePlaylist removes an authorized document and cascades its entries.
func (s *SQLite) DeletePlaylist(ctx context.Context, user int64, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM playlists WHERE id=? AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))`, id, user, user)
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
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM playlists WHERE source='' AND id=? AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))`, id, user, user).Scan(&owner); errors.Is(err, sql.ErrNoRows) {
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
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM playlists WHERE source='' AND id=? AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))`, id, user, user).Scan(&owner); errors.Is(err, sql.ErrNoRows) {
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

// UpdatePlaylistDetails atomically updates metadata, visibility and administrator ownership.
// Authorization is checked in the same statement as the update, including transfer.
func (s *SQLite) UpdatePlaylistDetails(ctx context.Context, user int64, id, name, comment string, visibility *string, owner *int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE playlists SET name=?,comment=?,visibility=COALESCE(?,visibility),user_id=COALESCE(?,user_id),source=CASE WHEN ? IS NULL OR ?=user_id THEN source ELSE '' END
 WHERE id=? AND (user_id=? OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))
 AND (? IS NULL OR EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin))
 AND (? IS NULL OR EXISTS(SELECT 1 FROM users WHERE id=?))`, name, comment, visibility, owner, owner, owner, id, user, user, owner, user, owner, owner)
	return playlistAffected(result, err)
}
