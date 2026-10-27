package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/snonux/player/internal/mediatype"
	"github.com/snonux/player/internal/model"
)

// podcastEpisodeService manages podcast episode browsing, downloading and completion.
type podcastEpisodeService struct {
	svc *podcastService
}

func newPodcastEpisodeService(svc *podcastService) *podcastEpisodeService {
	return &podcastEpisodeService{svc: svc}
}

// removeAndLog unlinks path and logs a warning if the unlink fails with
// anything other than fs.ErrNotExist. NotExist means the file was already
// cleaned up elsewhere, but permission/I/O errors are real signal worth
// surfacing rather than silently dropping.
func (s *podcastEpisodeService) removeAndLog(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.svc.logger.Warn("cleanup unlink failed", "path", path, "err", err)
	}
}

// ListEpisodes returns podcast episodes visible to the user within a set.
func (s *podcastEpisodeService) ListEpisodes(ctx context.Context, setID, userID int64, limit, offset int) ([]model.PodcastEpisodeWithStatus, error) {
	if err := s.svc.helper.checkSetPermission(ctx, setID, userID, ""); err != nil {
		return nil, err
	}

	feeds, err := s.svc.store.ListFeedsBySetID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("list feeds by set: %w", err)
	}
	if len(feeds) == 0 {
		return nil, ErrNotFound
	}

	feedIDs := make([]int64, len(feeds))
	for i, f := range feeds {
		feedIDs[i] = f.ID
	}
	return s.svc.store.ListEpisodesByFeedIDsWithStatus(ctx, userID, feedIDs, limit, offset)
}

// DownloadEpisode downloads the episode enclosure and imports it as media.
//
// The file is written to disk first, then a defer-based cleanup guard is
// armed so that any subsequent failure (DB insert, probe, link update) will
// remove the partially-written file and roll back the database row, leaving
// the filesystem in a clean state. The guard is disarmed on the happy path
// by setting succeeded=true before returning.
func (s *podcastEpisodeService) DownloadEpisode(ctx context.Context, episodeID, userID int64) (*model.Media, error) {
	episode, set, path, err := s.resolveEpisodeAndSet(ctx, episodeID, userID)
	if err != nil {
		return nil, err
	}

	n, err := s.downloadEnclosure(ctx, episode, path)
	if err != nil {
		// downloadEnclosure already cleans up path on failure.
		return nil, err
	}

	// Arm a top-level cleanup guard: if anything after the file write fails,
	// remove the file so no orphaned partial downloads are left on disk.
	succeeded := false
	feedGone := false
	var dbCleanup func()
	defer func() {
		if !succeeded {
			s.undoDownload(ctx, set, path, dbCleanup, feedGone)
		}
	}()

	media, dbCleanup, err := s.persistDownloadedEpisode(ctx, episode, set, path, n)
	if err != nil {
		return nil, err
	}

	// Post-persistence failure: link episode to media row. If the episode
	// vanished (feed unsubscribed mid-download) the deferred cleanup removes
	// the new file and media row, and an emptied feed folder goes too.
	if err := s.svc.store.UpdateEpisodeMedia(ctx, episode.ID, media.ID, filepath.Base(path)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			feedGone = true
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update episode media: %w", err)
	}

	succeeded = true
	return media, nil
}

// undoDownload removes a failed download's file and media row. When the
// feed was unsubscribed meanwhile, the unsubscribe may have skipped the
// folder while the new row existed, so the folder is tidied now (feed
// artwork, then the directory once empty).
func (s *podcastEpisodeService) undoDownload(ctx context.Context, set *model.Set, path string, dbCleanup func(), feedGone bool) {
	// Cleanup must finish even if the request was cancelled.
	ctx = context.WithoutCancel(ctx)
	if dbCleanup != nil {
		dbCleanup()
	} else {
		s.removeAndLog(path)
	}
	if !feedGone {
		return
	}
	folder := topFolder(relPathIn(s.svc.mediaRoot, set, path))
	if folder == "" {
		return
	}
	if err := s.svc.tidyFolders(ctx, set, map[string]bool{folder: true}); err != nil {
		s.svc.logger.Warn("tidy podcast folder after unsubscribe", "folder", folder, "err", err)
	}
}

// resolveEpisodeAndSet fetches the episode, feed, and set, verifies user
// permission, and returns the unique target file path on disk.
func (s *podcastEpisodeService) resolveEpisodeAndSet(ctx context.Context, episodeID, userID int64) (*model.PodcastEpisode, *model.Set, string, error) {
	episode, err := s.svc.store.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get episode: %w", err)
	}
	if episode == nil {
		return nil, nil, "", ErrNotFound
	}

	feed, err := s.svc.store.GetFeedByID(ctx, episode.FeedID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return nil, nil, "", ErrNotFound
	}

	if err := s.svc.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
		return nil, nil, "", err
	}

	set, err := s.svc.store.GetSetByID(ctx, feed.SetID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, nil, "", ErrNotFound
	}

	feedFolder := podcastFolderName("", feed.Title, feed.ID)
	setPath := filepath.Join(s.svc.mediaRoot, set.RootPath, feedFolder)
	path := buildEpisodePath(setPath, episode, s.svc.clock.Now())
	return episode, set, path, nil
}

