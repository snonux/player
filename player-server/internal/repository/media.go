package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/snonux/player/internal/model"
)

// CreateMedia inserts a new media and returns the generated ID.
func (s *SQLite) CreateMedia(ctx context.Context, media *model.Media) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO media (set_id, rel_path, file_name, abs_path, type, duration, codec, resolution, bitrate, file_size_bytes, width, height, exif_camera, exif_lens, exif_date, exif_iso, exif_f_number, exif_exposure, exif_focal_length, thumbnail_path, play_count, deleted_at, created_at, music)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		media.SetID, media.RelPath, media.FileName, media.AbsPath, string(media.Type),
		media.Duration, media.Codec, media.Resolution, media.Bitrate, media.FileSizeBytes,
		media.Width, media.Height, media.EXIFCamera, media.EXIFLens, media.EXIFDate,
		media.EXIFISO, media.EXIFFNumber, media.EXIFExposure, media.EXIFFocalLength,
		sqlNullString(media.ThumbnailPath), media.PlayCount, sqlNullTime(media.DeletedAt), media.CreatedAt, musicJSON(media.Music),
	)
	if err != nil {
		return 0, fmt.Errorf("insert media: %w", err)
	}
	return res.LastInsertId()
}

// scanMedia scans the standard media column list. extra receives any columns
// a query selects after it (e.g. the saved position for in-progress media).
func scanMedia(row sqlScanner, extra ...any) (*model.Media, error) {
	var m model.Media
	var music, podcastMetadata sql.NullString
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
	dest := []any{
		&m.ID, &m.SetID, &m.RelPath, &m.FileName, &m.AbsPath, &mediaType,
		&duration, &codec, &resolution, &bitrate, &fileSize,
		&width, &height, &exifCamera, &exifLens, &exifDate, &exifISO,
		&exifFNumber, &exifExposure, &exifFocalLength,
		&thumbnail, &m.PlayCount, &deleted, &m.CreatedAt, &music, &podcastMetadata,
	}
	err := row.Scan(append(dest, extra...)...)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if music.Valid && music.String != "" {
		if err := json.Unmarshal([]byte(music.String), &m.Music); err != nil {
			return nil, fmt.Errorf("decode music tags: %w", err)
		}
	}
	if podcastMetadata.Valid && podcastMetadata.String != "" {
		var err error
		m.Podcast, err = decodePodcastMediaMetadata(podcastMetadata.String)
		if err != nil {
			return nil, err
		}
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
		`SELECT id, set_id, rel_path, file_name, abs_path, type, duration, codec, resolution, bitrate, file_size_bytes, width, height, exif_camera, exif_lens, exif_date, exif_iso, exif_f_number, exif_exposure, exif_focal_length, thumbnail_path, play_count, deleted_at, created_at, music, `+podcastMediaColumn+` FROM media WHERE id = ? AND deleted_at IS NULL`, id)
	return scanMedia(row)
}

