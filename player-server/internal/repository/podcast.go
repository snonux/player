package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/podcast"
)

// ------------------------------------------------------------------
// Podcast Feeds
// ------------------------------------------------------------------

// feedColumns are the podcast_feeds columns scanFeed reads, in its order.
const feedColumns = `id, set_id, feed_url, title, description, image_url, last_checked_at, last_etag,
 check_interval_minutes, auto_download, consecutive_failures, next_check_at, created_at,
 feed_key, merged_into, created_by`

// episodeColumns are the podcast_episodes columns scanEpisode reads.
const episodeColumns = `id, feed_id, media_id, guid, episode_key, enclosure_key, title, description,
 published_at, episode_url, duration_seconds, file_size, file_name, is_downloaded, created_at, image_url`

// CreateFeed stores a new catalog feed and returns its database ID. A feed
// without a FeedKey gets the key of its URL, or "legacy-feed:<id>" when the
// URL has none (credentials, unsupported scheme, ...).
func (s *SQLite) CreateFeed(ctx context.Context, feed *model.PodcastFeed) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("insert podcast feed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertFeed(ctx, tx, feed)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("insert podcast feed: %w", err)
	}
	return id, nil
}

// insertFeed inserts a catalog feed inside the caller's transaction and
// sets feed.FeedKey to the key it was stored under.
func insertFeed(ctx context.Context, tx *sql.Tx, feed *model.PodcastFeed) (int64, error) {
	key := feed.FeedKey
	if key == "" {
		// Unique among catalog rows until it is replaced below.
		key = fmt.Sprintf("legacy-feed:pending-%d", feed.CreatedAt.UnixNano())
		if k, err := podcast.FeedKey(feed.FeedURL); err == nil {
			key = k
		}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO podcast_feeds (set_id, feed_url, title, description, image_url, last_checked_at, last_etag, check_interval_minutes, auto_download, consecutive_failures, next_check_at, created_at, feed_key, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		feed.SetID, feed.FeedURL, sqlNullString(feed.Title), sqlNullString(feed.Description), sqlNullString(feed.ImageURL),
		sqlNullTime(feed.LastCheckedAt), sqlNullString(feed.LastETag), feed.CheckIntervalMinutes, boolToInt(feed.AutoDownload),
		feed.ConsecutiveFailures, sqlNullTime(feed.NextCheckAt), feed.CreatedAt, key, feed.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert podcast feed: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("insert podcast feed: %w", err)
	}
	if strings.HasPrefix(key, "legacy-feed:pending-") {
		key = fmt.Sprintf("legacy-feed:%d", id)
		if _, err := tx.ExecContext(ctx, `UPDATE podcast_feeds SET feed_key = ? WHERE id = ?`, key, id); err != nil {
			return 0, fmt.Errorf("set legacy feed key: %w", err)
		}
	}
	feed.FeedKey = key
	return id, nil
}

// UpdateFeed replaces mutable fields for a podcast feed.
func (s *SQLite) UpdateFeed(ctx context.Context, feed *model.PodcastFeed) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE podcast_feeds SET feed_url = ?, title = ?, description = ?, image_url = ?, last_checked_at = ?, last_etag = ?, check_interval_minutes = ?, auto_download = ?, consecutive_failures = ?, next_check_at = ? WHERE id = ?`,
		feed.FeedURL, sqlNullString(feed.Title), sqlNullString(feed.Description), sqlNullString(feed.ImageURL),
		sqlNullTime(feed.LastCheckedAt), sqlNullString(feed.LastETag), feed.CheckIntervalMinutes, boolToInt(feed.AutoDownload),
		feed.ConsecutiveFailures, sqlNullTime(feed.NextCheckAt), feed.ID,
	)
	if err != nil {
		return fmt.Errorf("update podcast feed: %w", err)
	}
	return nil
}

// DeleteFeed removes every catalog row sharing the given row's key only
// while no active subscription exists. Checking and deleting in a single
// statement serializes this decision with a concurrent subscription edit.
func (s *SQLite) DeleteFeed(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM podcast_feeds
WHERE feed_key = (SELECT feed_key FROM podcast_feeds WHERE id = ?)
 AND NOT EXISTS (SELECT 1 FROM podcast_subscriptions s WHERE s.subscribed = 1
 AND (s.feed_key = podcast_feeds.feed_key OR s.feed_key IN
  (SELECT alias_key FROM podcast_feed_aliases a WHERE a.feed_key = podcast_feeds.feed_key)))`, id)
	if err != nil {
		return fmt.Errorf("delete podcast feed: %w", err)
	}
	return nil
}

