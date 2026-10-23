package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// Feed-owned artwork file names: cover.jpg is downloaded on subscribe and
// .cover.jpg is generated folder artwork. Both belong to the feed's folder.
var feedArtwork = map[string]bool{"cover.jpg": true, ".cover.jpg": true}

// UnsubscribeFeed removes a catalog feed's server copies and, when nobody
// is subscribed to it any more, the catalog feed itself ("Remove from
// server", contract §7.4). Account state is never touched: subscriptions
// and episode state are not foreign keys of the feed, so they survive both
// cases, and a feed with active subscribers stays in the catalog (without
// copies) and keeps being refreshed for them.
//
// All catalog rows of the canonical key participate. Copies are removed
// before the catalog rows, so a failed cleanup can be retried with their
// episode links intact. The final repository deletion checks subscriptions
// atomically, preventing a concurrent subscribe from losing its catalog.
func (s *podcastSubscriptionService) UnsubscribeFeed(ctx context.Context, feedID int64, userID int64) error {
	store := s.podcastService.store
	feed, err := store.GetFeedByID(ctx, feedID)
	if err != nil {
		return fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return ErrNotFound
	}
	if err := s.podcastService.helper.verifySetModifyAccess(ctx, feed.SetID, userID); err != nil {
		return err
	}
	set, err := store.GetSetByID(ctx, feed.SetID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}
	media, err := s.episodeMedia(ctx, feed)
	if err != nil {
		return err
	}
	// Finish every collected copy even if the client disconnects.
	ctx = context.WithoutCancel(ctx)

	var errs []error
	folders := map[int64]map[string]bool{feed.SetID: {podcastFolderName("", feed.Title, feed.ID): true}}
	for _, m := range media {
		if folder := topFolder(m.RelPath); folder != "" {
			if folders[m.SetID] == nil {
				folders[m.SetID] = map[string]bool{}
			}
			folders[m.SetID][folder] = true
		}
		errs = append(errs, s.deleteMediaAndFile(ctx, m))
	}
	if err := errors.Join(errs...); err != nil {
		err = errors.Join(err, s.tidyFeedFolders(ctx, set, folders))
		return err // keep catalog/episode links for a retry
	}
	if err := store.ClearEpisodeDownloads(ctx, feed.ID); err != nil {
		return err
	}
	if err := store.DeleteFeed(ctx, feed.ID); err != nil {
		return fmt.Errorf("delete feed: %w", err)
	}
	return s.tidyFeedFolders(ctx, set, folders)
}

// tidyFeedFolders handles copies retained by merges across different sets.
func (s *podcastSubscriptionService) tidyFeedFolders(ctx context.Context, feedSet *model.Set, folders map[int64]map[string]bool) error {
	var errs []error
	for id, names := range folders {
		set := feedSet
		if set == nil || set.ID != id {
			var err error
			set, err = s.podcastService.store.GetSetByID(ctx, id)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if set != nil {
			errs = append(errs, s.tidyFolders(ctx, set, names))
		}
	}
	return errors.Join(errs...)
}

// episodeMedia loads the media rows of all catalog rows sharing feed's key,
// including trashed ones: GetMediaByID skips those, and a trashed episode
// left behind would keep its folder and could be restored without a feed.
func (s *podcastSubscriptionService) episodeMedia(ctx context.Context, feed *model.PodcastFeed) ([]*model.Media, error) {
	store := s.podcastService.store
	ids, err := store.ListEpisodeMediaIDs(ctx, []int64{feed.ID})
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	wanted := make(map[int64]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	all, err := store.ListMedia(ctx, repository.MediaFilter{IncludeDeleted: true})
	if err != nil {
		return nil, fmt.Errorf("list episode media: %w", err)
	}
	var media []*model.Media
	for i := range all {
		if wanted[all[i].ID] {
			media = append(media, &all[i])
		}
	}
	return media, nil
}

// tidyFolders removes the artwork and then the (empty) directory of each
// folder no remaining feed uses and that holds no other indexed media.
func (s *podcastSubscriptionService) tidyFolders(ctx context.Context, set *model.Set, folders map[string]bool) error {
	store := s.podcastService.store
	feeds, err := store.ListFeedsBySetID(ctx, set.ID)
	if err != nil {
		return fmt.Errorf("list feeds: %w", err)
	}
	used := map[string]bool{}
	for _, f := range feeds {
		used[podcastFolderName("", f.Title, f.ID)] = true
	}
	all, err := store.ListMedia(ctx, repository.MediaFilter{SetID: &set.ID, IncludeDeleted: true})
	if err != nil {
		return fmt.Errorf("list set media: %w", err)
	}
	var errs []error
	for folder := range folders {
		if used[folder] {
			continue
		}
		errs = append(errs, s.tidyFolder(ctx, set, folder, all))
	}
	return errors.Join(errs...)
}

// tidyFolder deletes a folder's feed artwork (rows and files) and the folder
// itself, unless it holds other indexed media. The directory is removed
// non-recursively, so files that were never indexed keep it in place.
func (s *podcastSubscriptionService) tidyFolder(ctx context.Context, set *model.Set, folder string, all []model.Media) error {
	var artwork []*model.Media
	prefix := folder + "/"
	for i := range all {
		m := &all[i]
		if !strings.HasPrefix(m.RelPath, prefix) {
			continue
		}
		if !feedArtwork[strings.TrimPrefix(m.RelPath, prefix)] {
			return nil // uploads or another feed's episodes: keep all of it
		}
		artwork = append(artwork, m)
	}
	var errs []error
	for _, m := range artwork {
		errs = append(errs, s.deleteMediaAndFile(ctx, m))
	}
	dir := filepath.Join(s.podcastService.mediaRoot, set.RootPath, folder)
	for name := range feedArtwork {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", name, err))
		}
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Unindexed files remain; leave the folder rather than delete them.
		s.podcastService.logger.Info("podcast folder kept after unsubscribe", "folder", folder, "err", err)
	}
	return errors.Join(errs...)
}

// deleteMediaAndFile removes the file before the row. On failure the row
// and episode link stay available for a retry. Only files under the media
// root are touched; media-scoped metadata goes with HardDeleteMedia.
func (s *podcastSubscriptionService) deleteMediaAndFile(ctx context.Context, m *model.Media) error {
	if m.AbsPath != "" && s.insideMediaRoot(m.AbsPath) {
		if err := os.Remove(m.AbsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove media file %d: %w", m.ID, err)
		}
		removeOwnThumbnail(s.podcastService.thumbRm, s.podcastService.thumbLayout, m)
	}
	if err := s.podcastService.store.HardDeleteMedia(ctx, m.ID); err != nil {
		return fmt.Errorf("delete media %d: %w", m.ID, err)
	}
	return nil
}

// topFolder returns the first path segment of a set-relative path, or ""
// for files at the set root and for unsafe segments, so the set root itself
// is never selected for tidying.
func topFolder(relPath string) string {
	folder, _, found := strings.Cut(filepath.ToSlash(relPath), "/")
	if !found || folder == "" || folder == "." || folder == ".." {
		return ""
	}
	return folder
}

// insideMediaRoot reports whether path lies below the configured media root.
func (s *podcastSubscriptionService) insideMediaRoot(path string) bool {
	rel, err := filepath.Rel(s.podcastService.mediaRoot, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
