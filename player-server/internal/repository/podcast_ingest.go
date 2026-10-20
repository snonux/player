package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
)

// IngestFeedEpisodes implements PodcastCatalogRepo. Re-ingesting the same
// document changes nothing; known episodes keep their ids, media links and
// metadata, only their latest enclosure (key and URL) is refreshed, so a
// rotating signed URL neither creates episodes nor accumulates aliases.
func (s *SQLite) IngestFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, episodes []model.PodcastEpisode, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ingest episodes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	known, ids, err := loadKnownEpisodes(ctx, tx, feed)
	if err != nil {
		return err
	}
	items := make([]podcast.IdentityItem, len(episodes))
	for i, e := range episodes {
		items[i] = podcast.IdentityItem{GUID: e.GUID, EnclosureURL: e.EpisodeURL}
	}
	for i, m := range podcast.MatchEpisodes(known, items) {
		if m.Key == "" {
			continue
		}
		if err := applyIngestMatch(ctx, tx, feed, m, episodes[i], ids, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ingest episodes: %w", err)
	}
	return nil
}

// applyIngestMatch stores the outcome for one item: a new episode, or the
// refreshed enclosure of a known one, plus an alias when ingest found one.
func applyIngestMatch(ctx context.Context, tx *sql.Tx, feed *model.PodcastFeed, m podcast.IngestMatch, ep model.PodcastEpisode, ids map[string]int64, now time.Time) error {
	enc, _ := podcast.EnclosureKey(ep.EpisodeURL)
	if m.New {
		ep.ID, ep.FeedID, ep.MediaID, ep.FileName, ep.IsDownloaded = 0, feed.ID, nil, "", false
		ep.EpisodeKey, ep.EnclosureKey, ep.CreatedAt = m.Key, enc, now
		if _, err := insertEpisode(ctx, tx, &ep); err != nil {
			return fmt.Errorf("ingest episode %q: %w", m.Key, err)
		}
	} else if _, err := tx.ExecContext(ctx,
		`UPDATE podcast_episodes SET enclosure_key = ?, episode_url = ? WHERE id = ?`,
		enc, ep.EpisodeURL, ids[m.Key]); err != nil {
		return fmt.Errorf("refresh episode %q: %w", m.Key, err)
	}
	if m.Alias == "" {
		return nil
	}
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO podcast_episode_aliases
(feed_key, alias_key, episode_key, change_seq) VALUES (?, ?, ?, ?)`, feed.FeedKey, m.Alias, m.Key, seq); err != nil {
		return fmt.Errorf("record episode alias: %w", err)
	}
	return nil
}

// loadKnownEpisodes returns the feed's catalog episodes as ingest sees them
// and their ids by canonical key.
func loadKnownEpisodes(ctx context.Context, tx *sql.Tx, feed *model.PodcastFeed) ([]podcast.KnownEpisode, map[string]int64, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, episode_key, enclosure_key FROM podcast_episodes WHERE feed_id = ? ORDER BY id`, feed.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("load known episodes: %w", err)
	}
	var known []podcast.KnownEpisode
	ids := map[string]int64{}
	index := map[string]int{}
	for rows.Next() {
		var id int64
		var ep podcast.KnownEpisode
		if err := rows.Scan(&id, &ep.Key, &ep.EnclosureKey); err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("scan known episode: %w", err)
		}
		ids[ep.Key], index[ep.Key] = id, len(known)
		known = append(known, ep)
	}
	if err := closeRows(rows); err != nil {
		return nil, nil, err
	}
	if err := loadEpisodeAliases(ctx, tx, feed.FeedKey, known, index); err != nil {
		return nil, nil, err
	}
	return known, ids, nil
}

// loadEpisodeAliases adds the recorded aliases to the known episodes.
func loadEpisodeAliases(ctx context.Context, tx *sql.Tx, feedKey string, known []podcast.KnownEpisode, index map[string]int) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT alias_key, episode_key FROM podcast_episode_aliases WHERE feed_key = ? ORDER BY change_seq`, feedKey)
	if err != nil {
		return fmt.Errorf("load episode aliases: %w", err)
	}
	for rows.Next() {
		var alias, key string
		if err := rows.Scan(&alias, &key); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan episode alias: %w", err)
		}
		if i, ok := index[key]; ok {
			known[i].Aliases = append(known[i].Aliases, alias)
		}
	}
	return closeRows(rows)
}
