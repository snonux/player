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
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// browseService handles read-only browsing and media streaming operations.
type browseService struct {
	store     repository.BrowseServiceStore
	clock     clock.Clock
	mediaRoot string
	thumbGen  thumb.Generator
	prober    probe.Prober
	helper    *accessHelper
}

// NewBrowseService creates a BrowseService.
func NewBrowseService(store repository.BrowseServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober, helper *accessHelper) MediaBrowseService {
	return &browseService{
		store:     store,
		clock:     clk,
		mediaRoot: mediaRoot,
		thumbGen:  thumbGen,
		prober:    prober,
		helper:    helper,
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
	if media.ThumbnailPath == "" {
		return nil, errors.New("thumbnail not found")
	}
	info, err := os.Stat(media.ThumbnailPath)
	if err != nil {
		return nil, fmt.Errorf("stat thumbnail: %w", err)
	}
	return &FileResult{
		Path:     media.ThumbnailPath,
		FileName: filepath.Base(media.ThumbnailPath),
		FileSize: info.Size(),
	}, nil
}

func (s *browseService) RegenerateThumbnail(ctx context.Context, mediaID, userID int64) error {
	media, err := s.helper.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	if media.Type != model.MediaTypeVideo && media.Type != model.MediaTypeImage {
		return errors.New("thumbnails can only be generated for video and image files")
	}

	meta, err := s.prober.Probe(ctx, media.AbsPath)
	if err != nil {
		return fmt.Errorf("probe media: %w", err)
	}

	thumbDir := filepath.Join(filepath.Dir(media.AbsPath), ".thumbnails")
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return fmt.Errorf("mkdir thumbnails: %w", err)
	}
	thumbName := strings.TrimSuffix(filepath.Base(media.AbsPath), filepath.Ext(media.AbsPath)) + ".jpg"
	thumbnailPath := filepath.Join(thumbDir, thumbName)

	if err := s.thumbGen.Generate(ctx, media.AbsPath, thumbnailPath, meta.Duration); err != nil {
		return fmt.Errorf("generate thumbnail: %w", err)
	}

	media.ThumbnailPath = thumbnailPath
	if err := s.store.UpdateMedia(ctx, media); err != nil {
		return fmt.Errorf("update media: %w", err)
	}
	return nil
}

func (s *browseService) RegenerateSetCover(ctx context.Context, setID int64, folder string, userID int64) error {
	if err := s.helper.verifySetModifyAccess(ctx, setID, userID); err != nil {
		return err
	}

	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return ErrNotFound
	}

	media, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return fmt.Errorf("list media: %w", err)
	}

	prefix := filepath.ToSlash(strings.Trim(folder, "/"))
	var candidates []model.Media
	for _, m := range media {
		if m.DeletedAt != nil {
			continue
		}
		rel := filepath.ToSlash(m.RelPath)
		if prefix != "" {
			if !strings.HasPrefix(rel, prefix+"/") {
				continue
			}
			suffix := strings.TrimPrefix(rel, prefix+"/")
			if strings.Contains(suffix, "/") {
				continue
			}
		} else if strings.Contains(rel, "/") {
			continue
		}
		if m.Type == model.MediaTypeVideo {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return errors.New("no video files available for cover")
	}

	candidate := candidates[0]
	if len(candidates) > 1 {
		candidate = candidates[mrand.Intn(len(candidates))]
	}

	baseDir := filepath.Join(s.mediaRoot, filepath.FromSlash(set.RootPath))
	if prefix != "" {
		baseDir = filepath.Join(baseDir, filepath.FromSlash(prefix))
	}
	coverPath := filepath.Join(filepath.Clean(baseDir), ".cover.jpg")
	meta, err := s.prober.Probe(ctx, candidate.AbsPath)
	if err != nil {
		return fmt.Errorf("probe cover candidate: %w", err)
	}

	if err := s.thumbGen.Generate(ctx, candidate.AbsPath, coverPath, meta.Duration); err != nil {
		return fmt.Errorf("generate cover: %w", err)
	}

	return nil
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
		total := len(fc.files) + len(fc.subfolders)
		if total == 1 && len(fc.files) == 1 {
			items = append(items, fc.files[0])
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

	// For podcast sets, also load episodes (undownloaded items) into the grid.
	if set.IsPodcast {
		feed, err := s.store.GetFeedBySetID(ctx, setID)
		if err == nil && feed != nil {
			episodes, err := s.store.ListEpisodesWithStatus(ctx, userID, feed.ID, 1000, 0)
			if err == nil {
				result.Episodes = episodes
			}
		}
	}

	return result, nil
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
		return nil, fmt.Errorf("stat cover: %w", err)
	}
	info, err = os.Stat(candidate)
	if err != nil {
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
		if prefix == "" && strings.Contains(rel, "/") {
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
