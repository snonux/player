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

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func (s *mediaService) ListSets(ctx context.Context, userID int64) ([]model.Set, error) {
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

func (s *mediaService) GetMediaDetail(ctx context.Context, mediaID, userID int64) (*MediaDetail, error) {
	media, err := s.verifyAccess(ctx, mediaID, userID)
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

func (s *mediaService) ListMedia(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	if user != nil && user.IsAdmin {
		filter.UserID = userID
		return s.store.ListMedia(ctx, filter)
	}

	perms, err := s.store.ListPermissionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	allowed := make([]int64, 0, len(perms))
	for _, p := range perms {
		allowed = append(allowed, p.SetID)
	}
	filter.AllowedSetIDs = allowed
	filter.UserID = userID
	return s.store.ListMedia(ctx, filter)
}

func (s *mediaService) StreamMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	media, err := s.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	return &FileResult{
		Path:     media.AbsPath,
		FileName: media.FileName,
		FileSize: media.FileSizeBytes,
	}, nil
}

func (s *mediaService) DownloadMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	return s.StreamMedia(ctx, mediaID, userID)
}

func (s *mediaService) GetThumbnail(ctx context.Context, mediaID, userID int64) (*FileResult, error) {
	media, err := s.verifyAccess(ctx, mediaID, userID)
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

func (s *mediaService) RegenerateThumbnail(ctx context.Context, mediaID, userID int64) error {
	media, err := s.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	if media.Type != model.MediaTypeVideo {
		return errors.New("thumbnails can only be generated for video files")
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

func (s *mediaService) RegenerateSetCover(ctx context.Context, setID int64, folder string, userID int64) error {
	if err := s.verifySetModifyAccess(ctx, setID, userID); err != nil {
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

func (s *mediaService) GetSetCover(ctx context.Context, setID int64, folder string, userID int64) (*FileResult, error) {
	if err := s.checkSetPermission(ctx, setID, userID, ""); err != nil {
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
	if err != nil {
		return nil, fmt.Errorf("stat cover: %w", err)
	}

	return &FileResult{
		Path:     coverPath,
		FileName: filepath.Base(coverPath),
		FileSize: info.Size(),
	}, nil
}

func (s *mediaService) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	if _, err := s.verifyAccess(ctx, mediaID, userID); err != nil {
		return false, err
	}
	return s.store.ToggleFavorite(ctx, userID, mediaID)
}

func (s *mediaService) AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error {
	if _, err := s.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	tag, err := s.store.GetTagByName(ctx, tagName)
	if err != nil {
		return fmt.Errorf("get tag: %w", err)
	}
	if tag == nil {
		id, err := s.store.CreateTag(ctx, tagName)
		if err != nil {
			return fmt.Errorf("create tag: %w", err)
		}
		tag = &model.Tag{ID: id, Name: tagName}
	}
	return s.store.AssignTag(ctx, mediaID, tag.ID)
}

func (s *mediaService) RemoveTag(ctx context.Context, mediaID, userID int64, tagName string) error {
	if _, err := s.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	tag, err := s.store.GetTagByName(ctx, tagName)
	if err != nil {
		return fmt.Errorf("get tag: %w", err)
	}
	if tag == nil {
		return errors.New("tag not found")
	}
	return s.store.RemoveTag(ctx, mediaID, tag.ID)
}

// BrowseSet returns the immediate subfolders and media files inside
// a specific folder (parent) of a set.
// If a subfolder contains exactly one file and no further subfolders,
// that file is "flattened" and shown at the current level instead of
// presenting the folder.
func (s *mediaService) BrowseSet(ctx context.Context, setID, userID int64, parent string) (*BrowseResult, error) {
	if err := s.checkSetPermission(ctx, setID, userID, ""); err != nil {
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

	type folderContent struct {
		files      []model.Media
		subfolders map[string]struct{}
	}
	folderMap := make(map[string]*folderContent)
	var items []model.Media

	for _, m := range media {
		if m.DeletedAt != nil {
			continue
		}
		rel := filepath.ToSlash(m.RelPath)
		prefix := ""
		if parent != "" {
			prefix = parent + "/"
		}
		if !strings.HasPrefix(rel, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(rel, prefix)
		if suffix == "" {
			continue
		}
		parts := strings.SplitN(suffix, "/", 2)
		name := parts[0]
		if len(parts) == 1 {
			// File at the current level.
			items = append(items, m)
			continue
		}
		// Inside a subfolder — count what is in there.
		fc, ok := folderMap[name]
		if !ok {
			fc = &folderContent{subfolders: make(map[string]struct{})}
			folderMap[name] = fc
		}
		rest := parts[1]
		subparts := strings.SplitN(rest, "/", 2)
		if len(subparts) == 1 {
			fc.files = append(fc.files, m)
		} else {
			fc.subfolders[subparts[0]] = struct{}{}
		}
	}

	var folders []BrowseFolder
	for name, fc := range folderMap {
		total := len(fc.files) + len(fc.subfolders)
		if total == 1 && len(fc.files) == 1 {
			// Flatten: show the lone file at the current level.
			items = append(items, fc.files[0])
		} else {
			subPath := filepath.Join(parent, name)
			coverPath := filepath.Join(filepath.Clean(filepath.Join(s.mediaRoot, set.RootPath, filepath.FromSlash(subPath))), ".cover.jpg")
			_, err := os.Stat(coverPath)
			folders = append(folders, BrowseFolder{Name: name, HasCover: err == nil})
		}
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })

	return &BrowseResult{
		CurrentPath: parent,
		Folders:     folders,
		Media:       items,
	}, nil
}
