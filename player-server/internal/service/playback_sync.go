package service

import (
	"context"
	"math"
	"strings"
	"unicode/utf8"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

type PlaybackSyncStore interface {
	repository.AccessHelperStore
	SyncPlayback(context.Context, int64, int64, model.PlaybackSync) (*model.PlaybackSync, error)
}

func SyncPlayback(ctx context.Context, store PlaybackSyncStore, userID, mediaID int64, incoming model.PlaybackSync) (*model.PlaybackSync, error) {
	m, err := NewAccessHelper(store).verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	valid := func(position float64, ms int64) bool {
		return !math.IsNaN(position) && !math.IsInf(position, 0) && position >= 0 && position <= 9000000000000 && ms > 0 && ms <= 253402300799999
	}
	if len(incoming.MusicPlays) > 100 {
		return nil, ErrInvalidBookmark
	}
	for _, play := range incoming.MusicPlays {
		if m.Type != model.MediaTypeAudio || len(play.ID) != 32 || !valid(play.AccumulatedSeconds, play.ObservedMS) || play.AccumulatedSeconds < 60 || play.AccumulatedSeconds > 72 {
			return nil, ErrInvalidBookmark
		}
		for _, c := range play.ID {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return nil, ErrInvalidBookmark
			}
		}
	}
	if len(incoming.Bookmarks) > 1000 {
		return nil, ErrInvalidBookmark
	}
	if p := incoming.Progress; p != nil && !valid(p.PositionSeconds, p.ObservedMS) {
		return nil, ErrInvalidBookmark
	}
	for i, b := range incoming.Bookmarks {
		if m.Type != model.MediaTypeAudio && m.Type != model.MediaTypeVideo {
			return nil, ErrInvalidBookmark
		}
		if len(b.Key) == 0 || len(b.Key) > 100 || !utf8.ValidString(b.Key) || strings.TrimSpace(b.Key) != b.Key || !valid(b.PositionSeconds, b.ObservedMS) || !utf8.ValidString(b.Label) || utf8.RuneCountInString(b.Label) > MaxBookmarkLabelRunes || (!b.Deleted && m.Duration > 0 && b.PositionSeconds > m.Duration) {
			return nil, ErrInvalidBookmark
		}
		incoming.Bookmarks[i].Label = strings.TrimSpace(b.Label)
	}
	return store.SyncPlayback(ctx, userID, mediaID, incoming)
}
