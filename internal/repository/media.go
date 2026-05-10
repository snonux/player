package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// CreateMedia inserts a new media and returns the generated ID.
func (s *SQLite) CreateMedia(ctx context.Context, media *model.Media) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO media (set_id, rel_path, file_name, abs_path, type, duration, codec, resolution, bitrate, file_size_bytes, width, height, exif_camera, exif_lens, exif_date, exif_iso, exif_f_number, exif_exposure, exif_focal_length, thumbnail_path, play_count, deleted_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		media.SetID, media.RelPath, media.FileName, media.AbsPath, string(media.Type),
		media.Duration, media.Codec, media.Resolution, media.Bitrate, media.FileSizeBytes,
		media.Width, media.Height, media.EXIFCamera, media.EXIFLens, media.EXIFDate,
		media.EXIFISO, media.EXIFFNumber, media.EXIFExposure, media.EXIFFocalLength,
		sqlNullString(media.ThumbnailPath), media.PlayCount, sqlNullTime(media.DeletedAt), media.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert media: %w", err)
	}
	return res.LastInsertId()
}

func scanMedia(row sqlScanner) (*model.Media, error) {
	var m model.Media
	var deleted sql.NullTime
	var mediaType string
	var thumbnail sql.NullString
	var codec sql.NullString
	var resolution sql.NullString
	var duration sql.NullFloat64
	var bitrate sql.NullInt64
	var fileSize sql.NullInt64
	var exifCamera sql.NullString
	var exifLens sql.NullString
	var exifDate sql.NullString
	var exifISO sql.NullString
	var exifFNumber sql.NullString
	var exifExposure sql.NullString
	var exifFocalLength sql.NullString
	var width sql.NullInt64
	var height sql.NullInt64
	err := row.Scan(
		&m.ID, &m.SetID, &m.RelPath, &m.FileName, &m.AbsPath, &mediaType,
		&duration, &codec, &resolution, &bitrate, &fileSize,
		&width, &height, &exifCamera, &exifLens, &exifDate, &exifISO,
		&exifFNumber, &exifExposure, &exifFocalLength,
		&thumbnail, &m.PlayCount, &deleted, &m.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Type = model.MediaType(mediaType)
	if deleted.Valid {
		m.DeletedAt = &deleted.Time
	}
	if thumbnail.Valid {
		m.ThumbnailPath = thumbnail.String
	}
	if codec.Valid {
		m.Codec = codec.String
	}
	if resolution.Valid {
		m.Resolution = resolution.String
	}
	if duration.Valid {
		m.Duration = duration.Float64
	}
	if bitrate.Valid {
		m.Bitrate = int(bitrate.Int64)
	}
	if fileSize.Valid {
		m.FileSizeBytes = fileSize.Int64
	}
	if width.Valid {
		m.Width = int(width.Int64)
	}
	if height.Valid {
		m.Height = int(height.Int64)
	}
	if exifCamera.Valid {
		m.EXIFCamera = exifCamera.String
	}
	if exifLens.Valid {
		m.EXIFLens = exifLens.String
	}
	if exifDate.Valid {
		m.EXIFDate = exifDate.String
	}
	if exifISO.Valid {
		m.EXIFISO = exifISO.String
	}
	if exifFNumber.Valid {
		m.EXIFFNumber = exifFNumber.String
	}
	if exifExposure.Valid {
		m.EXIFExposure = exifExposure.String
	}
	if exifFocalLength.Valid {
		m.EXIFFocalLength = exifFocalLength.String
	}
	return &m, nil
}

// GetMediaByID retrieves a media by ID.
func (s *SQLite) GetMediaByID(ctx context.Context, id int64) (*model.Media, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, set_id, rel_path, file_name, abs_path, type, duration, codec, resolution, bitrate, file_size_bytes, width, height, exif_camera, exif_lens, exif_date, exif_iso, exif_f_number, exif_exposure, exif_focal_length, thumbnail_path, play_count, deleted_at, created_at FROM media WHERE id = ? AND deleted_at IS NULL`, id)
	return scanMedia(row)
}

// UpdateMedia updates all mutable fields of a media record.
func (s *SQLite) UpdateMedia(ctx context.Context, media *model.Media) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media SET set_id = ?, rel_path = ?, file_name = ?, abs_path = ?, type = ?, duration = ?, codec = ?, resolution = ?, bitrate = ?, file_size_bytes = ?, width = ?, height = ?, exif_camera = ?, exif_lens = ?, exif_date = ?, exif_iso = ?, exif_f_number = ?, exif_exposure = ?, exif_focal_length = ?, thumbnail_path = ?, play_count = ?, deleted_at = ? WHERE id = ?`,
		media.SetID, media.RelPath, media.FileName, media.AbsPath, string(media.Type), media.Duration,
		media.Codec, media.Resolution, media.Bitrate, media.FileSizeBytes,
		media.Width, media.Height, media.EXIFCamera, media.EXIFLens, media.EXIFDate,
		media.EXIFISO, media.EXIFFNumber, media.EXIFExposure, media.EXIFFocalLength,
		sqlNullString(media.ThumbnailPath),
		media.PlayCount, sqlNullTime(media.DeletedAt), media.ID,
	)
	if err != nil {
		return fmt.Errorf("update media: %w", err)
	}
	return nil
}

