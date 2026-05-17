package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/model"
)

// podcastEpisodeService manages podcast episode browsing, downloading and completion.
type podcastEpisodeService struct {
	svc *podcastService
}

func newPodcastEpisodeService(svc *podcastService) *podcastEpisodeService {
	return &podcastEpisodeService{svc: svc}
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
func (s *podcastEpisodeService) DownloadEpisode(ctx context.Context, episodeID, userID int64) (*model.Media, error) {
	episode, set, path, err := s.resolveEpisodeAndSet(ctx, episodeID, userID)
	if err != nil {
		return nil, err
	}

	n, err := s.downloadEnclosure(ctx, episode, path)
	if err != nil {
		return nil, err
	}

	media, cleanup, err := s.persistDownloadedEpisode(ctx, episode, set, path, n)
	if err != nil {
		return nil, err
	}

	// Post-persistence failure: link episode to media row.
	if err := s.svc.store.UpdateEpisodeMedia(ctx, episode.ID, media.ID, filepath.Base(path)); err != nil {
		cleanup()
		return nil, fmt.Errorf("update episode media: %w", err)
	}

	return media, nil
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

	ext := filepath.Ext(episode.EpisodeURL)
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
// returns the number of bytes written.
func (s *podcastEpisodeService) downloadEnclosure(ctx context.Context, episode *model.PodcastEpisode, path string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, episode.EpisodeURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build download request: %w", err)
	}
	resp, err := s.svc.httpClient.Do(req)
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
		os.Remove(path)
		return 0, fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return 0, fmt.Errorf("close file: %w", err)
	}

	return n, nil
}

// persistDownloadedEpisode records the downloaded file in the database,
// probes it, and returns a cleanup function to undo work on failure.
func (s *podcastEpisodeService) persistDownloadedEpisode(ctx context.Context, episode *model.PodcastEpisode, set *model.Set, path string, n int64) (*model.Media, func(), error) {
	relPath, err := filepath.Rel(filepath.Join(s.svc.mediaRoot, set.RootPath), path)
	if err != nil {
		os.Remove(path)
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
		os.Remove(path)
		return nil, nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = mediaID

	cleanup := func() {
		os.Remove(path)
		_ = s.svc.store.HardDeleteMedia(ctx, media.ID)
	}

	if err := ImportMediaFile(ctx, s.svc.store, media, s.svc.prober, s.svc.thumbGen); err != nil {
		cleanup()
		return nil, nil, err
	}

	return media, cleanup, nil
}

// ToggleEpisodeComplete flips the completion status of an episode for a user.
func (s *podcastEpisodeService) ToggleEpisodeComplete(ctx context.Context, episodeID, userID int64) error {
	// Verify user has access to the set containing this episode.
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

	if err := s.svc.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
		return err
	}

	// Get current status.
	status, err := s.svc.store.GetEpisodeProgress(ctx, userID, episodeID)
	if err != nil {
		return fmt.Errorf("get episode progress: %w", err)
	}

	isCompleted := true
	if status != nil && status.IsCompleted {
		isCompleted = false
	}

	now := s.svc.clock.Now()
	newStatus := &model.PodcastStatus{
		UserID:      userID,
		EpisodeID:   episodeID,
		IsCompleted: isCompleted,
		UpdatedAt:   now,
	}
	return s.svc.store.UpsertEpisodeProgress(ctx, newStatus)
}
