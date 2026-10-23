package service

import (
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"context"
	"errors"
	"math"
	"unicode/utf8"
)

// ErrInvalidQueue identifies an invalid portable queue request.
var ErrInvalidQueue = errors.New("invalid saved queue")

// SavedQueueStore keeps queue persistence behind the repository boundary.
type SavedQueueStore interface {
	repository.SavedQueueRepo
	repository.AccessHelperStore
}

// PutSavedQueue validates the complete session before its atomic revision write.
func PutSavedQueue(ctx context.Context, store SavedQueueStore, userID, revision int64, q model.SavedQueue) (*model.SavedQueue, bool, error) {
	if err := validateSavedQueue(q, revision); err != nil {
		return nil, false, err
	}
	seen := map[int64]bool{}
	for _, entry := range q.Entries {
		if seen[entry.MediaID] {
			continue
		}
		seen[entry.MediaID] = true
		m, err := NewAccessHelper(store).verifyAccess(ctx, entry.MediaID, userID)
		if err != nil {
			return nil, false, err
		}
		if m.Type != model.MediaTypeAudio {
			return nil, false, ErrInvalidQueue
		}
	}
	if q.Entries == nil {
		q.Entries = []model.QueueOccurrence{}
	}
	if q.Canonical == nil {
		q.Canonical = []int64{}
	}
	if q.UpNext == nil {
		q.UpNext = []int64{}
	}
	return store.PutSavedQueue(ctx, userID, revision, q)
}

func validateSavedQueue(q model.SavedQueue, revision int64) error {
	if (q.ShuffleMode != "" && q.ShuffleMode != "tracks" && q.ShuffleMode != "albums") || revision < 0 || revision >= 9007199254740991 || len(q.Entries) > 10000 || len(q.MutationID) == 0 || len(q.MutationID) > 100 || !utf8.ValidString(q.MutationID) || len(q.Label) > 2000 || !utf8.ValidString(q.Label) || math.IsNaN(q.Position) || math.IsInf(q.Position, 0) || q.Position < 0 || q.Position > 9000000000000 || (q.Repeat != "off" && q.Repeat != "all" && q.Repeat != "one") {
		return ErrInvalidQueue
	}
	ids := map[int64]bool{}
	for _, e := range q.Entries {
		if e.ID <= 0 || e.ID > 9007199254740991 || e.MediaID <= 0 || e.MediaID > 9007199254740991 || ids[e.ID] {
			return ErrInvalidQueue
		}
		ids[e.ID] = true
	}
	if (len(ids) == 0 && (q.CurrentID != 0 || q.Position != 0)) || (len(ids) > 0 && !ids[q.CurrentID]) || len(q.Canonical) != len(ids) || len(q.UpNext) > len(ids) {
		return ErrInvalidQueue
	}
	for _, list := range [][]int64{q.Canonical, q.UpNext} {
		seen := map[int64]bool{}
		for _, id := range list {
			if !ids[id] || seen[id] {
				return ErrInvalidQueue
			}
			seen[id] = true
		}
	}
	return nil
}