// UpdateMediaThumbnail only patches the thumbnail_path field.
func (s *SQLite) UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media SET thumbnail_path = ? WHERE id = ?`,
		sqlNullString(thumbnailPath), id,
	)
	if err != nil {
		return fmt.Errorf("update media thumbnail: %w", err)
	}
	return nil
}

// SoftDeleteMedia sets deleted_at to NOW().
func (s *SQLite) SoftDeleteMedia(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("soft delete media: %w", err)
	}
	return nil
}

// RestoreMedia clears deleted_at.
func (s *SQLite) RestoreMedia(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET deleted_at = NULL WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("restore media: %w", err)
	}
	return nil
}

// HardDeleteMedia permanently deletes a media record.
func (s *SQLite) HardDeleteMedia(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("hard delete media: %w", err)
	}
	return nil
}

// ListMedia returns media matching the filter.
func (s *SQLite) ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error) {
	var args []any
	var conds []string
	var joins string
	query := `SELECT DISTINCT media.id, media.set_id, media.rel_path, media.file_name, media.abs_path, media.type, media.duration, media.codec, media.resolution, media.bitrate, media.file_size_bytes, media.width, media.height, media.exif_camera, media.exif_lens, media.exif_date, media.exif_iso, media.exif_f_number, media.exif_exposure, media.exif_focal_length, media.thumbnail_path, media.play_count, media.deleted_at, media.created_at FROM media`

	if filter.Search != "" {
		conds = append(conds, `(media.file_name LIKE ? ESCAPE '\' OR media.rel_path LIKE ? ESCAPE '\')`)
		term := filter.Search
		term = strings.ReplaceAll(term, "\\", "\\\\")
		term = strings.ReplaceAll(term, "%", "\\%")
		term = strings.ReplaceAll(term, "_", "\\_")
		like := "%" + term + "%"
		args = append(args, like, like)
	}
	if filter.Favorites {
		joins += ` INNER JOIN favorites f ON f.media_id = media.id AND f.user_id = ?`
		args = append(args, filter.UserID)
	}
	if len(filter.Tags) > 0 {
		joins += ` INNER JOIN media_tags mt ON mt.media_id = media.id INNER JOIN tags t ON t.id = mt.tag_id`
		conds = append(conds, `t.name IN (`+placeholders(len(filter.Tags))+`)`)
		for _, t := range filter.Tags {
			args = append(args, t)
		}
		// Require all tags by grouping and checking count
		// This is handled below via HAVING
	}

	if filter.SetID != nil {
		conds = append(conds, `media.set_id = ?`)
		args = append(args, *filter.SetID)
	}
	if len(filter.SetIDs) > 0 {
		conds = append(conds, "media.set_id IN ("+placeholders(len(filter.SetIDs))+")")
		for _, id := range filter.SetIDs {
			args = append(args, id)
		}
	}
	if len(filter.AllowedSetIDs) > 0 {
		conds = append(conds, "media.set_id IN ("+placeholders(len(filter.AllowedSetIDs))+")")
		for _, id := range filter.AllowedSetIDs {
			args = append(args, id)
		}
	}
	if filter.Type != nil {
		conds = append(conds, `media.type = ?`)
		args = append(args, string(*filter.Type))
	}
	if filter.MinDuration != nil {
		conds = append(conds, `media.duration >= ?`)
		args = append(args, *filter.MinDuration)
	}
	if filter.MaxDuration != nil {
		conds = append(conds, `media.duration <= ?`)
		args = append(args, *filter.MaxDuration)
	}
	conds = append(conds, `media.deleted_at IS NULL`)

	query += joins
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	if len(filter.Tags) > 0 {
		query += ` GROUP BY media.id HAVING COUNT(DISTINCT t.name) = ` + fmt.Sprintf("%d", len(filter.Tags))
	}
	switch filter.Sort {
	case "duration":
		query += " ORDER BY media.duration"
	case "play_count":
		query += " ORDER BY media.play_count DESC"
	case "date":
		query += " ORDER BY media.created_at DESC"
	case "random":
		query += " ORDER BY RANDOM()"
	default:
		query += " ORDER BY media.file_name"
	}

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()

	var media []model.Media
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		media = append(media, *m)
	}
	return media, rows.Err()
}

// ListDeletedMedia returns all soft-deleted media.
func (s *SQLite) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, set_id, rel_path, file_name, abs_path, type, duration, codec, resolution, bitrate, file_size_bytes, width, height, exif_camera, exif_lens, exif_date, exif_iso, exif_f_number, exif_exposure, exif_focal_length, thumbnail_path, play_count, deleted_at, created_at FROM media WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list deleted media: %w", err)
	}
	defer rows.Close()
	var media []model.Media
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		media = append(media, *m)
	}
	return media, rows.Err()
}

// IncrementPlayCount increments the play_count of a media by 1.
func (s *SQLite) IncrementPlayCount(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET play_count = play_count + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("increment play count: %w", err)
	}
	return nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
