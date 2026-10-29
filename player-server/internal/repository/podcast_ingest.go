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

// ingestFeed is the catalog row an ingest writes to and the feed key its
// episodes are known under, read inside the ingest's transaction.
type ingestFeed struct {
	id  int64
	key string
}

// IngestFeedEpisodes implements PodcastCatalogRepo. Re-ingesting the same
// document changes nothing; known episodes keep their ids, media links and
// state. Publisher metadata and the latest enclosure (key and URL) are refreshed, so a
// rotating signed URL neither creates episodes nor accumulates aliases.
//
// The episodes known to the feed are those of every catalog row sharing its
// key (rows merged into it keep theirs, contract §3.3), so a feed that
// absorbed another never mints a second episode for one the other row
// holds. The feed's key is read inside the transaction: a row merged into
// another since the caller loaded it is not refreshed any more, and a
// retired legacy key is not used.
func (s *SQLite) IngestFeedEpisodes(ctx context.Context, feed *model.PodcastFeed, episodes []model.PodcastEpisode, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ingest episodes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	target := ingestFeed{id: feed.ID}
	err = tx.QueryRowContext(ctx, `SELECT feed_key FROM podcast_feeds WHERE id = ? AND merged_into IS NULL`, feed.ID).Scan(&target.key)
	if err == sql.ErrNoRows {
		return nil // removed or merged into another catalog feed meanwhile
	}
	if err != nil {
		return fmt.Errorf("ingest episodes: %w", err)
	}
	known, err := loadKnownEpisodesByKey(ctx, tx, target.key)
	if err != nil {
		return err
	}
	items := make([]podcast.IdentityItem, len(episodes))
	for i, e := range episodes {
		items[i] = podcast.IdentityItem{GUID: e.GUID, EnclosureURL: e.EpisodeURL}
	}
	keys := podcast.EpisodeKeys(items)
	for i, m := range podcast.MatchEpisodes(known, items) {
		if m.Key == "" {
			continue
		}
		if err := applyIngestMatch(ctx, tx, target, m, keys[i], episodes[i], now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ingest episodes: %w", err)
	}
	return nil
}

// applyIngestMatch stores the outcome for one item: a new episode, or the
// refreshed enclosure of a known one, plus what ingest learnt about its
// identity (recordIngestIdentity). itemKey is the item's own key.
func applyIngestMatch(ctx context.Context, tx *sql.Tx, feed ingestFeed, m podcast.IngestMatch, itemKey string, ep model.PodcastEpisode, now time.Time) error {
	enc, _ := podcast.EnclosureKey(ep.EpisodeURL)
	if m.New {
		ep.ID, ep.FeedID, ep.MediaID, ep.FileName, ep.IsDownloaded = 0, feed.id, nil, "", false
		ep.EpisodeKey, ep.EnclosureKey, ep.CreatedAt = m.Key, enc, now
		if _, err := insertEpisode(ctx, tx, &ep); err != nil {
			return fmt.Errorf("ingest episode %q: %w", m.Key, err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE podcast_episodes SET enclosure_key = ?3, episode_url = ?4,
 title=CASE WHEN ?5<>'' THEN ?5 ELSE title END, description=CASE WHEN ?6<>'' THEN ?6 ELSE description END,
 published_at=COALESCE(?7,published_at), image_url=CASE WHEN ?8<>'' THEN ?8 ELSE image_url END
WHERE (episode_key = ?1 OR episode_key IN (SELECT alias_key FROM podcast_episode_aliases WHERE feed_key = ?2 AND episode_key = ?1))
 AND feed_id IN (SELECT id FROM podcast_feeds WHERE feed_key = ?2)`,
		m.Key, feed.key, enc, ep.EpisodeURL, strings.TrimSpace(ep.Title), strings.TrimSpace(ep.Description), sqlNullTime(ep.PublishedAt), ep.ImageURL); err != nil {
		return fmt.Errorf("refresh episode %q: %w", m.Key, err)
	}
	return recordIngestIdentity(ctx, tx, feed.key, m, itemKey)
}

// recordIngestIdentity records what matching a known episode taught:
//   - a legacy-episode: key claimed by an item with a key of its own (by
//     its stored enclosure, or through an alias recorded earlier) is
//     retired: the item's key becomes the episode's key and the legacy key
//     its alias (§3.1);
//   - otherwise a new alias (a GUID that appeared, changed or vanished),
//     and every user's state kept under that key, which was a state-only
//     key until now, moves to the episode.
func recordIngestIdentity(ctx context.Context, tx *sql.Tx, feedKey string, m podcast.IngestMatch, itemKey string) error {
	if strings.HasPrefix(m.Key, legacyEpisodePrefix) && itemKey != "" && itemKey != m.Key {
		free, err := episodeKeyFree(ctx, tx, feedKey, itemKey, m.Key)
		if err != nil {
			return err
		}
		if free {
			return renameEpisodeKey(ctx, tx, feedKey, m.Key, itemKey)
		}
	}
	if m.Alias == "" {
		return nil
	}
	if err := putEpisodeAlias(ctx, tx, feedKey, m.Alias, m.Key); err != nil {
		return err
	}
	return moveEpisodeKeyStates(ctx, tx, feedKey, m.Alias, m.Key)
}

// episodeKeyFree reports whether key may become the canonical key of the
// episode owner under feedKey: no catalog episode carries it and it is no
// alias of another episode.
func episodeKeyFree(ctx context.Context, tx *sql.Tx, feedKey, key, owner string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM podcast_episodes e JOIN podcast_feeds f ON f.id = e.feed_id WHERE f.feed_key = ?1 AND e.episode_key = ?2)
 + (SELECT COUNT(*) FROM podcast_episode_aliases WHERE feed_key = ?1 AND alias_key = ?2 AND episode_key <> ?3)`,
		feedKey, key, owner).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check episode key: %w", err)
	}
	return n == 0, nil
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
