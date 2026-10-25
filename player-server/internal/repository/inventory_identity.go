package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/snonux/player/internal/model"
)

// MergeInventoryAudio adopts a pending inventory destination atomically, only
// while it has no user state. False leaves both identities intact for a choice.
// The initial write acquires SQLite's writer lock before testing references, so
// a concurrent progress/favorite/playlist write cannot race a cascading delete.
func (s *SQLite) MergeInventoryAudio(ctx context.Context, keepID int64, candidate model.Media) (bool, error) {
	if keepID == candidate.ID {
		return false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin inventory identity: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE media SET id=id WHERE id=? AND set_id=? AND rel_path=? AND deleted_at IS NULL AND play_count=0 AND json_extract(music,'$.inventory_pending')=1`, candidate.ID, candidate.SetID, candidate.RelPath)
	if err != nil {
		return false, fmt.Errorf("lock inventory destination: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	unused, err := inventoryDestinationUnused(ctx, tx, candidate.ID)
	if err != nil || !unused {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM media WHERE id=?`, candidate.ID); err != nil {
		return false, fmt.Errorf("drop inventory destination: %w", err)
	}
	if candidate.Music != nil {
		copy := *candidate.Music
		copy.InventoryPending = false
		candidate.Music = &copy
	}
	result, err = tx.ExecContext(ctx, `UPDATE media SET set_id=?,rel_path=?,file_name=?,abs_path=?,type=?,duration=?,codec=?,resolution=?,bitrate=?,file_size_bytes=?,width=?,height=?,thumbnail_path=COALESCE(?,thumbnail_path),music=? WHERE id=? AND deleted_at IS NULL`,
		candidate.SetID, candidate.RelPath, candidate.FileName, candidate.AbsPath, string(candidate.Type), candidate.Duration, candidate.Codec, candidate.Resolution, candidate.Bitrate, candidate.FileSizeBytes, candidate.Width, candidate.Height, sqlNullString(candidate.ThumbnailPath), musicJSON(candidate.Music), keepID)
	if err != nil {
		return false, fmt.Errorf("adopt inventory source: %w", err)
	}
	n, err = result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit inventory identity: %w", err)
	}
	return true, nil
}

func inventoryDestinationUnused(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	for _, owned := range mediaOwned {
		var used bool
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s=?)", owned.table, owned.column)
		if err := tx.QueryRowContext(ctx, query, id).Scan(&used); err != nil {
			return false, fmt.Errorf("inspect inventory state: %w", err)
		}
		if used {
			return false, nil
		}
	}
	for _, query := range []string{
		`SELECT EXISTS(SELECT 1 FROM saved_queues,json_each(snapshot,'$.entries') WHERE json_extract(json_each.value,'$.media_id')=?)`,
		`SELECT EXISTS(SELECT 1 FROM shares,json_each(media_ids) WHERE json_each.value=?)`,
		`SELECT EXISTS(SELECT 1 FROM podcast_episodes WHERE media_id=?)`,
	} {
		var used bool
		if err := tx.QueryRowContext(ctx, query, id).Scan(&used); err != nil {
			return false, fmt.Errorf("inspect inventory references: %w", err)
		}
		if used {
			return false, nil
		}
	}
	return true, nil
}
