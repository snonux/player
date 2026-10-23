package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// ownedRows names a column that references another table's id and whose
// rows have no meaning once the referenced row is gone.
type ownedRows struct {
	table  string
	column string
}

// userOwned are the rows that belong to a user. The schema declares all of
// them ON DELETE CASCADE; they are also listed here because cascades only
// run on a connection that enforces foreign keys, and for a while not every
// connection did (see connection.go). DeleteUser removes them explicitly and
// removeOrphanRows cleans up what earlier deletes left behind.
var userOwned = []ownedRows{
	{"music_entity_favorites", "user_id"},
	{"saved_queues", "user_id"},
	{"playlists", "user_id"},
	{"sessions", "user_id"},
	{"api_tokens", "user_id"},
	{"set_permissions", "user_id"},
	{"favorites", "user_id"},
	{"playback_progress", "user_id"},
	{"playback_resets", "user_id"},
	{"media_notes", "user_id"},
	{"media_bookmarks", "user_id"},
	{"shares", "created_by"},
	{"podcast_status", "user_id"},
	{"podcast_subscriptions", "user_id"},
	{"podcast_episode_state", "user_id"},
	{"podcast_catalog_creations", "user_id"},
}

// mediaOwned are the rows that belong to a media item.
var mediaOwned = []ownedRows{
	{"playlist_entries", "media_id"},
	{"favorites", "media_id"},
	{"playback_progress", "media_id"},
	{"playback_resets", "media_id"},
	{"media_notes", "media_id"},
	{"media_bookmarks", "media_id"},
	{"shares", "media_id"},
	{"media_tags", "media_id"},
	{"playback_accumulator", "media_id"},
}

// orphanedAccumulators deletes playback accumulators whose session is gone.
// A session id is either a sessions row or "api-token:<id>" for a Bearer
// token (see DeleteUser).
const orphanedAccumulators = `DELETE FROM playback_accumulator
WHERE session_id NOT IN (SELECT id FROM sessions)
  AND session_id NOT IN (SELECT 'api-token:' || id FROM api_tokens)`

// removeOrphanRows deletes rows whose user or media item no longer exists.
// With foreign keys enforced there are none; the pass exists for databases
// written while enforcement could silently switch off, where a deleted
// user's sessions, tokens and permissions stayed behind. It runs in one
// transaction, is idempotent, and logs only when it removed something.
func removeOrphanRows(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	removed, err := deleteOrphans(tx, userOwned, "users")
	if err != nil {
		return err
	}
	ofMedia, err := deleteOrphans(tx, mediaOwned, "media")
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM playlist_entries WHERE playlist_id NOT IN (SELECT id FROM playlists)`)
	if err != nil {
		return fmt.Errorf("playlist entry orphans: %w", err)
	}
	res, err := tx.Exec(orphanedAccumulators)
	if err != nil {
		return fmt.Errorf("playback_accumulator.session_id: %w", err)
	}
	ofSessions, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if total := removed + ofMedia + ofSessions; total > 0 {
		slog.Warn("repository removed orphaned rows",
			"of_deleted_users", removed, "of_deleted_media", ofMedia, "of_ended_sessions", ofSessions)
	}
	return nil
}

// deleteOrphans removes the rows of each listed column whose value is not an
// id of parent, and returns how many rows went.
func deleteOrphans(tx *sql.Tx, owned []ownedRows, parent string) (int64, error) {
	var total int64
	for _, o := range owned {
		// Table and column names come from the fixed lists above.
		res, err := tx.Exec(fmt.Sprintf(
			`DELETE FROM %s WHERE %s NOT IN (SELECT id FROM %s)`, o.table, o.column, parent))
		if err != nil {
			return 0, fmt.Errorf("%s.%s: %w", o.table, o.column, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// deleteUserRows removes everything a user owns, inside the caller's
// transaction, without relying on the schema's cascades.
func deleteUserRows(ctx context.Context, tx *sql.Tx, userID int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_entries WHERE playlist_id IN (SELECT id FROM playlists WHERE user_id=?)`, userID); err != nil {
		return fmt.Errorf("delete user playlist entries: %w", err)
	}
	for _, o := range userOwned {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, o.table, o.column), userID); err != nil {
			return fmt.Errorf("delete user rows in %s: %w", o.table, err)
		}
	}
	return nil
}
