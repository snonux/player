package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// mediaService is the concrete implementation of MediaService.
type mediaService struct {
	store     repository.MediaServiceStore
	clock     clock.Clock
	mediaRoot string
	thumbGen  thumb.Generator
	prober    probe.Prober
}

// NewMediaService creates a concrete MediaService.
func NewMediaService(store repository.MediaServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober) MediaService {
	return &mediaService{
		store:     store,
		clock:     clk,
		mediaRoot: mediaRoot,
		thumbGen:  thumbGen,
		prober:    prober,
	}
}

// Sentinel errors returned by the media service layer.
var (
	ErrNotFound           = errors.New("not found")
	ErrForbidden          = errors.New("access denied")
	ErrShareNotFound      = errors.New("share not found")
	ErrShareExpired       = errors.New("share expired")
	ErrMediaNotFound      = errors.New("media not found")
	ErrUnsupportedExtension = errors.New("unsupported file extension")
)

// supportedExtensions lists all file extensions accepted by UploadMedia.
var supportedExtensions = map[string]struct{}{
	".mp4":  {},
	".mkv":  {},
	".avi":  {},
	".mov":  {},
	".wmv":  {},
	".flv":  {},
	".webm": {},
	".mp3":  {},
	".wav":  {},
	".flac": {},
	".aac":  {},
	".ogg":  {},
	".m4a":  {},
	".wma":  {},
}

func isSupportedExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := supportedExtensions[ext]
	return ok
}

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

// verifyModifyAccess checks that the user has access to the media and is an owner or admin.
func (s *mediaService) verifyModifyAccess(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	media, err := s.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return media, nil
	}

	perm, err := s.store.GetPermission(ctx, media.SetID, userID)
	if err != nil {
		return nil, fmt.Errorf("get permission: %w", err)
	}
	if perm != nil && perm.Role == model.RoleOwner {
		return media, nil
	}

	set, err := s.store.GetSetByID(ctx, media.SetID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set != nil {
		for _, p := range set.Permissions {
			if p.UserID == userID && p.Role == model.RoleOwner {
				return media, nil
			}
		}
	}

	return nil, ErrForbidden
}

// verifySetModifyAccess checks that the user is an owner or admin for a set.
func (s *mediaService) verifySetModifyAccess(ctx context.Context, setID, userID int64) error {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user != nil && user.IsAdmin {
		return nil
	}

	perm, err := s.store.GetPermission(ctx, setID, userID)
	if err != nil {
		return fmt.Errorf("get permission: %w", err)
	}
	if perm != nil && perm.Role == model.RoleOwner {
		return nil
	}

	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}
	if set != nil {
		for _, p := range set.Permissions {
			if p.UserID == userID && p.Role == model.RoleOwner {
				return nil
			}
		}
	}

	return ErrForbidden
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

func (s *mediaService) RegenerateSetCover(ctx context.Context, setID, userID int64) error {
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

	var candidates []model.Media
	for _, m := range media {
		if m.Type == model.MediaTypeVideo && m.DeletedAt == nil {
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

	coverPath := filepath.Join(filepath.Clean(filepath.Join(s.mediaRoot, set.RootPath)), ".cover.jpg")
	meta, err := s.prober.Probe(ctx, candidate.AbsPath)
	if err != nil {
		return fmt.Errorf("probe cover candidate: %w", err)
	}

	if err := s.thumbGen.Generate(ctx, candidate.AbsPath, coverPath, meta.Duration); err != nil {
		return fmt.Errorf("generate cover: %w", err)
	}

	set.CoverThumbnailPath = coverPath
	if err := s.store.UpdateSet(ctx, set); err != nil {
		return fmt.Errorf("update set: %w", err)
	}
	return nil
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

func (s *mediaService) SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.SoftDeleteMedia(ctx, mediaID)
}

func (s *mediaService) RestoreMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
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
		return nil, ErrNotFound
	}

	if err := s.verifySetModifyAccess(ctx, setID, userID); err != nil {
		return nil, err
	}

	if !isSupportedExtension(filename) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedExtension, filepath.Ext(filename))
	}

	dir := filepath.Clean(filepath.Join(s.mediaRoot, set.RootPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	path := s.uniqueFilename(dir, filename)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator)) {
		return nil, errors.New("invalid filename")
	}

	media, err := s.saveUploadedMedia(ctx, setID, path, data, size)
	if err != nil {
		os.Remove(path)
		return nil, err
	}

	meta, err := s.probeMedia(ctx, path)
	if err != nil {
		os.Remove(path)
		s.store.HardDeleteMedia(ctx, media.ID)
		return nil, err
	}
	media.Duration = meta.Duration
	media.Codec = meta.Codec
	media.Resolution = meta.Resolution
	media.Bitrate = meta.Bitrate

	if media.Type == model.MediaTypeVideo {
		if err := s.generateThumbnail(ctx, media, meta.Duration); err != nil {
			os.Remove(path)
			_ = s.store.HardDeleteMedia(ctx, media.ID)
			return nil, err
		}
	}

	if err := s.store.UpdateMedia(ctx, media); err != nil {
		os.Remove(path)
		_ = s.store.HardDeleteMedia(ctx, media.ID)
		return nil, fmt.Errorf("update media metadata: %w", err)
	}

	return media, nil
}

