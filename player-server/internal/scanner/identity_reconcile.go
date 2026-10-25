package scanner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// identityLedger collects missing sources and inserts from one Scan for music
// identity reconciliation. Inventory never trashes unmatched missing rows.
type identityLedger struct {
	missing    []model.Media
	createdIDs []int64
	inventory  bool
}

func (l *identityLedger) noteMissing(m model.Media) {
	if m.Type != model.MediaTypeAudio {
		return
	}
	l.missing = append(l.missing, m)
}

func (l *identityLedger) noteCreated(id int64) {
	if id > 0 {
		l.createdIDs = append(l.createdIDs, id)
	}
}

// IdentityConflictStore persists unresolved move matches for admin recovery.
type IdentityConflictStore interface {
	ReplaceMusicIdentityConflicts(ctx context.Context, conflicts []model.MusicIdentityConflict) error
}

// reconcileMusicIdentity merges clear moved/renamed audio within each library
// and records ambiguous conflicts. It is a no-op when the ledger is empty.
func (s *FSScanner) reconcileMusicIdentity(ctx context.Context, ledger *identityLedger, progress *model.ScanProgress) error {
	if ledger == nil || (len(ledger.missing) == 0 && len(ledger.createdIDs) == 0) ||
		(ledger.inventory && len(ledger.createdIDs) == 0) {
		return nil
	}
	sets, err := s.store.ListSets(ctx)
	if err != nil {
		return fmt.Errorf("identity list sets: %w", err)
	}
	libOfSet := map[int64]int64{}
	podcast := map[int64]bool{}
	for _, set := range sets {
		libOfSet[set.ID] = set.LibraryID
		podcast[set.ID] = set.IsPodcast
	}

	createdByLib := map[int64][]model.Media{}
	for _, id := range ledger.createdIDs {
		row, err := s.store.GetMediaByID(ctx, id)
		if err != nil {
			return fmt.Errorf("identity load created %d: %w", id, err)
		}
		if row == nil || row.Type != model.MediaTypeAudio || podcast[row.SetID] {
			continue
		}
		lib := libOfSet[row.SetID]
		createdByLib[lib] = append(createdByLib[lib], *row)
	}

	missingByLib := map[int64][]model.Media{}
	for _, m := range ledger.missing {
		if podcast[m.SetID] {
			continue
		}
		lib := libOfSet[m.SetID]
		missingByLib[lib] = append(missingByLib[lib], m)
	}

	var moved int
	var conflicts []model.MusicIdentityConflict
	now := s.clock.Now()
	for libID, missing := range missingByLib {
		created := createdByLib[libID]
		for _, pair := range PairIdentities(missing, created) {
			if pair.Ambiguous {
				ids := make([]int64, 0, len(pair.Candidates))
				paths := make([]string, 0, len(pair.Candidates))
				for _, c := range pair.Candidates {
					ids = append(ids, c.ID)
					paths = append(paths, c.RelPath)
				}
				raw, _ := json.Marshal(ids)
				conflicts = append(conflicts, model.MusicIdentityConflict{
					LibraryID:         libID,
					MissingMediaID:    pair.Missing.ID,
					CandidateMediaIDs: string(raw),
					MissingPath:       pair.Missing.RelPath,
					CandidatePaths:    paths,
					Reason:            "ambiguous match; kept separate",
					CreatedAt:         now,
				})
				s.log().Info("scanner music identity conflict",
					"library_id", libID, "missing_id", pair.Missing.ID, "missing", pair.Missing.RelPath,
					"candidates", len(pair.Candidates))
				continue
			}
			if ledger.inventory || (pair.New.Music != nil && pair.New.Music.InventoryPending) {
				merger, ok := s.store.(interface {
					MergeInventoryAudio(context.Context, int64, model.Media) (bool, error)
				})
				merged := false
				if ok {
					var err error
					merged, err = merger.MergeInventoryAudio(ctx, pair.Missing.ID, pair.New)
					if err != nil {
						return err
					}
				}
				if !merged {
					raw, _ := json.Marshal([]int64{pair.New.ID})
					conflicts = append(conflicts, model.MusicIdentityConflict{LibraryID: libID, MissingMediaID: pair.Missing.ID, CandidateMediaIDs: string(raw), MissingPath: pair.Missing.RelPath, CandidatePaths: []string{pair.New.RelPath}, Reason: "destination has saved state or cannot be safely merged; kept separate", CreatedAt: now})
					continue
				}
			} else if err := MergeMovedAudio(ctx, s.store, pair.Missing, pair.New); err != nil {
				return err
			}
			moved++
			s.log().Info("scanner music identity preserved",
				"library_id", libID, "id", pair.Missing.ID,
				"from", pair.Missing.RelPath, "to", pair.New.RelPath, "match", pair.Kind)
		}
	}
	if progress != nil {
		progress.SetIdentityStats(moved, len(conflicts))
	}
	if ledger.inventory {
		// Inventory must not replace/clear unrelated unresolved conflicts.
		if store, ok := s.store.(interface {
			RecordMusicIdentityConflicts(context.Context, []model.MusicIdentityConflict) error
		}); ok {
			if err := store.RecordMusicIdentityConflicts(ctx, conflicts); err != nil {
				return fmt.Errorf("record inventory conflicts: %w", err)
			}
		}
	} else if store, ok := s.store.(IdentityConflictStore); ok {
		if err := store.ReplaceMusicIdentityConflicts(ctx, conflicts); err != nil {
			return fmt.Errorf("store identity conflicts: %w", err)
		}
	}
	// Only a completely successful inventory finalizes insertion provenance.
	// Failure/cancellation before this point leaves it durable for a fresh retry.
	if ledger.inventory {
		for _, id := range ledger.createdIDs {
			row, err := s.store.GetMediaByID(ctx, id)
			if err != nil {
				return err
			}
			if row == nil || row.Music == nil || !row.Music.InventoryPending {
				continue
			}
			row.Music.InventoryPending = false
			if err := s.store.UpdateMediaMusic(ctx, id, row.Music, row.FileSizeBytes); err != nil {
				return err
			}
		}
	}
	return nil
}

// MergeMovedAudio keeps keep.ID (and user state), adopts neu's path/metadata,
// and removes the temporary neu row. neu must currently own the destination path.
func MergeMovedAudio(ctx context.Context, store repository.ScannerStore, keep, neu model.Media) error {
	thumb := neu.ThumbnailPath
	if thumb == "" {
		thumb = keep.ThumbnailPath
	}
	if err := store.HardDeleteMedia(ctx, neu.ID); err != nil {
		return fmt.Errorf("identity drop temporary %d: %w", neu.ID, err)
	}
	keep.SetID = neu.SetID
	keep.RelPath = neu.RelPath
	keep.FileName = neu.FileName
	keep.AbsPath = neu.AbsPath
	keep.Type = neu.Type
	keep.Duration = neu.Duration
	keep.Codec = neu.Codec
	keep.Resolution = neu.Resolution
	keep.Bitrate = neu.Bitrate
	keep.FileSizeBytes = neu.FileSizeBytes
	keep.Width = neu.Width
	keep.Height = neu.Height
	keep.ThumbnailPath = thumb
	keep.Music = neu.Music
	keep.DeletedAt = nil
	if err := store.UpdateMedia(ctx, &keep); err != nil {
		return fmt.Errorf("identity update %d → %s: %w", keep.ID, neu.RelPath, err)
	}
	return nil
}
