package service

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// browseService handles read-only browsing and media streaming operations.
//
// The thumbnail resolver is injected so the service layer no longer reaches
// into the filesystem itself; tests can swap in a fake Resolver instead of
// writing temporary files.
type browseService struct {
	store          repository.BrowseServiceStore
	clock          clock.Clock
	mediaRoot      string
	helper         *accessHelper
	podcastBrowser PodcastBrowser
	thumbResolver  thumb.Resolver
}

// PodcastBrowser augments a BrowseResult with podcast-specific folders and episodes.
type PodcastBrowser interface {
	AugmentBrowseSet(ctx context.Context, result *BrowseResult, set *model.Set, userID int64, media []model.Media) error
}

// podcastBrowseService handles podcast-specific grid augmentation.
type podcastBrowseService struct {
	store     repository.PodcastRepo
	mediaRoot string
}

// NewPodcastBrowseService creates a PodcastBrowser backed by a PodcastRepo.
func NewPodcastBrowseService(store repository.PodcastRepo, mediaRoot string) *podcastBrowseService {
	return &podcastBrowseService{store: store, mediaRoot: mediaRoot}
}

// AugmentBrowseSet adds undownloaded podcast feed folders or episodes to a BrowseResult.
func (p *podcastBrowseService) AugmentBrowseSet(ctx context.Context, result *BrowseResult, set *model.Set, userID int64, media []model.Media) error {
	if !set.IsPodcast {
		return nil
	}
	feeds, err := p.store.ListFeedsBySetID(ctx, set.ID)
	if err != nil {
		return err
	}
	if result.CurrentPath == "" {
		knownFolders := make(map[string]struct{}, len(result.Folders))
		for _, folder := range result.Folders {
			knownFolders[folder.Name] = struct{}{}
		}
		for _, feed := range feeds {
			name := podcastFolderName("", feed.Title, feed.ID)
			if _, ok := knownFolders[name]; ok {
				continue
			}
			result.Folders = append(result.Folders, BrowseFolder{Name: name, HasCover: folderHasCover(p.mediaRoot, set.RootPath, "", name, media)})
		}
		sort.Slice(result.Folders, func(i, j int) bool { return result.Folders[i].Name < result.Folders[j].Name })
	} else {
		for _, feed := range feeds {
			if result.CurrentPath != podcastFolderName("", feed.Title, feed.ID) {
				continue
			}
			episodes, err := p.store.ListEpisodesWithStatus(ctx, userID, feed.ID, 1000, 0)
			if err == nil {
				result.Episodes = append(result.Episodes, undownloadedEpisodes(episodes)...)
			}
		}
	}
	return nil
}

// NewBrowseService creates a BrowseService with the production filesystem
// thumbnail resolver. Use NewBrowseServiceWithResolver to inject a custom
// Resolver (e.g. a fake in tests).
func NewBrowseService(store repository.BrowseServiceStore, clk clock.Clock, mediaRoot string, helper *accessHelper, browser PodcastBrowser) *browseService {
	return NewBrowseServiceWithResolver(store, clk, mediaRoot, helper, browser, thumb.NewFSResolver())
}

// NewBrowseServiceWithResolver creates a BrowseService with a caller-supplied
// thumbnail Resolver. A nil resolver falls back to the default filesystem
// implementation so existing callers keep working.
func NewBrowseServiceWithResolver(store repository.BrowseServiceStore, clk clock.Clock, mediaRoot string, helper *accessHelper, browser PodcastBrowser, resolver thumb.Resolver) *browseService {
	if resolver == nil {
		resolver = thumb.NewFSResolver()
	}
	return &browseService{
		store:          store,
		clock:          clk,
		mediaRoot:      mediaRoot,
		helper:         helper,
		podcastBrowser: browser,
		thumbResolver:  resolver,
	}
}

func (s *browseService) ListSets(ctx context.Context, userID int64) ([]model.Set, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sets: %w", err)
	}

	if user != nil && user.IsAdmin {
		return sets, nil
	}

	perms, err := s.store.ListPermissionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	allowed := make(map[int64]struct{}, len(perms))
	for _, p := range perms {
		allowed[p.SetID] = struct{}{}
	}

	var filtered []model.Set
	for _, set := range sets {
		if _, ok := allowed[set.ID]; ok {
			filtered = append(filtered, set)
			continue
		}
		for _, p := range set.Permissions {
			if p.UserID == userID {
				filtered = append(filtered, set)
				break
			}
		}
	}

	return filtered, nil
}

func (s *browseService) GetMediaDetail(ctx context.Context, mediaID, userID int64) (*MediaDetail, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}

	tags, err := s.store.ListTagsByMedia(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	fav, err := s.store.IsFavorite(ctx, userID, mediaID)
	if err != nil {
		return nil, fmt.Errorf("check favorite: %w", err)
	}

	note, err := s.store.GetNote(ctx, mediaID, userID)
	if err != nil {
		return nil, fmt.Errorf("get note: %w", err)
	}

	progress, err := s.store.GetProgress(ctx, userID, mediaID)
	if err != nil {
		return nil, fmt.Errorf("get progress: %w", err)
	}

	return &MediaDetail{
		Media:    media,
		Tags:     tags,
		Favorite: fav,
		Note:     note,
		Progress: progress,
	}, nil
}

