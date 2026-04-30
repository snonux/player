package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/snonux/play/internal/clock"
	"codeberg.org/snonux/play/internal/model"
	"codeberg.org/snonux/play/internal/repository"
)

// mediaService is the concrete implementation of MediaService.
type mediaService struct {
	store     repository.MediaServiceStore
	clock     clock.Clock
	mediaRoot string
}

// NewMediaService creates a concrete MediaService.
func NewMediaService(store repository.MediaServiceStore, clk clock.Clock, mediaRoot string) MediaService {
	return &mediaService{
		store:     store,
		clock:     clk,
		mediaRoot: mediaRoot,
	}
}

// Sentinel errors returned by the media service layer.
var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("access denied")
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
	media, err := s.store.GetMediaByID(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil {
		return nil, nil
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

func (s *mediaService) ListMedia(ctx context.Context, filter repository.MediaFilter) ([]model.Media, error) {
	return s.store.ListMedia(ctx, filter)
}

func (s *mediaService) verifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := s.store.GetMediaByID(ctx, mediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil || media.DeletedAt != nil {
		return nil, ErrNotFound
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return media, nil
	}

	set, err := s.store.GetSetByID(ctx, media.SetID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	for _, p := range set.Permissions {
		if p.UserID == userID {
			return media, nil
		}
	}

	perm, err := s.store.GetPermission(ctx, media.SetID, userID)
	if err != nil {
		return nil, fmt.Errorf("get permission: %w", err)
	}
	if perm != nil {
		return media, nil
	}

	return nil, ErrForbidden
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
	return errors.New("not implemented")
}

func (s *mediaService) RegenerateSetCover(ctx context.Context, setID, userID int64) error {
	return errors.New("not implemented")
}

func (s *mediaService) ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error) {
	return s.store.ToggleFavorite(ctx, userID, mediaID)
}

func (s *mediaService) AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error {
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
	tag, err := s.store.GetTagByName(ctx, tagName)
	if err != nil {
		return fmt.Errorf("get tag: %w", err)
	}
	if tag == nil {
		return errors.New("tag not found")
	}
	return s.store.RemoveTag(ctx, mediaID, tag.ID)
}

func (s *mediaService) SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.SoftDeleteMedia(ctx, mediaID)
}

func (s *mediaService) RestoreMedia(ctx context.Context, mediaID, userID int64) error {
	return s.store.RestoreMedia(ctx, mediaID)
}

func (s *mediaService) uniqueFilename(dir, filename string) string {
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == "" {
		return ""
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}

	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (s *mediaService) UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error) {
	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, errors.New("set not found")
	}

	dir := filepath.Clean(filepath.Join(s.mediaRoot, set.RootPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	path := s.uniqueFilename(dir, filename)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator)) {
		return nil, errors.New("invalid filename")
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, data)
	if err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("write file: %w", err)
	}

	now := s.clock.Now()
	media := &model.Media{
		SetID:         setID,
		RelPath:       filepath.Base(path),
		FileName:      filepath.Base(path),
		AbsPath:       path,
		Type:          guessMediaType(filepath.Base(path)),
		FileSizeBytes: n,
		CreatedAt:     now,
	}

	_ = size

	id, err := s.store.CreateMedia(ctx, media)
	if err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = id
	return media, nil
}

func guessMediaType(name string) model.MediaType {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm":
		return model.MediaTypeVideo
	case ".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma":
		return model.MediaTypeAudio
	default:
		return model.MediaTypeVideo
	}
}

func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *mediaService) CreateShare(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error) {
	_, err := s.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	share := &model.Share{
		Token:     token,
		MediaID:   mediaID,
		CreatedBy: userID,
		CreatedAt: s.clock.Now(),
		ExpiresAt: expiresAt,
	}

	if err := s.store.CreateShare(ctx, share); err != nil {
		return nil, fmt.Errorf("create share: %w", err)
	}
	return share, nil
}

func (s *mediaService) ListShares(ctx context.Context, mediaID, userID int64) ([]model.Share, error) {
	_, err := s.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	return s.store.ListSharesByMedia(ctx, mediaID)
}

func (s *mediaService) RevokeShare(ctx context.Context, token string, userID int64) error {
	share, err := s.store.GetShareByToken(ctx, token)
	if err != nil {
		return fmt.Errorf("get share: %w", err)
	}
	if share == nil {
		return errors.New("share not found")
	}

	_, err = s.verifyAccess(ctx, share.MediaID, userID)
	if err != nil {
		return err
	}

	return s.store.DeleteShare(ctx, token)
}

func (s *mediaService) ValidateShareToken(ctx context.Context, token string) (*model.Share, error) {
	share, err := s.store.GetShareByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("get share: %w", err)
	}
	if share == nil {
		return nil, nil
	}

	now := s.clock.Now()
	if now.After(share.ExpiresAt) {
		return nil, nil
	}

	if share.MaxUses != nil && share.UsedCount >= *share.MaxUses {
		return nil, nil
	}

	return share, nil
}

func (s *mediaService) StreamSharedMedia(ctx context.Context, token string) (*FileResult, error) {
	share, err := s.ValidateShareToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if share == nil {
		return nil, errors.New("invalid or expired share")
	}

	media, err := s.store.GetMediaByID(ctx, share.MediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil {
		return nil, errors.New("media not found")
	}

	_ = s.store.UseShare(ctx, token)

	return &FileResult{
		Path:     media.AbsPath,
		FileName: media.FileName,
		FileSize: media.FileSizeBytes,
	}, nil
}

func (s *mediaService) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	return s.store.GetNote(ctx, mediaID, userID)
}

func (s *mediaService) UpsertNote(ctx context.Context, note *model.Note) error {
	note.UpdatedAt = s.clock.Now()
	return s.store.UpsertNote(ctx, note)
}

func (s *mediaService) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	return s.store.DeleteNote(ctx, mediaID, userID)
}
