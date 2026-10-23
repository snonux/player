package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/scanner"
)

var (
	// ErrIdentityConflictNotFound is returned when the conflict id is unknown or resolved.
	ErrIdentityConflictNotFound = errors.New("identity conflict not found")
	// ErrIdentityConflictBadAction is returned for an unknown resolve action.
	ErrIdentityConflictBadAction = errors.New("invalid identity conflict action")
)

// MusicIdentityStore is the persistence used by music identity recovery.
type MusicIdentityStore interface {
	repository.MusicIdentityRepo
	repository.ScannerStore
}

// MusicIdentityService lists and resolves ambiguous move matches.
type MusicIdentityService struct {
	store MusicIdentityStore
	clock interface{ Now() time.Time }
}

// NewMusicIdentityService wires conflict recovery.
func NewMusicIdentityService(store MusicIdentityStore, clk interface{ Now() time.Time }) *MusicIdentityService {
	return &MusicIdentityService{store: store, clock: clk}
}

// ListConflicts returns unresolved conflicts for Admin.
func (s *MusicIdentityService) ListConflicts(ctx context.Context) ([]model.MusicIdentityConflict, error) {
	return s.store.ListMusicIdentityConflicts(ctx)
}

// Resolve applies keep_separate or merge for one conflict.
// merge requires candidate_media_id of the path to adopt into missing_media_id.
func (s *MusicIdentityService) Resolve(ctx context.Context, conflictID int64, action string, candidateMediaID int64) error {
	c, err := s.store.GetMusicIdentityConflict(ctx, conflictID)
	if err != nil {
		return err
	}
	if c == nil || c.ResolvedAt != nil {
		return ErrIdentityConflictNotFound
	}
	switch action {
	case "keep_separate":
		return s.store.ResolveMusicIdentityConflict(ctx, conflictID, "keep_separate", s.clock.Now())
	case "merge":
		var ids []int64
		if err := json.Unmarshal([]byte(c.CandidateMediaIDs), &ids); err != nil {
			return fmt.Errorf("conflict candidates: %w", err)
		}
		ok := false
		for _, id := range ids {
			if id == candidateMediaID {
				ok = true
				break
			}
		}
		if !ok || candidateMediaID == 0 {
			return fmt.Errorf("%w: candidate_media_id required", ErrIdentityConflictBadAction)
		}
		keep, err := s.store.GetMediaByIDAny(ctx, c.MissingMediaID)
		if err != nil {
			return err
		}
		neu, err := s.store.GetMediaByIDAny(ctx, candidateMediaID)
		if err != nil {
			return err
		}
		if keep == nil || neu == nil {
			return ErrIdentityConflictNotFound
		}
		if err := scanner.MergeMovedAudio(ctx, s.store, *keep, *neu); err != nil {
			return err
		}
		return s.store.ResolveMusicIdentityConflict(ctx, conflictID, fmt.Sprintf("merge:%d", candidateMediaID), s.clock.Now())
	default:
		return ErrIdentityConflictBadAction
	}
}