func (s *browseService) ListMedia(ctx context.Context, userID int64, filter MediaQueryFilter) ([]model.Media, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	repoFilter := repository.MediaFilter{
		SetID:       filter.SetID,
		SetIDs:      filter.SetIDs,
		Type:        filter.Type,
		Search:      filter.Search,
		Tags:        filter.Tags,
		Favorites:   filter.Favorites,
		MinDuration: filter.MinDuration,
		MaxDuration: filter.MaxDuration,
		MinFileSize: filter.MinFileSize,
		MaxFileSize: filter.MaxFileSize,
		Sort:        filter.Sort,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
	}

	if user != nil && user.IsAdmin {
		repoFilter.UserID = userID
		return s.store.ListMedia(ctx, repoFilter)
	}

	perms, err := s.store.ListPermissionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	allowed := make([]int64, 0, len(perms))
	for _, p := range perms {
		allowed = append(allowed, p.SetID)
	}
	if len(allowed) == 0 {
		return []model.Media{}, nil
	}
	repoFilter.AllowedSetIDs = allowed
	repoFilter.UserID = userID
	return s.store.ListMedia(ctx, repoFilter)
}

func (s *browseService) StreamMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	return &FileResult{
		Path:     media.AbsPath,
		FileName: media.FileName,
		FileSize: media.FileSizeBytes,
		Duration: media.Duration,
	}, nil
}

func (s *browseService) DownloadMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	return s.StreamMedia(ctx, mediaID, userID)
}