// saveUploadedMedia writes data to disk and creates a minimal media row.
func (s *mediaService) saveUploadedMedia(ctx context.Context, setID int64, path string, data io.Reader, size int64) (*model.Media, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, data)
	if err != nil {
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
		return nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = id
	return media, nil
}

// probeMedia extracts metadata from the uploaded file.
func (s *mediaService) probeMedia(ctx context.Context, path string) (*model.Metadata, error) {
	if s.prober == nil {
		return &model.Metadata{}, nil
	}
	meta, err := s.prober.Probe(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("probe media: %w", err)
	}
	return meta, nil
}

// generateThumbnail creates a thumbnail for a video file.
func (s *mediaService) generateThumbnail(ctx context.Context, media *model.Media, duration float64) error {
	thumbDir := filepath.Join(filepath.Dir(media.AbsPath), ".thumbnails")
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return fmt.Errorf("mkdir thumbnails: %w", err)
	}
	thumbName := strings.TrimSuffix(filepath.Base(media.AbsPath), filepath.Ext(media.AbsPath)) + ".jpg"
	thumbnailPath := filepath.Join(thumbDir, thumbName)

	if s.thumbGen == nil {
		media.ThumbnailPath = thumbnailPath
		return nil
	}
	if err := s.thumbGen.Generate(ctx, media.AbsPath, thumbnailPath, duration); err != nil {
		return fmt.Errorf("generate thumbnail: %w", err)
	}
	media.ThumbnailPath = thumbnailPath
	return nil
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
		return nil, ErrShareNotFound
	}

	now := s.clock.Now()
	if now.After(share.ExpiresAt) {
		return nil, ErrShareExpired
	}

	if share.MaxUses != nil && share.UsedCount >= *share.MaxUses {
		return nil, ErrShareExpired
	}

	return share, nil
}

func (s *mediaService) StreamSharedMedia(ctx context.Context, token string) (*FileResult, error) {
	share, err := s.ValidateShareToken(ctx, token)
	if err != nil {
		return nil, err
	}

	media, err := s.store.GetMediaByID(ctx, share.MediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil {
		return nil, ErrMediaNotFound
	}

	_ = s.store.UseShare(ctx, token)

	return &FileResult{
		Path:     media.AbsPath,
		FileName: media.FileName,
		FileSize: media.FileSizeBytes,
	}, nil
}

func (s *mediaService) GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
	if _, err := s.verifyAccess(ctx, mediaID, userID); err != nil {
		return nil, err
	}
	return s.store.GetNote(ctx, mediaID, userID)
}

func (s *mediaService) UpsertNote(ctx context.Context, note *model.Note) error {
	if _, err := s.verifyAccess(ctx, note.MediaID, note.UserID); err != nil {
		return err
	}
	note.UpdatedAt = s.clock.Now()
	return s.store.UpsertNote(ctx, note)
}

func (s *mediaService) DeleteNote(ctx context.Context, mediaID, userID int64) error {
	if _, err := s.verifyAccess(ctx, mediaID, userID); err != nil {
		return err
	}
	return s.store.DeleteNote(ctx, mediaID, userID)
}