func scanFeed(row sqlScanner) (*model.PodcastFeed, error) {
	var f model.PodcastFeed
	var title, description, imageURL, lastETag sql.NullString
	var lastChecked, nextCheckAt sql.NullTime
	var autoDownloadInt int
	var mergedInto, createdBy sql.NullInt64
	err := row.Scan(&f.ID, &f.SetID, &f.FeedURL, &title, &description, &imageURL, &lastChecked, &lastETag, &f.CheckIntervalMinutes, &autoDownloadInt, &f.ConsecutiveFailures, &nextCheckAt, &f.CreatedAt,
		&f.FeedKey, &mergedInto, &createdBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.AutoDownload = intToBool(autoDownloadInt)
	if mergedInto.Valid {
		f.MergedInto = &mergedInto.Int64
	}
	if createdBy.Valid {
		f.CreatedBy = &createdBy.Int64
	}
	f.Title = title.String
	f.Description = description.String
	f.ImageURL = imageURL.String
	if lastChecked.Valid {
		f.LastCheckedAt = &lastChecked.Time
	}
	f.LastETag = lastETag.String
	if nextCheckAt.Valid {
		f.NextCheckAt = &nextCheckAt.Time
	}
	return &f, nil
}

// GetFeedByID returns a podcast feed by database ID.
func (s *SQLite) GetFeedByID(ctx context.Context, id int64) (*model.PodcastFeed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+feedColumns+` FROM podcast_feeds WHERE id = ?`, id)
	return scanFeed(row)
}

// GetFeedBySetID returns a podcast feed linked to a set.
func (s *SQLite) GetFeedBySetID(ctx context.Context, setID int64) (*model.PodcastFeed, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+feedColumns+` FROM podcast_feeds WHERE set_id = ?`, setID)
	return scanFeed(row)
}

