package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/snonux/player/internal/model"
)

// Episode views of the web (docs/podcast-sync-contract.md §6.3): the
// episodes of the feeds a user is subscribed to, joined with that user's
// episode state, per feed or across all feeds, filtered by completion and
// paged in a stable order.

// PodcastEpisodeQuery selects an episode view.
type PodcastEpisodeQuery struct {
	// FeedKey limits the view to one feed (a canonical key or an alias);
	// "" lists the episodes of every subscribed feed.
	FeedKey string
	// Filter is model.PodcastFilterUncompleted, ...Completed or ...All.
	Filter        string
	Limit, Offset int
}

// PodcastEpisodeViewRepo lists episode views.
type PodcastEpisodeViewRepo interface {
	// ListSubscribedEpisodes returns the episodes of the user's subscribed
	// catalog feeds (subscription under the feed key or an alias of it)
	// with the user's registers, newest first by publication date, then by
	// episode key. Server copies are reported when not deleted; whether
	// the user may read them is the caller's decision.
	ListSubscribedEpisodes(ctx context.Context, userID int64, q PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error)
}

var _ PodcastEpisodeViewRepo = (*SQLite)(nil)

// subscribedEpisodesQuery emits each canonical episode once across every
// catalog row sharing the feed key. Prefer a live copy the caller can read;
// otherwise keep the oldest row for stable metadata and download controls.
const subscribedEpisodesQuery = `WITH candidates AS (
SELECT e.id, f.feed_key, COALESCE(a.episode_key, e.episode_key) AS episode_key, COALESCE(f.title, '') AS feed_title, COALESCE(e.title, '') AS title,
 COALESCE(e.description, '') AS description, COALESCE(e.image_url, '') AS image_url, COALESCE(f.image_url, '') AS feed_image_url, e.published_at, e.duration_seconds, e.episode_url,
 COALESCE(s.completed, 0) AS completed, COALESCE(s.completion_source, '') AS completion_source,
 COALESCE(s.position_seconds, 0) AS position_seconds,
 CASE WHEN m.id IS NOT NULL AND m.deleted_at IS NULL AND
  (EXISTS (SELECT 1 FROM users u WHERE u.id = ?1 AND u.is_admin = 1)
   OR EXISTS (SELECT 1 FROM set_permissions p WHERE p.user_id = ?1 AND p.set_id = m.set_id))
  THEN m.id END AS readable_copy, d.set_id
FROM podcast_feeds f
JOIN podcast_feeds d ON d.feed_key = f.feed_key
JOIN podcast_episodes e ON e.feed_id = d.id
LEFT JOIN podcast_episode_aliases a ON a.feed_key = f.feed_key AND a.alias_key = e.episode_key
LEFT JOIN podcast_episode_state s ON s.user_id = ?1 AND s.feed_key = f.feed_key AND s.episode_key = COALESCE(a.episode_key, e.episode_key)
LEFT JOIN media m ON m.id = e.media_id
WHERE f.merged_into IS NULL
 AND EXISTS (SELECT 1 FROM podcast_subscriptions sub WHERE sub.user_id = ?1 AND sub.subscribed = 1 AND (
  sub.feed_key = f.feed_key OR sub.feed_key IN (SELECT alias_key FROM podcast_feed_aliases a WHERE a.feed_key = f.feed_key)))
 AND (?2 = '' OR f.feed_key = ?2)
 AND (?3 = 'all' OR COALESCE(s.completed, 0) = (?3 = 'completed'))
), ranked AS (
 SELECT *, ROW_NUMBER() OVER (PARTITION BY feed_key, episode_key ORDER BY readable_copy IS NULL, id) AS rank FROM candidates
)
SELECT id, feed_key, episode_key, feed_title, title, description, image_url, feed_image_url, published_at, duration_seconds, episode_url,
 completed, completion_source, position_seconds, readable_copy, set_id
FROM ranked WHERE rank = 1
ORDER BY published_at IS NULL, published_at DESC, episode_key, feed_key
LIMIT ?4 OFFSET ?5`

// ListSubscribedEpisodes implements PodcastEpisodeViewRepo. A feed key that
// denotes no catalog feed lists nothing.
func (s *SQLite) ListSubscribedEpisodes(ctx context.Context, userID int64, q PodcastEpisodeQuery) ([]model.PodcastEpisodeView, error) {
	feedKey := ""
	if q.FeedKey != "" {
		canonical, err := canonicalFeedKey(ctx, s.db, q.FeedKey)
		if err != nil || canonical == "" {
			return []model.PodcastEpisodeView{}, err
		}
		feedKey = canonical
	}
	rows, err := s.db.QueryContext(ctx, subscribedEpisodesQuery, userID, feedKey, q.Filter, q.Limit, q.Offset)
	if err != nil {
		return nil, fmt.Errorf("list subscribed episodes: %w", err)
	}
	views := []model.PodcastEpisodeView{}
	for rows.Next() {
		v, err := scanEpisodeView(rows)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan subscribed episode: %w", err)
		}
		views = append(views, v)
	}
	return views, closeRows(rows)
}

// scanEpisodeView scans one row of subscribedEpisodesQuery.
func scanEpisodeView(rows *sql.Rows) (model.PodcastEpisodeView, error) {
	var v model.PodcastEpisodeView
	var published sql.NullTime
	var duration sql.NullFloat64
	var mediaID sql.NullInt64
	err := rows.Scan(&v.ID, &v.FeedKey, &v.EpisodeKey, &v.FeedTitle, &v.Title, &v.Description, &v.ImageURL, &v.FeedImageURL, &published, &duration,
		&v.EpisodeURL, &v.Completed, &v.CompletionSource, &v.PositionSeconds, &mediaID, &v.SetID)
	if err != nil {
		return v, err
	}
	if published.Valid {
		v.PublishedAt = &published.Time
	}
	if duration.Valid {
		v.DurationSeconds = &duration.Float64
	}
	if mediaID.Valid {
		v.ServerCopy = &model.PodcastServerCopy{MediaID: mediaID.Int64}
	}
	return v, nil
}
