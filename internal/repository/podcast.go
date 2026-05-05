package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// ------------------------------------------------------------------
// Podcast Feeds
// ------------------------------------------------------------------

// CreateFeed stores a new podcast feed and returns its database ID.
func (s *SQLite) CreateFeed(ctx context.Context, feed *model.PodcastFeed) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO podcast_feeds (set_id, feed_url, title, description, image_url, last_checked_at, last_etag, check_interval_minutes, auto_download, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		feed.SetID, feed.FeedURL, sqlNullString(feed.Title), sqlNullString(feed.Description), sqlNullString(feed.ImageURL),
		sqlNullTime(feed.LastCheckedAt), sqlNullString(feed.LastETag), feed.CheckIntervalMinutes, boolToInt(feed.AutoDownload), feed.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert podcast feed: %w", err)
	}
	return res.LastInsertId()
}

// UpdateFeed replaces mutable fields for a podcast feed.
func (s *SQLite) UpdateFeed(ctx context.Context, feed *model.PodcastFeed) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE podcast_feeds SET feed_url = ?, title = ?, description = ?, image_url = ?, last_checked_at = ?, last_etag = ?, check_interval_minutes = ?, auto_download = ? WHERE id = ?`,
		feed.FeedURL, sqlNullString(feed.Title), sqlNullString(feed.Description), sqlNullString(feed.ImageURL),
		sqlNullTime(feed.LastCheckedAt), sqlNullString(feed.LastETag), feed.CheckIntervalMinutes, boolToInt(feed.AutoDownload), feed.ID,
	)
	if err != nil {
		return fmt.Errorf("update podcast feed: %w", err)
	}
	return nil
}

// DeleteFeed removes a podcast feed by database ID.
func (s *SQLite) DeleteFeed(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM podcast_feeds WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete podcast feed: %w", err)
	}
	return nil
}

func scanFeed(row sqlScanner) (*model.PodcastFeed, error) {
	var f model.PodcastFeed
	var title, description, imageURL, lastETag sql.NullString
	var lastChecked sql.NullTime
	err := row.Scan(&f.ID, &f.SetID, &f.FeedURL, &title, &description, &imageURL, &lastChecked, &lastETag, &f.CheckIntervalMinutes, &f.AutoDownload, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.Title = title.String
	f.Description = description.String
	f.ImageURL = imageURL.String
	if lastChecked.Valid {
		f.LastCheckedAt = &lastChecked.Time
	}
	f.LastETag = lastETag.String
	return &f, nil
}

// GetFeedByID returns a podcast feed by database ID.
func (s *SQLite) GetFeedByID(ctx context.Context, id int64) (*model.PodcastFeed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, set_id, feed_url, title, description, image_url, last_checked_at, last_etag, check_interval_minutes, auto_download, created_at FROM podcast_feeds WHERE id = ?`, id)
	return scanFeed(row)
}

// GetFeedBySetID returns a podcast feed linked to a set.
func (s *SQLite) GetFeedBySetID(ctx context.Context, setID int64) (*model.PodcastFeed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, set_id, feed_url, title, description, image_url, last_checked_at, last_etag, check_interval_minutes, auto_download, created_at FROM podcast_feeds WHERE set_id = ?`, setID)
	return scanFeed(row)
}

// ListFeeds returns all podcast feeds.
func (s *SQLite) ListFeeds(ctx context.Context) ([]model.PodcastFeed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, set_id, feed_url, title, description, image_url, last_checked_at, last_etag, check_interval_minutes, auto_download, created_at FROM podcast_feeds ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list podcast feeds: %w", err)
	}
	defer rows.Close()
	var feeds []model.PodcastFeed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, *f)
	}
	return feeds, rows.Err()
}

// ListFeedsNeedingCheck returns feeds whose last_checked_at is before the given time.
func (s *SQLite) ListFeedsNeedingCheck(ctx context.Context, before time.Time) ([]model.PodcastFeed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, set_id, feed_url, title, description, image_url, last_checked_at, last_etag, check_interval_minutes, auto_download, created_at FROM podcast_feeds
		WHERE last_checked_at IS NULL OR last_checked_at < ? ORDER BY last_checked_at ASC`, before)
	if err != nil {
		return nil, fmt.Errorf("list feeds needing check: %w", err)
	}
	defer rows.Close()
	var feeds []model.PodcastFeed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, *f)
	}
	return feeds, rows.Err()
}