// buildEpisodePath builds a unique local path for the episode enclosure.
func buildEpisodePath(setPath string, episode *model.PodcastEpisode, now time.Time) string {
	dateStr := now.Format("2006-01-02")
	if episode.PublishedAt != nil {
		dateStr = episode.PublishedAt.Format("2006-01-02")
	}

	// The enclosure's query identifies a publisher resource, not a filename
	// suffix. Including it misclassifies audio copies as video and gives them
	// application/octet-stream when the downloaded media is served back.
	ext := ""
	if enclosure, err := url.Parse(episode.EpisodeURL); err == nil {
		ext = path.Ext(enclosure.Path)
	}
	if ext == "" {
		ext = ".mp3"
	}
	cleanTitle := sanitizeFilename(episode.Title)
	if cleanTitle == "" {
		cleanTitle = fmt.Sprintf("episode-%d", episode.ID)
	}
	filename := fmt.Sprintf("%s - %s%s", dateStr, cleanTitle, ext)
	return uniqueFilename(setPath, filename)
}

// downloadEnclosure performs the HTTP GET, writes the body to path, and
// returns the number of bytes written. The enclosure is fetched with the
// same client as its feed, so the enclosure of a feed a non-admin's
// subscription created cannot reach internal addresses either.
func (s *podcastEpisodeService) downloadEnclosure(ctx context.Context, episode *model.PodcastEpisode, path string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, episode.EpisodeURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build download request: %w", err)
	}
	client := s.svc.restrictedDownloadClient // fail closed when the feed is unknown
	if feed, err := s.svc.store.GetFeedByID(ctx, episode.FeedID); err == nil && feed != nil {
		client = s.svc.enclosureClient(ctx, feed)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download episode: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download episode: status %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir episode folder: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		f.Close()
		s.removeAndLog(path)
		return 0, fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		s.removeAndLog(path)
		return 0, fmt.Errorf("close file: %w", err)
	}

	return n, nil
}

// persistDownloadedEpisode records the downloaded file in the database,
// probes it, and returns a cleanup function to undo work on failure.
func (s *podcastEpisodeService) persistDownloadedEpisode(ctx context.Context, episode *model.PodcastEpisode, set *model.Set, path string, n int64) (*model.Media, func(), error) {
	relPath, err := filepath.Rel(filepath.Join(s.svc.mediaRoot, set.RootPath), path)
	if err != nil {
		s.removeAndLog(path)
		return nil, nil, fmt.Errorf("relative episode path: %w", err)
	}
	relPath = filepath.ToSlash(relPath)
	media := &model.Media{
		SetID:         set.ID,
		RelPath:       relPath,
		FileName:      filepath.Base(path),
		AbsPath:       path,
		Type:          mediatype.TypeForExt(filepath.Base(path)),
		FileSizeBytes: n,
		CreatedAt:     s.svc.clock.Now(),
	}
	mediaID, err := s.svc.store.CreateMedia(ctx, media)
	if err != nil {
		s.removeAndLog(path)
		return nil, nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = mediaID

	cleanup := func() {
		s.removeAndLog(path)
		// A video episode got a thumbnail when it was imported.
		removeOwnThumbnail(s.svc.thumbRm, s.svc.thumbLayout, media)
		// Detached: a cancelled request must not leave a row whose file is gone.
		_ = s.svc.store.HardDeleteMedia(context.WithoutCancel(ctx), media.ID)
	}

	if err := ImportMediaFile(ctx, s.svc.store, media, s.svc.prober, s.svc.thumbs); err != nil {
		cleanup()
		return nil, nil, err
	}

	return media, cleanup, nil
}

// ToggleEpisodeComplete flips the caller's completion register of an
// episode by a manual edit at server time (contract §7.1); the store
// rewrites the progress mirrors of the episode's server copies in the same
// transaction. A flip is not retry-safe (a retried toggle undoes itself),
// so new clients write completion edits through the sync API instead.
func (s *podcastEpisodeService) ToggleEpisodeComplete(ctx context.Context, episodeID, userID int64) error {
	episode, err := s.svc.store.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return fmt.Errorf("get episode: %w", err)
	}
	if episode == nil {
		return ErrNotFound
	}
	feed, err := s.svc.store.GetFeedByID(ctx, episode.FeedID)
	if err != nil {
		return fmt.Errorf("get feed: %w", err)
	}
	if feed == nil {
		return ErrNotFound
	}
	// Only users who may read the episode's set may toggle it.
	if err := s.svc.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
		return err
	}
	if _, err := s.svc.store.ToggleEpisodeCompletion(ctx, userID, episodeID, s.svc.clock.Now()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("toggle episode completion: %w", err)
	}
	return nil
}

// relPathIn returns path relative to the set root, in slash form, or "" when
// it does not lie inside it.
func relPathIn(mediaRoot string, set *model.Set, path string) string {
	rel, err := filepath.Rel(filepath.Join(mediaRoot, set.RootPath), path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}
