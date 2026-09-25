package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// Feed-owned artwork file names: cover.jpg is downloaded on subscribe and
// .cover.jpg is generated folder artwork. Both belong to the feed's folder.
var feedArtwork = map[string]bool{"cover.jpg": true, ".cover.jpg": true}

// UnsubscribeFeed removes a podcast feed and what it put on disk.
//
// Order matters:
//  1. Collect the feed's downloaded episode media.
//  2. Delete the feed (cascading its episodes). A download still running now
//     fails to link its media (UpdateEpisodeMedia reports sql.ErrNoRows) and
//     removes its own file and row, so it cannot outlive the feed.
//  3. Delete the collected media, row then file.
//  4. Tidy the feed's folders (the current-title folder and any older-title
//     folders its episodes were stored in). Only feed artwork goes, and the
//     folder itself only once it is empty. A folder another remaining feed
//     maps to by title, or that still holds any other indexed media (uploads,
//     another feed's episodes), is left untouched. Folder names come from
//     titles, so this never deletes user files that merely share a name.
//
// Steps 3 and 4 are best-effort: once the feed is gone a retry would 404, so
// they run detached from request cancellation, every item is attempted, and
// all failures are returned joined.
//
// Hard-deleting media also removes users' notes, progress, favorites and
// shares for it. One narrow race remains for a shared folder: a download that
// links its media between steps 1 and 2 keeps its row and file, since the
// store offers no transaction spanning both steps. A download that has
// created but not yet linked its media when step 4 runs makes the folder
// look in use; its own cleanup then tidies the folder (see DownloadEpisode).
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
	if err := store.DeleteFeed(ctx, feedID); err != nil {
		return fmt.Errorf("delete feed: %w", err)
	}
	// Past the point of no return: finish the cleanup even if the client
	// goes away, since a retry would only get 404.
	ctx = context.WithoutCancel(ctx)

	var errs []error
	folders := map[string]bool{podcastFolderName("", feed.Title, feed.ID): true}
	for _, m := range media {
		if folder := topFolder(m.RelPath); folder != "" {
			folders[folder] = true
		}
		errs = append(errs, s.deleteMediaAndFile(ctx, m))
	}
	if set != nil {
		errs = append(errs, s.tidyFolders(ctx, set, folders))
	}
	return errors.Join(errs...)
}

// episodeMedia loads the media rows of feedID's downloaded episodes,
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
	all, err := store.ListMedia(ctx, repository.MediaFilter{SetID: &feed.SetID, IncludeDeleted: true})
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

// deleteMediaAndFile removes a media row, then its file. The row goes first
// so a failure never leaves a listed item whose file is missing; the file is
// removed explicitly because folders are never removed recursively, and a
// leftover file would be re-imported by the next rescan. Only files under the
// media root are touched.
func (s *podcastSubscriptionService) deleteMediaAndFile(ctx context.Context, m *model.Media) error {
	if err := s.podcastService.store.HardDeleteMedia(ctx, m.ID); err != nil {
		return fmt.Errorf("delete media %d: %w", m.ID, err)
	}
	if m.AbsPath == "" || !s.insideMediaRoot(m.AbsPath) {
		return nil
	}
	if err := os.Remove(m.AbsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove media file %d: %w", m.ID, err)
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
