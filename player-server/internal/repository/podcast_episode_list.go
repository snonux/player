package repository

import (
	"context"
	"database/sql"
	"fmt"

	"codeberg.org/snonux/player/internal/model"
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

// subscribedEpisodesQuery lists the episode views; ?1 is the user, ?2 the
// canonical feed key or "", ?3 the completion filter. Only catalog rows not
// merged into another are listed. Episodes that exist only on a merged-away
// row (it keeps its episode rows, contract §3.3) are therefore missing from
// the views; listing them through the winning row needs the feed merge
// itself and is left to task mn3.
const subscribedEpisodesQuery = `SELECT e.id, f.feed_key, e.episode_key, COALESCE(f.title, ''), COALESCE(e.title, ''),
 COALESCE(e.description, ''), e.published_at, e.duration_seconds, e.episode_url,
 COALESCE(s.completed, 0), COALESCE(s.completion_source, ''), COALESCE(s.position_seconds, 0),
 CASE WHEN m.id IS NOT NULL AND m.deleted_at IS NULL THEN m.id END, f.set_id
FROM podcast_feeds f
JOIN podcast_episodes e ON e.feed_id = f.id
LEFT JOIN podcast_episode_state s ON s.user_id = ?1 AND s.feed_key = f.feed_key AND s.episode_key = e.episode_key
LEFT JOIN media m ON m.id = e.media_id
WHERE f.merged_into IS NULL
 AND EXISTS (SELECT 1 FROM podcast_subscriptions sub WHERE sub.user_id = ?1 AND sub.subscribed = 1 AND (
  sub.feed_key = f.feed_key OR sub.feed_key IN (SELECT alias_key FROM podcast_feed_aliases a WHERE a.feed_key = f.feed_key)))
 AND (?2 = '' OR f.feed_key = ?2)
 AND (?3 = 'all' OR COALESCE(s.completed, 0) = (?3 = 'completed'))
ORDER BY e.published_at IS NULL, e.published_at DESC, e.episode_key
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
	err := rows.Scan(&v.ID, &v.FeedKey, &v.EpisodeKey, &v.FeedTitle, &v.Title, &v.Description, &published, &duration,
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