func (s *browseService) GetThumbnail(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	// Delegate filesystem access to the injected resolver so the service
	// layer stays free of os.Stat. The resolver returns thumb.ErrNotFound
	// for missing thumbnails; map that to the service-level sentinel so
	// handleError renders an HTTP 404 instead of a 500.
	resolved, err := s.thumbResolver.Resolve(media)
	if err != nil {
		if errors.Is(err, thumb.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &FileResult{
		Path:     resolved.Path,
		FileName: resolved.FileName,
		FileSize: resolved.FileSize,
	}, nil
}

// prefixForParent builds the slash-terminated prefix used for matching paths under parent.
func prefixForParent(parent string) string {
	if parent == "" {
		return ""
	}
	return parent + "/"
}

// classifyMediaPath splits a media relPath under a parent prefix into the first path component and remainder.
// It returns name (first component), rest (remaining path), and a bool indicating whether the media is directly inside the parent.
func classifyMediaPath(rel, prefix string) (name string, rest string, isDirect bool) {
	if !strings.HasPrefix(rel, prefix) {
		return "", "", false
	}
	if prefix != "" {
		rel = strings.TrimPrefix(rel, prefix)
	}
	if rel == "" {
		return "", "", false
	}
	parts := strings.SplitN(rel, "/", 2)
	name = parts[0]
	if len(parts) == 1 {
		return name, "", true
	}
	return name, parts[1], false
}

// folderContent collects files and subfolders discovered under a single folder name.
type folderContent struct {
	files      []model.Media
	subfolders map[string]struct{}
}

func isFolderArtworkMedia(media model.Media) bool {
	if media.Type != model.MediaTypeImage {
		return false
	}
	switch strings.ToLower(filepath.Base(media.RelPath)) {
	case "cover.jpg", "cover.jpeg", "cover.png", "cover.gif", "folder.jpg", "folder.jpeg", "folder.png", "folder.gif":
		return true
	default:
		return false
	}
}

func folderContentFiles(files []model.Media) []model.Media {
	content := make([]model.Media, 0, len(files))
	for _, file := range files {
		if isFolderArtworkMedia(file) {
			continue
		}
		content = append(content, file)
	}
	return content
}

// buildFolderMap walks media and groups entries by the first folder component under parent.
func buildFolderMap(media []model.Media, parent string) (map[string]*folderContent, []model.Media) {
	prefix := prefixForParent(parent)
	folderMap := make(map[string]*folderContent)
	var items []model.Media

	for _, m := range media {
		if m.DeletedAt != nil {
			continue
		}
		rel := filepath.ToSlash(m.RelPath)
		name, rest, isDirect := classifyMediaPath(rel, prefix)
		if name == "" {
			continue
		}
		if isDirect {
			items = append(items, m)
			continue
		}
		fc, ok := folderMap[name]
		if !ok {
			fc = &folderContent{subfolders: make(map[string]struct{})}
			folderMap[name] = fc
		}
		subparts := strings.SplitN(rest, "/", 2)
		if len(subparts) == 1 {
			fc.files = append(fc.files, m)
		} else {
			fc.subfolders[subparts[0]] = struct{}{}
		}
	}
	return folderMap, items
}

// folderHasCover determines whether a folder has a cover image on disk or among thumbnails.
func folderHasCover(mediaRoot, setRootPath, parent, name string, media []model.Media) bool {
	subPath := filepath.Join(parent, name)
	folderDir := filepath.Clean(filepath.Join(mediaRoot, setRootPath, filepath.FromSlash(subPath)))
	coverPath := filepath.Join(folderDir, ".cover.jpg")
	_, err := os.Stat(coverPath)
	_, hasDirectCover := folderCoverFile(folderDir)
	return err == nil || hasDirectCover || randomFolderThumbnail(media, filepath.ToSlash(subPath)) != ""
}

// buildFolders converts the folder map into sorted BrowseFolder results, flattening single-file folders.
func buildFolders(folderMap map[string]*folderContent, media []model.Media, items []model.Media, mediaRoot, setRootPath, parent string) ([]BrowseFolder, []model.Media) {
	var folders []BrowseFolder
	for name, fc := range folderMap {
		files := folderContentFiles(fc.files)
		total := len(files) + len(fc.subfolders)
		if total == 0 {
			continue
		}
		if total == 1 && len(files) == 1 {
			items = append(items, files[0])
		} else {
			hasCover := folderHasCover(mediaRoot, setRootPath, parent, name, media)
			folders = append(folders, BrowseFolder{Name: name, HasCover: hasCover})
		}
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
	return folders, items
}

func (s *browseService) BrowseSet(ctx context.Context, setID, userID int64, parent string) (*BrowseResult, error) {
	if err := s.helper.checkSetPermission(ctx, setID, userID, ""); err != nil {
		return nil, err
	}

	parent = filepath.ToSlash(strings.Trim(parent, "/"))
	media, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}

	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	folderMap, items := buildFolderMap(media, parent)
	folders, items := buildFolders(folderMap, media, items, s.mediaRoot, set.RootPath, parent)

	result := &BrowseResult{
		CurrentPath: parent,
		Folders:     folders,
		Media:       items,
	}

	// Delegate podcast-specific grid augmentation to the injected strategy.
	if s.podcastBrowser != nil {
		_ = s.podcastBrowser.AugmentBrowseSet(ctx, result, set, userID, media)
	}

	return result, nil
}

func undownloadedEpisodes(episodes []model.PodcastEpisodeWithStatus) []model.PodcastEpisodeWithStatus {
	undownloaded := make([]model.PodcastEpisodeWithStatus, 0, len(episodes))
	for _, episode := range episodes {
		if episode.IsDownloaded {
			continue
		}
		undownloaded = append(undownloaded, episode)
	}
	return undownloaded
}

func (s *browseService) GetSetCover(ctx context.Context, setID int64, folder string, userID int64) (*FileResult, error) {
	if err := s.helper.checkSetPermission(ctx, setID, userID, ""); err != nil {
		return nil, err
	}

	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	prefix := filepath.ToSlash(strings.Trim(folder, "/"))
	baseDir := filepath.Join(s.mediaRoot, filepath.FromSlash(set.RootPath))
	if prefix != "" {
		baseDir = filepath.Join(baseDir, filepath.FromSlash(prefix))
	}
	coverPath := filepath.Join(filepath.Clean(baseDir), ".cover.jpg")

	info, err := os.Stat(coverPath)
	if err == nil {
		return &FileResult{
			Path:     coverPath,
			FileName: filepath.Base(coverPath),
			FileSize: info.Size(),
		}, nil
	}
	if fr, ok := folderCoverFile(baseDir); ok {
		return fr, nil
	}

	media, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	candidate := randomFolderThumbnail(media, prefix)
	if candidate == "" {
		return nil, ErrNotFound
	}
	info, err = os.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat thumbnail cover: %w", err)
	}
	return &FileResult{
		Path:     candidate,
		FileName: filepath.Base(candidate),
		FileSize: info.Size(),
	}, nil
}

func randomFolderThumbnail(media []model.Media, folder string) string {
	prefix := filepath.ToSlash(strings.Trim(folder, "/"))
	if prefix != "" {
		prefix += "/"
	}
	var candidates []string
	for _, m := range media {
		if m.DeletedAt != nil || m.ThumbnailPath == "" {
			continue
		}
		rel := filepath.ToSlash(m.RelPath)
		if prefix != "" && !strings.HasPrefix(rel, prefix) {
			continue
		}
		candidates = append(candidates, m.ThumbnailPath)
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[mrand.Intn(len(candidates))]
}

func folderCoverFile(dir string) (*FileResult, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(entry.Name()) {
		case "cover.jpg", "cover.jpeg", "cover.png", "cover.gif", "folder.jpg", "folder.jpeg", "folder.png", "folder.gif":
			info, err := entry.Info()
			if err != nil {
				return nil, false
			}
			path := filepath.Join(dir, entry.Name())
			return &FileResult{Path: path, FileName: entry.Name(), FileSize: info.Size()}, true
		}
	}
	return nil, false
}