// GetMediaByIDAny retrieves a media by ID including soft-deleted rows (identity recovery).
func (s *SQLite) GetMediaByIDAny(ctx context.Context, id int64) (*model.Media, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, set_id, rel_path, file_name, abs_path, type, duration, codec, resolution, bitrate, file_size_bytes, width, height, exif_camera, exif_lens, exif_date, exif_iso, exif_f_number, exif_exposure, exif_focal_length, thumbnail_path, play_count, deleted_at, created_at, music, `+podcastMediaColumn+` FROM media WHERE id = ?`, id)
	return scanMedia(row)
}

// UpdateMedia updates all mutable fields of a media record.
func (s *SQLite) UpdateMedia(ctx context.Context, media *model.Media) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media SET set_id = ?, rel_path = ?, file_name = ?, abs_path = ?, type = ?, duration = ?, codec = ?, resolution = ?, bitrate = ?, file_size_bytes = ?, width = ?, height = ?, exif_camera = ?, exif_lens = ?, exif_date = ?, exif_iso = ?, exif_f_number = ?, exif_exposure = ?, exif_focal_length = ?, thumbnail_path = ?, play_count = ?, deleted_at = ?, music = ? WHERE id = ?`,
		media.SetID, media.RelPath, media.FileName, media.AbsPath, string(media.Type), media.Duration,
		media.Codec, media.Resolution, media.Bitrate, media.FileSizeBytes,
		media.Width, media.Height, media.EXIFCamera, media.EXIFLens, media.EXIFDate,
		media.EXIFISO, media.EXIFFNumber, media.EXIFExposure, media.EXIFFocalLength,
		sqlNullString(media.ThumbnailPath),
		media.PlayCount, sqlNullTime(media.DeletedAt), musicJSON(media.Music), media.ID,
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
	_, err := s.db.ExecContext(ctx, `UPDATE media SET deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`, id)
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

// podcastMediaColumn projects public publisher metadata with one indexed lookup.
// Stored publication times are normalized to UTC at startup; JSON uses RFC3339.
const podcastMediaColumn = `(SELECT json_object('title',COALESCE(e.title,''),'feed_title',COALESCE(f.title,''),
 'description',COALESCE(e.description,''),'image_url',COALESCE(e.image_url,''),'feed_image_url',COALESCE(f.image_url,''),
 'published_at',e.published_at)
 FROM podcast_episodes e JOIN podcast_feeds f ON f.id=e.feed_id WHERE e.media_id=media.id ORDER BY e.id LIMIT 1)`

// mediaListColumns is the column list scanMedia expects, qualified with the
// media table because ListMedia may join favorites and tags.
const mediaListColumns = `media.id, media.set_id, media.rel_path, media.file_name, media.abs_path, media.type, media.duration, media.codec, media.resolution, media.bitrate, media.file_size_bytes, media.width, media.height, media.exif_camera, media.exif_lens, media.exif_date, media.exif_iso, media.exif_f_number, media.exif_exposure, media.exif_focal_length, media.thumbnail_path, media.play_count, media.deleted_at, media.created_at, media.music, ` + podcastMediaColumn

// mediaQuery collects SQL fragments with their bind arguments. JOIN and WHERE
// arguments are kept apart because JOIN placeholders precede WHERE
// placeholders in the final SQL, whatever order the filters are added in.
// Mixing them in one slice bound the favorites user ID to a search pattern.
type mediaQuery struct {
	joins    []string
	joinArgs []any
	conds    []string
	condArgs []any
}

func (q *mediaQuery) join(clause string, args ...any) {
	q.joins = append(q.joins, clause)
	q.joinArgs = append(q.joinArgs, args...)
}

func (q *mediaQuery) where(cond string, args ...any) {
	q.conds = append(q.conds, cond)
	q.condArgs = append(q.condArgs, args...)
}

// ListMedia returns media matching the filter.
func (s *SQLite) ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error) {
	q := mediaFilterQuery(filter)
	query := `SELECT DISTINCT ` + mediaListColumns + ` FROM media` + strings.Join(q.joins, "")
	if len(q.conds) > 0 {
		query += " WHERE " + strings.Join(q.conds, " AND ")
	}
	args := append(append([]any{}, q.joinArgs...), q.condArgs...)
	if len(filter.Tags) > 0 {
		// Require every requested tag, not just one of them.
		query += fmt.Sprintf(" GROUP BY media.id HAVING COUNT(DISTINCT t.name) = %d", len(filter.Tags))
	}
	if field, _ := mediaSort(filter.Sort); field == "last_played" {
		// Qualified last-played dates are personal.
		query += mediaPersonalOrderBy(filter.Sort)
		args = append(args, filter.UserID)
	} else {
		query += mediaOrderBy(filter.Sort)
	}
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}
	return s.queryMedia(ctx, query, args)
}

