package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// MaxBookmarkLabelRunes is the maximum number of characters in a bookmark name.
const MaxBookmarkLabelRunes = 200

var ErrInvalidBookmark = &apiError{msg: "invalid bookmark", status: http.StatusBadRequest}

type bookmarkService struct {
	store  repository.BookmarkRepo
	clock  clock.Clock
	helper *accessHelper
}

func NewBookmarkService(store repository.BookmarkRepo, clk clock.Clock, helper *accessHelper) *bookmarkService {
	return &bookmarkService{store: store, clock: clk, helper: helper}
}

func (s *bookmarkService) media(ctx context.Context, mediaID, userID int64) (*model.Media, error) {
	m, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	if m.Type != model.MediaTypeAudio && m.Type != model.MediaTypeVideo {
		return nil, fmt.Errorf("%w: audio or video required", ErrInvalidBookmark)
	}
	return m, nil
}

func (s *bookmarkService) ListBookmarks(ctx context.Context, mediaID, userID int64) ([]model.Bookmark, error) {
	if _, err := s.media(ctx, mediaID, userID); err != nil {
		return nil, err
	}
	return s.store.ListBookmarks(ctx, mediaID, userID)
}

func (s *bookmarkService) CreateBookmark(ctx context.Context, mediaID, userID int64, position float64, label string) (*model.Bookmark, error) {
	m, err := s.media(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 || (m.Duration > 0 && position > m.Duration) {
		return nil, fmt.Errorf("%w: position must be finite, nonnegative and within the media duration", ErrInvalidBookmark)
	}
	label = strings.TrimSpace(label)
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > MaxBookmarkLabelRunes {
		return nil, fmt.Errorf("%w: label must be at most %d characters", ErrInvalidBookmark, MaxBookmarkLabelRunes)
	}
	b := &model.Bookmark{MediaID: mediaID, UserID: userID, PositionSeconds: position, Label: label, CreatedAt: s.clock.Now().UTC()}
	b.ID, err = s.store.CreateBookmark(ctx, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *bookmarkService) DeleteBookmark(ctx context.Context, id, mediaID, userID int64) error {
	if _, err := s.media(ctx, mediaID, userID); err != nil {
		return err
	}
	var deleted bool
	var err error
	if timed, ok := s.store.(interface {
		DeleteBookmarkAt(context.Context, int64, int64, int64, time.Time) (bool, error)
	}); ok {
		deleted, err = timed.DeleteBookmarkAt(ctx, id, mediaID, userID, s.clock.Now())
	} else {
		deleted, err = s.store.DeleteBookmark(ctx, id, mediaID, userID)
	}
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}
