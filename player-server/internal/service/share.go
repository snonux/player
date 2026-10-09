package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// shareService handles creation, validation and revocation of share links,
// and the viewings that count their uses (see share_viewing.go).
type shareService struct {
	store  repository.ShareServiceStore
	clock  clock.Clock
	helper *accessHelper

	// keyMu guards key, the viewing-credential signing key cached from the
	// store. The key never changes, so instances that load it separately
	// (app.wireTranscoding builds a second one) agree.
	keyMu sync.Mutex
	key   []byte
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

func (s *shareService) CreateShare(ctx context.Context, userID, mediaID int64, expiresAt time.Time, maxUses *int) (*model.Share, error) {
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
		MaxUses:   maxUses,
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

	if share.CreatedBy != userID {
		user, err := s.helper.store.GetUserByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		if user == nil || !user.IsAdmin {
			return ErrForbidden
		}
	}

	return s.store.DeleteShare(ctx, token)
}

// ValidateShareToken returns the share for a token that a client without a
// viewing may still open: it exists, has not expired and has uses left.
func (s *shareService) ValidateShareToken(ctx context.Context, token string) (*model.Share, error) {
	return s.usableShare(ctx, token)
}

// usableShare returns the share behind token if this request may use it.
//
// The share row is loaded on every request, so a revoked (deleted) share
// answers ErrShareNotFound and an expired one ErrShareExpired at once, also
// for a client inside a viewing. Only the max_uses check depends on the
// credential: a valid one means the use was already paid for.
func (s *shareService) usableShare(ctx context.Context, token string, credentials ...string) (*model.Share, error) {
	share, err := s.store.GetShareByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("get share: %w", err)
	}
	if share == nil {
		return nil, ErrShareNotFound
	}
	if !s.clock.Now().Before(share.ExpiresAt) {
		return nil, ErrShareExpired
	}
	if share.MaxUses == nil || share.UsedCount < *share.MaxUses {
		return share, nil
	}
	_, active, err := s.activeViewing(ctx, token, credentials...)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrShareExpired
	}
	return share, nil
}

// ResolveSharedMedia checks that the request may use the share (see
// usableShare) and returns the media item it shares. It never counts a use.
func (s *shareService) ResolveSharedMedia(ctx context.Context, token string, credentials ...string) (*model.Media, error) {
	share, err := s.usableShare(ctx, token, credentials...)
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
	return media, nil
}

// resolveForContent resolves the shared media and the viewing the request
// runs under. The share is checked first so that a use is only consumed for
// a share whose media exists. A probe opens no viewing; it is told about the
// one its credential stands for, if any.
func (s *shareService) resolveForContent(ctx context.Context, access ShareAccess) (*model.Media, ShareViewing, error) {
	media, err := s.ResolveSharedMedia(ctx, access.Token, access.Credentials...)
	if err != nil {
		return nil, ShareViewing{}, err
	}
	if access.Probe {
		viewing, _, err := s.activeViewing(ctx, access.Token, access.Credentials...)
		return media, viewing, err
	}
	viewing, err := s.EnsureShareViewing(ctx, access.Token, access.Credentials...)
	if err != nil {
		return nil, ShareViewing{}, err
	}
	return media, viewing, nil
}

// StreamSharedMedia returns the shared original file. A request without a
// valid viewing credential opens a viewing (one use); the returned viewing
// then has Opened set and the caller hands its credential to the client.
func (s *shareService) StreamSharedMedia(ctx context.Context, access ShareAccess) (*FileResult, ShareViewing, error) {
	media, viewing, err := s.resolveForContent(ctx, access)
	if err != nil {
		return nil, ShareViewing{}, err
	}
	return &FileResult{
		Path:     media.AbsPath,
		FileName: media.FileName,
		FileSize: media.FileSizeBytes,
		Duration: media.Duration,
	}, viewing, nil
}

// GetSharedMedia returns the metadata behind the share page, its JSON and
// the page's "open viewing" request. Unless the request is a probe it opens
// a viewing; a client that already has one keeps it and is not charged
// again.
func (s *shareService) GetSharedMedia(ctx context.Context, access ShareAccess) (*GetSharedMediaResult, error) {
	media, viewing, err := s.resolveForContent(ctx, access)
	if err != nil {
		return nil, err
	}

	token := access.Token
	hasThumb := media.ThumbnailPath != ""
	thumbURL := ""
	if hasThumb {
		thumbURL = fmt.Sprintf("/s/%s/thumbnail", token)
	}

	// Same shared rule as the authenticated playback hint: legacy formats
	// are played through the compatibility rendition.
	streamURL := fmt.Sprintf("/s/%s/stream", token)
	playbackURL := streamURL
	transcoded := media.NeedsCompatStream()
	if transcoded {
		playbackURL = fmt.Sprintf("/s/%s/compat", token)
	}

	return &GetSharedMediaResult{
		Media:       sharedMediaView(media),
		HasThumb:    hasThumb,
		StreamURL:   streamURL,
		PlaybackURL: playbackURL,
		Transcoded:  transcoded,
		DownloadURL: fmt.Sprintf("/s/%s/download", token),
		ThumbURL:    thumbURL,
		Viewing:     viewing,
	}, nil
}

// sharedMediaView copies the public subset of a media item.
func sharedMediaView(media *model.Media) *SharedMediaView {
	return &SharedMediaView{
		ID:            media.ID,
		FileName:      media.FileName,
		Type:          media.Type,
		Duration:      media.Duration,
		Codec:         media.Codec,
		Resolution:    media.Resolution,
		Bitrate:       media.Bitrate,
		FileSizeBytes: media.FileSizeBytes,
	}
}

// GetSharedThumbnail returns the shared item's thumbnail. It never opens a
// viewing: a thumbnail is not the shared content, and link previews must not
// spend uses. It is served to a client inside a viewing, and — as before
// viewings existed — to anyone while the share still has uses left.
func (s *shareService) GetSharedThumbnail(ctx context.Context, token string, credentials ...string) (*FileResult, error) {
	media, err := s.ResolveSharedMedia(ctx, token, credentials...)
	if err != nil {
		return nil, err
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
