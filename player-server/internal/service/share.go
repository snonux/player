package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// shareService handles creation, validation and revocation of share links.
type shareService struct {
	store  repository.ShareServiceStore
	clock  clock.Clock
	helper *accessHelper
}

// NewShareService creates a ShareService.
func NewShareService(store repository.ShareServiceStore, clk clock.Clock, helper *accessHelper) *shareService {
	return &shareService{
		store:  store,
		clock:  clk,
		helper: helper,
	}
}

func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *shareService) CreateShare(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error) {
	_, err := s.helper.verifyAccess(ctx, mediaID, userID)
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

func (s *shareService) ListShares(ctx context.Context, mediaID, userID int64) ([]model.Share, error) {
	_, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	return s.store.ListSharesByMedia(ctx, mediaID)
}

func (s *shareService) RevokeShare(ctx context.Context, token string, userID int64) error {
	share, err := s.store.GetShareByToken(ctx, token)
	if err != nil {
		return fmt.Errorf("get share: %w", err)
	}
	if share == nil {
		// Return the sentinel so handleError maps this to HTTP 404.
		// Returning a plain errors.New here used to fall through to 500.
		return ErrShareNotFound
	}

	_, err = s.helper.verifyAccess(ctx, share.MediaID, userID)
	if err != nil {
		return err
	}

	return s.store.DeleteShare(ctx, token)
}

func (s *shareService) ValidateShareToken(ctx context.Context, token string) (*model.Share, error) {
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

func (s *shareService) StreamSharedMedia(ctx context.Context, token string) (*FileResult, error) {
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
		Duration: media.Duration,
	}, nil
}

func (s *shareService) GetSharedMedia(ctx context.Context, token string) (*GetSharedMediaResult, error) {
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

	hasThumb := media.ThumbnailPath != ""
	thumbURL := ""
	if hasThumb {
		thumbURL = fmt.Sprintf("/s/%s/thumbnail", token)
	}

	return &GetSharedMediaResult{
		Media: &SharedMediaView{
			ID:            media.ID,
			FileName:      media.FileName,
			Type:          media.Type,
			Duration:      media.Duration,
			Codec:         media.Codec,
			Resolution:    media.Resolution,
			Bitrate:       media.Bitrate,
			FileSizeBytes: media.FileSizeBytes,
		},
		HasThumb:    hasThumb,
		StreamURL:   fmt.Sprintf("/s/%s/stream", token),
		DownloadURL: fmt.Sprintf("/s/%s/download", token),
		ThumbURL:    thumbURL,
	}, nil
}

func (s *shareService) GetSharedThumbnail(ctx context.Context, token string) (*FileResult, error) {
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
	if media.ThumbnailPath == "" {
		return nil, ErrMediaNotFound
	}

	return &FileResult{
		Path:     media.ThumbnailPath,
		FileName: filepath.Base(media.ThumbnailPath),
	}, nil
}

func (s *shareService) ListMyShares(ctx context.Context, userID int64) ([]ShareInfo, error) {
	shares, err := s.store.ListSharesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}

	result := make([]ShareInfo, 0, len(shares))
	for _, sh := range shares {
		media, err := s.store.GetMediaByID(ctx, sh.MediaID)
		if err != nil {
			return nil, fmt.Errorf("get media: %w", err)
		}
		fileName := ""
		mediaType := model.MediaTypeVideo
		if media != nil {
			fileName = media.FileName
			mediaType = media.Type
		}
		result = append(result, ShareInfo{
			Token:     sh.Token,
			MediaID:   sh.MediaID,
			FileName:  fileName,
			MediaType: mediaType,
			CreatedAt: sh.CreatedAt,
			ExpiresAt: sh.ExpiresAt,
			MaxUses:   sh.MaxUses,
			UsedCount: sh.UsedCount,
		})
	}
	return result, nil
}