// mediaFilterQuery translates filter into joins and conditions.
func mediaFilterQuery(filter MediaFilter) *mediaQuery {
	q := &mediaQuery{}
	if filter.Search != "" {
		// escapeLike escapes LIKE wildcards so user input is treated as a literal substring.
		like := "%" + escapeLike(filter.Search) + "%"
		q.where(`(media.file_name LIKE ? ESCAPE '\' OR media.rel_path LIKE ? ESCAPE '\' OR json_extract(media.music, '$.title') LIKE ? ESCAPE '\' OR json_extract(media.music, '$.artist') LIKE ? ESCAPE '\' OR json_extract(media.music, '$.album') LIKE ? ESCAPE '\')`, like, like, like, like, like)
	}
	if filter.Favorites {
		q.join(` INNER JOIN favorites f ON f.media_id = media.id AND f.user_id = ?`, filter.UserID)
	}
	if len(filter.Tags) > 0 {
		q.join(` INNER JOIN media_tags mt ON mt.media_id = media.id INNER JOIN tags t ON t.id = mt.tag_id`)
		q.where(`t.name IN (`+placeholders(len(filter.Tags))+`)`, anySlice(filter.Tags)...)
	}
	if filter.SetID != nil {
		q.where(`media.set_id = ?`, *filter.SetID)
	}
	if len(filter.SetIDs) > 0 {
		q.where("media.set_id IN ("+placeholders(len(filter.SetIDs))+")", anySlice(filter.SetIDs)...)
	}
	if len(filter.AllowedSetIDs) > 0 {
		q.where("media.set_id IN ("+placeholders(len(filter.AllowedSetIDs))+")", anySlice(filter.AllowedSetIDs)...)
	}
	if filter.Type != nil {
		q.where(`media.type = ?`, string(*filter.Type))
	}
	if filter.MinDuration != nil {
		q.where(`media.duration >= ?`, *filter.MinDuration)
	}
	if filter.MaxDuration != nil {
		q.where(`media.duration <= ?`, *filter.MaxDuration)
	}
	if filter.MinFileSize != nil {
		q.where(`media.file_size_bytes >= ?`, *filter.MinFileSize)
	}
	if filter.MaxFileSize != nil {
		q.where(`media.file_size_bytes <= ?`, *filter.MaxFileSize)
	}
	if !filter.IncludeDeleted {
		q.where(`media.deleted_at IS NULL`)
	}
	return q
}

// mediaSort keeps legacy defaults while allowing explicit direction on every field.
func mediaSort(value string) (string, string) {
	direction := "ASC"
	if strings.HasSuffix(value, "_desc") {
		value = strings.TrimSuffix(value, "_desc")
		direction = "DESC"
	} else if strings.HasSuffix(value, "_asc") {
		value = strings.TrimSuffix(value, "_asc")
	} else if value == "date" || value == "play_count" || value == "last_played" {
		direction = "DESC"
	}
	return value, direction
}

const mediaSortTies = ", media.file_name COLLATE NOCASE ASC, media.rel_path ASC, media.id ASC"

func mediaPersonalOrderBy(value string) string {
	_, direction := mediaSort(value)
	personal := "(SELECT MAX(qualified_ms) FROM music_listening_sessions ls WHERE ls.media_id=media.id AND ls.user_id=? AND ls.qualified_ms IS NOT NULL)"
	// MAX is NULL for files with no qualified listening; always place them last.
	return " ORDER BY " + personal + " " + direction + " NULLS LAST" + mediaSortTies
}

func mediaOrderBy(value string) string {
	field, direction := mediaSort(value)
	var expression string
	switch field {
	case "play_count":
		expression = "media.play_count"
	case "duration":
		expression = "NULLIF(media.duration,0)"
	case "size":
		expression = "NULLIF(media.file_size_bytes,0)"
	case "date":
		expression = "media.created_at"
	case "date_taken":
		expression = "NULLIF(media.exif_date,'')"
	case "random":
		return " ORDER BY RANDOM()"
	default:
		expression = "media.file_name COLLATE NOCASE"
	}
	return " ORDER BY " + expression + " " + direction + " NULLS LAST" + mediaSortTies
}

// queryMedia runs a query selecting mediaListColumns and scans every row.
func (s *SQLite) queryMedia(ctx context.Context, query string, args []any) ([]model.Media, error) {
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

// anySlice converts typed values to database/sql bind arguments.
func anySlice[T any](values []T) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// ListDeletedMedia returns all soft-deleted media.
func (s *SQLite) ListDeletedMedia(ctx context.Context) ([]model.Media, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, set_id, rel_path, file_name, abs_path, type, duration, codec, resolution, bitrate, file_size_bytes, width, height, exif_camera, exif_lens, exif_date, exif_iso, exif_f_number, exif_exposure, exif_focal_length, thumbnail_path, play_count, deleted_at, created_at, music, `+podcastMediaColumn+` FROM media WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
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
	return incrementPlayCount(ctx, s.db, id)
}

func incrementPlayCount(ctx context.Context, db sqlExecer, id int64) error {
	_, err := db.ExecContext(ctx, `UPDATE media SET play_count = play_count + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("increment play count: %w", err)
	}
	return nil
}

// escapeLike escapes backslash, percent, and underscore in s so it can be
// used as a literal substring in a SQL LIKE ? ESCAPE '\' clause without
// allowing timing-based wildcard injection.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
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
