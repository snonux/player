package service

import (
	"context"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/snonux/player/internal/model"
)

// ErrInvalidShareEdit indicates invalid editable link settings.
var ErrInvalidShareEdit = &apiError{msg: "description must be at most 1000 characters and changed expiry must be in the future", status: http.StatusBadRequest}

// UpdateShare edits a link in place with the same owner/admin policy as revocation.
func (s *shareService) UpdateShare(ctx context.Context, token string, userID int64, description *string, expiresAt *time.Time, audioQuality ...*string) (*model.Share, error) {
	share, err := s.store.GetShareByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("get share: %w", err)
	}
	if share == nil {
		return nil, ErrShareNotFound
	}
	if share.CreatedBy != userID {
		user, err := s.helper.store.GetUserByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("get user: %w", err)
		}
		if user == nil || !user.IsAdmin {
			return nil, ErrForbidden
		}
	}
	var quality *string
	if len(audioQuality) > 0 {
		quality = audioQuality[0]
	}
	if quality != nil {
		kind := model.MediaTypeAudio
		if len(share.MediaIDs) == 0 {
			media, err := s.store.GetMediaByID(ctx, share.MediaID)
			if err != nil {
				return nil, err
			}
			if media == nil {
				return nil, ErrMediaNotFound
			}
			kind = media.Type
		}
		if err := validateShareQuality(*quality, kind); err != nil {
			return nil, err
		}
	}
	if (description == nil && expiresAt == nil && quality == nil) || (description != nil && (!utf8.ValidString(*description) || utf8.RuneCountInString(*description) > 1000)) {
		return nil, ErrInvalidShareEdit
	}
	if expiresAt != nil {
		if expiresAt.Equal(share.ExpiresAt) {
			expiresAt = nil
		} else if !expiresAt.After(s.clock.Now()) {
			return nil, ErrInvalidShareEdit
		}
	}
	updated, err := s.store.UpdateShare(ctx, token, description, expiresAt, quality)
	if err != nil {
		return nil, fmt.Errorf("update share: %w", err)
	}
	if !updated {
		return nil, ErrShareNotFound
	}
	// Reload to include concurrent viewing counts without ever overwriting them.
	share, err = s.store.GetShareByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("reload share: %w", err)
	}
	if share == nil {
		return nil, ErrShareNotFound
	}
	return share, nil
}