// ------------------------------------------------------------------
// Podcast Episodes
// ------------------------------------------------------------------

// CreateEpisode stores a new podcast episode and returns its database ID.
func (s *SQLite) CreateEpisode(ctx context.Context, episode *model.PodcastEpisode) (int64, error) {
	var mediaID sql.NullInt64
	if episode.MediaID != nil {
		mediaID = sql.NullInt64{Int64: *episode.MediaID, Valid: true}
	}
	var published sql.NullTime
	if episode.PublishedAt != nil {
		published = sql.NullTime{Time: *episode.PublishedAt, Valid: true}
	}
	var duration sql.NullFloat64
	if episode.DurationSeconds != nil {
		duration = sql.NullFloat64{Float64: *episode.DurationSeconds, Valid: true}
	}
	var fileSize sql.NullInt64
	if episode.FileSize != nil {
		fileSize = sql.NullInt64{Int64: *episode.FileSize, Valid: true}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO podcast_episodes (feed_id, media_id, guid, title, description, published_at, episode_url, duration_seconds, file_size, file_name, is_downloaded, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		episode.FeedID, mediaID, episode.GUID, sqlNullString(episode.Title), sqlNullString(episode.Description),
		published, episode.EpisodeURL, duration, fileSize, sqlNullString(episode.FileName), boolToInt(episode.IsDownloaded), episode.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert podcast episode: %w", err)
	}
	return res.LastInsertId()
}

func scanEpisode(row sqlScanner) (*model.PodcastEpisode, error) {
	var e model.PodcastEpisode
	var mediaID sql.NullInt64
	var title, description, fileName sql.NullString
	var published sql.NullTime
	var duration sql.NullFloat64
	var fileSize sql.NullInt64
	err := row.Scan(&e.ID, &e.FeedID, &mediaID, &e.GUID, &title, &description, &published, &e.EpisodeURL, &duration, &fileSize, &fileName, &e.IsDownloaded, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if mediaID.Valid {
		e.MediaID = &mediaID.Int64
	}
	e.Title = title.String
	e.Description = description.String
	if published.Valid {
		e.PublishedAt = &published.Time
	}
	if duration.Valid {
		e.DurationSeconds = &duration.Float64
	}
	if fileSize.Valid {
		e.FileSize = &fileSize.Int64
	}
	e.FileName = fileName.String
	return &e, nil
}

// GetEpisodeByID returns a podcast episode by database ID.
func (s *SQLite) GetEpisodeByID(ctx context.Context, id int64) (*model.PodcastEpisode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, feed_id, media_id, guid, title, description, published_at, episode_url, duration_seconds, file_size, file_name, is_downloaded, created_at FROM podcast_episodes WHERE id = ?`, id)
	return scanEpisode(row)
}

// GetEpisodeByGUID returns an episode by feed ID and GUID.
func (s *SQLite) GetEpisodeByGUID(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, feed_id, media_id, guid, title, description, published_at, episode_url, duration_seconds, file_size, file_name, is_downloaded, created_at FROM podcast_episodes WHERE feed_id = ? AND guid = ?`, feedID, guid)
	return scanEpisode(row)
}

// ListEpisodesByFeed returns episodes for a feed with pagination.
func (s *SQLite) ListEpisodesByFeed(ctx context.Context, feedID int64, limit, offset int) ([]model.PodcastEpisode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, feed_id, media_id, guid, title, description, published_at, episode_url, duration_seconds, file_size, file_name, is_downloaded, created_at FROM podcast_episodes WHERE feed_id = ? ORDER BY published_at DESC LIMIT ? OFFSET ?`,
		feedID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list podcast episodes: %w", err)
	}
	defer rows.Close()
	var episodes []model.PodcastEpisode
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, *e)
	}
	return episodes, rows.Err()
}

// UpdateEpisodeMedia links an episode to a media row after downloading.
func (s *SQLite) UpdateEpisodeMedia(ctx context.Context, episodeID, mediaID int64, fileName string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE podcast_episodes SET media_id = ?, file_name = ?, is_downloaded = 1 WHERE id = ?`,
		mediaID, fileName, episodeID)
	if err != nil {
		return fmt.Errorf("update episode media: %w", err)
	}
	return nil
}

