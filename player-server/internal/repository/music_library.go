package repository

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/snonux/player/internal/model"
)

// MusicLibraryRepo manages independent music library roots.
type MusicLibraryRepo interface {
	CreateMusicLibrary(ctx context.Context, lib *model.MusicLibrary) (int64, error)
	GetMusicLibrary(ctx context.Context, id int64) (*model.MusicLibrary, error)
	ListMusicLibraries(ctx context.Context) ([]model.MusicLibrary, error)
	UpdateMusicLibrary(ctx context.Context, lib *model.MusicLibrary) error
	DeleteMusicLibrary(ctx context.Context, id int64) error
	MusicLibraryStats(ctx context.Context, libraryID int64, allowedSetIDs []int64) (*model.MusicLibraryStats, error)
	EnsureDefaultMusicLibrary(ctx context.Context, mediaRoot string, now time.Time) (*model.MusicLibrary, error)
	ListSetIDsByLibrary(ctx context.Context, libraryID int64) ([]int64, error)
}

// CreateMusicLibrary inserts a library root and returns its id.
func (s *SQLite) CreateMusicLibrary(ctx context.Context, lib *model.MusicLibrary) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO music_libraries (name, root_path, created_at) VALUES (?, ?, ?)`,
		lib.Name, lib.RootPath, lib.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("insert music library: %w", err)
	}
	return res.LastInsertId()
}

func scanMusicLibrary(row sqlScanner) (*model.MusicLibrary, error) {
	var lib model.MusicLibrary
	err := row.Scan(&lib.ID, &lib.Name, &lib.RootPath, &lib.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lib, nil
}

// GetMusicLibrary returns one library by id.
func (s *SQLite) GetMusicLibrary(ctx context.Context, id int64) (*model.MusicLibrary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, root_path, created_at FROM music_libraries WHERE id = ?`, id)
	return scanMusicLibrary(row)
}

// ListMusicLibraries returns all libraries ordered by name.
func (s *SQLite) ListMusicLibraries(ctx context.Context) ([]model.MusicLibrary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, root_path, created_at FROM music_libraries ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list music libraries: %w", err)
	}
	defer rows.Close()
	var out []model.MusicLibrary
	for rows.Next() {
		lib, err := scanMusicLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *lib)
	}
	return out, rows.Err()
}

// UpdateMusicLibrary renames a library (root_path is immutable after create).
func (s *SQLite) UpdateMusicLibrary(ctx context.Context, lib *model.MusicLibrary) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE music_libraries SET name = ? WHERE id = ?`, lib.Name, lib.ID)
	if err != nil {
		return fmt.Errorf("update music library: %w", err)
	}
	return nil
}

// DeleteMusicLibrary removes a library row. Callers must ensure no sets remain.
func (s *SQLite) DeleteMusicLibrary(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM music_libraries WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete music library: %w", err)
	}
	return nil
}

// ListSetIDsByLibrary returns set ids owned by a library.
func (s *SQLite) ListSetIDsByLibrary(ctx context.Context, libraryID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sets WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list sets by library: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MusicLibraryStats aggregates audio under a library. When allowedSetIDs is
// non-nil, only those sets contribute (caller ACL). Nil means all sets.
func (s *SQLite) MusicLibraryStats(ctx context.Context, libraryID int64, allowedSetIDs []int64) (*model.MusicLibraryStats, error) {
	stats := &model.MusicLibraryStats{}
	setQuery := `SELECT COUNT(*) FROM sets WHERE library_id = ? AND is_podcast = 0`
	args := []any{libraryID}
	if allowedSetIDs != nil {
		if len(allowedSetIDs) == 0 {
			return stats, nil
		}
		ph, setArgs := intPlaceholders(allowedSetIDs)
		setQuery += ` AND id IN (` + ph + `)`
		args = append(args, setArgs...)
	}
	if err := s.db.QueryRowContext(ctx, setQuery, args...).Scan(&stats.SetCount); err != nil {
		return nil, fmt.Errorf("library set count: %w", err)
	}

	mediaQuery := `
SELECT COUNT(*), COALESCE(SUM(file_size_bytes), 0), COALESCE(SUM(duration), 0)
FROM media m
JOIN sets st ON st.id = m.set_id
WHERE st.library_id = ? AND st.is_podcast = 0 AND m.type = 'audio' AND m.deleted_at IS NULL`
	mediaArgs := []any{libraryID}
	if allowedSetIDs != nil {
		ph, setArgs := intPlaceholders(allowedSetIDs)
		mediaQuery += ` AND m.set_id IN (` + ph + `)`
		mediaArgs = append(mediaArgs, setArgs...)
	}
	if err := s.db.QueryRowContext(ctx, mediaQuery, mediaArgs...).Scan(
		&stats.SongCount, &stats.TotalBytes, &stats.TotalDuration); err != nil {
		return nil, fmt.Errorf("library media stats: %w", err)
	}
	return stats, nil
}

// EnsureDefaultMusicLibrary creates or repairs the library for MEDIA_ROOT and
// attaches sets that still lack a library_id.
func (s *SQLite) EnsureDefaultMusicLibrary(ctx context.Context, mediaRoot string, now time.Time) (*model.MusicLibrary, error) {
	abs, err := filepath.Abs(mediaRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve media root: %w", err)
	}
	abs = filepath.Clean(abs)

	libs, err := s.ListMusicLibraries(ctx)
	if err != nil {
		return nil, err
	}
	var def *model.MusicLibrary
	for i := range libs {
		if libs[i].RootPath == abs || libs[i].RootPath == "" || libs[i].RootPath == mediaRoot {
			def = &libs[i]
			break
		}
	}
	if def == nil && len(libs) == 1 && strings.TrimSpace(libs[0].RootPath) == "" {
		def = &libs[0]
	}
	if def == nil {
		def = &model.MusicLibrary{Name: "Library", RootPath: abs, CreatedAt: now}
		id, err := s.CreateMusicLibrary(ctx, def)
		if err != nil {
			return nil, err
		}
		def.ID = id
	} else if def.RootPath != abs {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE music_libraries SET root_path = ? WHERE id = ?`, abs, def.ID); err != nil {
			return nil, fmt.Errorf("repair default library path: %w", err)
		}
		def.RootPath = abs
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sets SET library_id = ? WHERE library_id IS NULL OR library_id = 0`, def.ID); err != nil {
		return nil, fmt.Errorf("attach sets to default library: %w", err)
	}
	return def, nil
}

func intPlaceholders(ids []int64) (string, []any) {
	parts := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		parts[i] = "?"
		args[i] = id
	}
	return strings.Join(parts, ","), args
}
