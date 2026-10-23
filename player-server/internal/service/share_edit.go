package service

import (
	"context"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"codeberg.org/snonux/player/internal/model"
)

// ErrInvalidShareEdit indicates invalid editable link settings.
var ErrInvalidShareEdit = &apiError{msg: "description must be at most 1000 characters and changed expiry must be in the future", status: http.StatusBadRequest}

// UpdateShare edits a link in place with the same owner/admin policy as revocation.
func (s *shareService) UpdateShare(ctx context.Context, token string, userID int64, description *string, expiresAt *time.Time) (*model.Share, error) {
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
	if (description == nil && expiresAt == nil) || (description != nil && (!utf8.ValidString(*description) || utf8.RuneCountInString(*description) > 1000)) {
		return nil, ErrInvalidShareEdit
	}
	if expiresAt != nil {
		if expiresAt.Equal(share.ExpiresAt) {
			expiresAt = nil
		} else if !expiresAt.After(s.clock.Now()) {
			return nil, ErrInvalidShareEdit
		}
	}
	updated, err := s.store.UpdateShare(ctx, token, description, expiresAt)
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