// DeleteEpisodesByFeed removes all episodes for a feed.
func (s *SQLite) DeleteEpisodesByFeed(ctx context.Context, feedID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM podcast_episodes WHERE feed_id = ?`, feedID)
	if err != nil {
		return fmt.Errorf("delete episodes by feed: %w", err)
	}
	return nil
}

// ------------------------------------------------------------------
// Podcast Status
// ------------------------------------------------------------------

// UpsertEpisodeProgress saves or updates a user's episode status.
func (s *SQLite) UpsertEpisodeProgress(ctx context.Context, status *model.PodcastStatus) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO podcast_status (user_id, episode_id, is_completed, position_seconds, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, episode_id) DO UPDATE SET
		is_completed = excluded.is_completed,
		position_seconds = excluded.position_seconds,
		updated_at = excluded.updated_at`,
		status.UserID, status.EpisodeID, boolToInt(status.IsCompleted), status.PositionSeconds, status.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert podcast status: %w", err)
	}
	return nil
}

// GetEpisodeProgress returns a user's status for an episode.
func (s *SQLite) GetEpisodeProgress(ctx context.Context, userID, episodeID int64) (*model.PodcastStatus, error) {
	var st model.PodcastStatus
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, episode_id, is_completed, position_seconds, updated_at FROM podcast_status WHERE user_id = ? AND episode_id = ?`,
		userID, episodeID).Scan(&st.UserID, &st.EpisodeID, &st.IsCompleted, &st.PositionSeconds, &st.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get podcast status: %w", err)
	}
	return &st, nil
}

// ListEpisodesWithStatus returns episodes with per-user completion and progress.
func (s *SQLite) ListEpisodesWithStatus(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.feed_id, e.media_id, e.guid, e.title, e.description, e.published_at, e.episode_url, e.duration_seconds, e.file_size, e.file_name, e.is_downloaded, e.created_at,
		COALESCE(s.is_completed, 0) AS is_completed, COALESCE(s.position_seconds, 0) AS position_seconds
		FROM podcast_episodes e
		LEFT JOIN podcast_status s ON s.episode_id = e.id AND s.user_id = ?
		WHERE e.feed_id = ?
		ORDER BY e.published_at DESC
		LIMIT ? OFFSET ?`,
		userID, feedID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list episodes with status: %w", err)
	}
	defer rows.Close()
	var episodes []model.PodcastEpisodeWithStatus
	for rows.Next() {
		var e model.PodcastEpisodeWithStatus
		var mediaID sql.NullInt64
		var title, description, fileName sql.NullString
		var published sql.NullTime
		var duration sql.NullFloat64
		var fileSize sql.NullInt64
		err := rows.Scan(&e.ID, &e.FeedID, &mediaID, &e.GUID, &title, &description, &published, &e.EpisodeURL, &duration, &fileSize, &fileName, &e.IsDownloaded, &e.CreatedAt, &e.IsCompleted, &e.PositionSeconds)
		if err != nil {
			return nil, err
		}
		if mediaID.Valid {
			e.MediaID = &mediaID.Int64
		}
		e.Title = title.String
		e.Description = description.String
		if published.Valid {
			e.PublishedAt = &published.Time
		}
		if duration.Valid {
			e.DurationSeconds = &duration.Float64
		}
		if fileSize.Valid {
			e.FileSize = &fileSize.Int64
		}
		e.FileName = fileName.String
		episodes = append(episodes, e)
	}
	return episodes, rows.Err()
}