// ListFeedsBySetID returns all podcast feeds linked to a set.
func (s *SQLite) ListFeedsBySetID(ctx context.Context, setID int64) ([]model.PodcastFeed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+feedColumns+` FROM podcast_feeds WHERE set_id = ? ORDER BY title, created_at`, setID)
	if err != nil {
		return nil, fmt.Errorf("list podcast feeds by set: %w", err)
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

// ListFeeds returns all catalog feeds; rows merged into another are left out.
func (s *SQLite) ListFeeds(ctx context.Context) ([]model.PodcastFeed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+feedColumns+` FROM podcast_feeds WHERE merged_into IS NULL ORDER BY created_at DESC`)
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

// ListFeedsNeedingCheck returns feeds eligible for a background check:
// next_check_at is NULL or in the past, last_checked_at is older than the
// given threshold, the row is not merged into another, and at least one
// user has an active subscription to its feed key or one of its aliases (a
// subscription kept under a key that only later became an alias). A feed
// nobody listens to any more is not fetched (contract §7.3).
func (s *SQLite) ListFeedsNeedingCheck(ctx context.Context, now, before time.Time) ([]model.PodcastFeed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+feedColumns+` FROM podcast_feeds
		WHERE (next_check_at IS NULL OR next_check_at <= ?) AND (last_checked_at IS NULL OR last_checked_at < ?)
		AND merged_into IS NULL
		AND EXISTS (SELECT 1 FROM podcast_subscriptions s WHERE s.subscribed = 1 AND (s.feed_key = podcast_feeds.feed_key
			OR s.feed_key IN (SELECT alias_key FROM podcast_feed_aliases a WHERE a.feed_key = podcast_feeds.feed_key)))
		ORDER BY last_checked_at ASC`, now, before)
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

// CreateEpisode stores one podcast episode and returns its database ID. An
// episode without an EpisodeKey is keyed by its GUID, else by its enclosure.
// Feed ingest uses IngestFeedEpisodes instead, which keys whole documents
// by the contract rules; this is for single rows (tests, tools).
func (s *SQLite) CreateEpisode(ctx context.Context, episode *model.PodcastEpisode) (int64, error) {
	if episode.EpisodeKey == "" {
		if guid := strings.TrimSpace(episode.GUID); guid != "" {
			episode.EpisodeKey = podcast.GUIDKey(guid)
		} else if key, err := podcast.EnclosureKey(episode.EpisodeURL); err == nil {
			episode.EpisodeKey = key
		} else {
			return 0, fmt.Errorf("insert podcast episode: no guid and %w", err)
		}
	}
	if episode.EnclosureKey == "" {
		episode.EnclosureKey, _ = podcast.EnclosureKey(episode.EpisodeURL)
	}
	return insertEpisode(ctx, s.db, episode)
}

// insertEpisode inserts an episode under its EpisodeKey. An empty GUID is
// stored as NULL.
func insertEpisode(ctx context.Context, ex sqlExecer, episode *model.PodcastEpisode) (int64, error) {
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
	res, err := ex.ExecContext(ctx,
		`INSERT INTO podcast_episodes (feed_id, media_id, guid, episode_key, enclosure_key, title, description, published_at, episode_url, duration_seconds, file_size, file_name, is_downloaded, created_at, image_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		episode.FeedID, mediaID, sqlNullString(episode.GUID), episode.EpisodeKey, episode.EnclosureKey, sqlNullString(episode.Title), sqlNullString(episode.Description),
		published, episode.EpisodeURL, duration, fileSize, sqlNullString(episode.FileName), boolToInt(episode.IsDownloaded), episode.CreatedAt, episode.ImageURL,
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
	var isDownloadedInt int
	var guid sql.NullString
	err := row.Scan(&e.ID, &e.FeedID, &mediaID, &guid, &e.EpisodeKey, &e.EnclosureKey, &title, &description, &published, &e.EpisodeURL, &duration, &fileSize, &fileName, &isDownloadedInt, &e.CreatedAt, &e.ImageURL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.GUID = guid.String
	e.IsDownloaded = intToBool(isDownloadedInt)
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
		`SELECT `+episodeColumns+` FROM podcast_episodes WHERE id = ?`, id)
	return scanEpisode(row)
}

// GetEpisodeByGUID returns an episode by feed ID and GUID.
func (s *SQLite) GetEpisodeByGUID(ctx context.Context, feedID int64, guid string) (*model.PodcastEpisode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+episodeColumns+` FROM podcast_episodes WHERE feed_id = ? AND guid = ?`, feedID, guid)
	return scanEpisode(row)
}

// ListEpisodesByFeed returns episodes for a feed with pagination.
func (s *SQLite) ListEpisodesByFeed(ctx context.Context, feedID int64, limit, offset int) ([]model.PodcastEpisode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+episodeColumns+` FROM podcast_episodes WHERE feed_id = ? ORDER BY published_at DESC LIMIT ? OFFSET ?`,
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

// ListEpisodeMediaIDs returns the media IDs linked to downloaded episodes of
// feedIDs, ordered by ID. It is unpaged on purpose: LIMIT/OFFSET over a
// non-unique sort could skip or repeat rows.
func (s *SQLite) ListEpisodeMediaIDs(ctx context.Context, feedIDs []int64) ([]int64, error) {
	if len(feedIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT media_id FROM podcast_episodes WHERE media_id IS NOT NULL AND feed_id IN (SELECT id FROM podcast_feeds WHERE feed_key IN
 (SELECT feed_key FROM podcast_feeds WHERE id IN (`+placeholders(len(feedIDs))+`))) ORDER BY media_id`,
		anySlice(feedIDs)...)
	if err != nil {
		return nil, fmt.Errorf("list episode media ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan episode media id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpdateEpisodeMedia links an episode to a media row after downloading. It
// returns an error wrapping sql.ErrNoRows when the episode no longer exists
// (its feed was unsubscribed during the download), so the caller can undo
// the download instead of leaving an unlinked media row behind. In the same
// transaction every user's episode state is projected onto the new copy's
// playback_progress (contract §7.1), so legacy readers see one history.
func (s *SQLite) UpdateEpisodeMedia(ctx context.Context, episodeID, mediaID int64, fileName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update episode media: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`UPDATE podcast_episodes SET media_id = ?, file_name = ?, is_downloaded = 1 WHERE id = ?`,
		mediaID, fileName, episodeID)
	if err != nil {
		return fmt.Errorf("update episode media: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("update episode media: episode %d: %w", episodeID, sql.ErrNoRows)
	}
	// The new copy starts from the episode state every client shares.
	if err := seedEpisodeMirrors(ctx, tx, episodeID, mediaID); err != nil {
		return fmt.Errorf("update episode media: %w", err)
	}
	if err := tx.Commit(); err != nil {
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
// Episodes with the caller's episode state
// ------------------------------------------------------------------

// The legacy podcast_status table is kept read-only for one release as a
// rollback aid (contract §9.1 step 8); nothing reads or writes it any more.
// Listings read the caller's episode registers instead, joined through the
// feed and episode keys, so state written by any client (sync API, legacy
// toggle, media progress of a server copy) shows up here.

// episodeWithStatusSelect lists episodes with the user's registers; the
// first placeholder is the user id.
const episodeWithStatusSelect = `SELECT e.id, e.feed_id, e.media_id, e.guid, COALESCE(a.episode_key, e.episode_key), e.enclosure_key, e.title, e.description,
 e.published_at, e.episode_url, e.duration_seconds, e.file_size, e.file_name, e.is_downloaded, e.created_at, COALESCE(e.image_url,''), COALESCE(f.image_url,''), f.feed_key,
 COALESCE(s.completed, 0), COALESCE(s.position_seconds, 0)
FROM podcast_episodes e
JOIN podcast_feeds f ON f.id = e.feed_id
LEFT JOIN podcast_episode_aliases a ON a.feed_key = f.feed_key AND a.alias_key = e.episode_key
LEFT JOIN podcast_episode_state s ON s.user_id = ? AND s.feed_key = f.feed_key AND s.episode_key = COALESCE(a.episode_key, e.episode_key)`

func scanEpisodeWithStatus(rows *sql.Rows) (model.PodcastEpisodeWithStatus, error) {
	var e model.PodcastEpisodeWithStatus
	var mediaID sql.NullInt64
	var title, description, fileName sql.NullString
	var published sql.NullTime
	var duration sql.NullFloat64
	var fileSize sql.NullInt64
	var isDownloadedInt, isCompletedInt int
	var positionSeconds sql.NullFloat64
	var guid sql.NullString
	err := rows.Scan(&e.ID, &e.FeedID, &mediaID, &guid, &e.EpisodeKey, &e.EnclosureKey, &title, &description, &published, &e.EpisodeURL, &duration, &fileSize, &fileName, &isDownloadedInt, &e.CreatedAt, &e.ImageURL, &e.FeedImageURL, &e.FeedKey, &isCompletedInt, &positionSeconds)
	if err != nil {
		return e, err
	}
	e.GUID = guid.String
	e.IsDownloaded = intToBool(isDownloadedInt)
	e.IsCompleted = intToBool(isCompletedInt)
	if positionSeconds.Valid {
		e.PositionSeconds = positionSeconds.Float64
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
	return e, nil
}

// ListEpisodesWithStatus returns a feed's episodes with the user's
// completion and position registers.
func (s *SQLite) ListEpisodesWithStatus(ctx context.Context, userID, feedID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
	rows, err := s.db.QueryContext(ctx, episodeWithStatusSelect+`
WHERE e.feed_id = ? AND COALESCE(s.deleted, 0) = 0
ORDER BY e.published_at DESC
LIMIT ? OFFSET ?`, userID, feedID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list episodes with status: %w", err)
	}
	defer rows.Close()
	var episodes []model.PodcastEpisodeWithStatus
	for rows.Next() {
		e, err := scanEpisodeWithStatus(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, e)
	}
	return episodes, rows.Err()
}

// ListEpisodesByFeedIDsWithStatus returns the episodes of several feeds
// with the user's completion and position registers, applying a global
// limit/offset.
func (s *SQLite) ListEpisodesByFeedIDsWithStatus(ctx context.Context, userID int64, feedIDs []int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
	if len(feedIDs) == 0 {
		return []model.PodcastEpisodeWithStatus{}, nil
	}

	placeholders := make([]string, len(feedIDs))
	args := make([]interface{}, 0, len(feedIDs)+3)
	args = append(args, userID)
	for i, id := range feedIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, limit, offset)

	query := fmt.Sprintf(episodeWithStatusSelect+`
WHERE e.feed_id IN (%s) AND COALESCE(s.deleted, 0) = 0
ORDER BY e.published_at DESC
LIMIT ? OFFSET ?`, strings.Join(placeholders, ", "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list episodes by feed ids with status: %w", err)
	}
	defer rows.Close()
	var episodes []model.PodcastEpisodeWithStatus
	for rows.Next() {
		e, err := scanEpisodeWithStatus(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, e)
	}
	return episodes, rows.Err()
}
