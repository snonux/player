package scanner

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/thumb"
)

// thumbMove describes one media row whose generated thumbnail is stored at a
// path the current naming scheme (thumb.ThumbnailPathFor) no longer produces.
type thumbMove struct {
	media  model.Media
	src    string // absolute path of the source media file
	parent string // directory owning the .thumbnails tree of from and to
	from   string // thumbnail path currently stored in the database
	to     string // thumbnail path under the current naming scheme
}

// migrateThumbnails moves thumbnails of already indexed media to the current
// naming scheme and updates media.thumbnail_path accordingly.
//
// Thumbnails used to be named after the source's stem and, for the scanner,
// were all kept flat in the set's .thumbnails directory. "holiday.mp4" and
// "holiday.png", or "a/clip.mp4" and "b/clip.mp4", therefore shared one JPEG
// and every card showed whichever was written last. Stored paths keep
// resolving after an upgrade, so nothing breaks before the first rescan;
// this step then repairs the shared ones:
//
//   - a thumbnail used by exactly one row is renamed, which is cheap and
//     keeps the frame the user may have picked via "regenerate thumbnail";
//   - a thumbnail shared by several rows cannot be attributed to one of
//     them, so each row gets a fresh one generated from its source;
//   - old files nothing references any more are deleted, so the migration
//     leaves no orphans behind.
//
// The whole step is best-effort: a row that fails keeps its old, still
// working path and is retried on the next rescan. Rows whose source file is
// gone are skipped (reconcileOrphans soft-deletes them).
//
// Known limit: sharing is judged from the database alone. If a rescan is
// interrupted after only some sharers of a file were migrated, the one left
// over looks exclusive next time and inherits the shared file by rename. It
// then shows what it showed before the upgrade; "regenerate thumbnail"
// fixes that single item.
func (s *FSScanner) migrateThumbnails(ctx context.Context, existing map[string]model.Media, seenRel map[string]struct{}, setPath, setName string) {
	moves := planThumbMoves(existing, seenRel, setPath)
	if len(moves) == 0 {
		return
	}
	// holders counts the rows pointing at each stored path before the
	// migration; it decides whether a thumbnail is exclusive and must not
	// change while rows move away, or the last of several sharers would
	// look exclusive and inherit the shared file. remaining is the live
	// count used for the final cleanup. claimed holds every destination: a
	// path in it is (or is about to become) another row's current
	// thumbnail, so it is neither renamed away nor deleted, e.g.
	// "holiday.mp4.jpg" being both the old thumbnail of "holiday.mp4.png"
	// and the new one of "holiday.mp4".
	holders := make(map[string]int, len(existing))
	for _, m := range existing {
		if m.ThumbnailPath != "" {
			holders[m.ThumbnailPath]++
		}
	}
	remaining := maps.Clone(holders)
	claimed := make(map[string]struct{}, len(moves))
	for _, mv := range moves {
		claimed[mv.to] = struct{}{}
	}

	var migrated int
	for _, mv := range moves {
		if ctx.Err() != nil {
			break
		}
		_, isClaimed := claimed[mv.from]
		newPath := s.moveThumbnail(ctx, mv, holders[mv.from] == 1 && !isClaimed)
		if newPath == "" {
			continue
		}
		if err := s.store.UpdateMediaThumbnail(ctx, mv.media.ID, newPath); err != nil {
			s.log().Warn("scanner thumbnail migration update failed", "set", setName, "rel_path", mv.media.RelPath, "err", err)
			continue
		}
		remaining[mv.from]--
		migrated++
	}
	s.removeStaleThumbnails(moves, remaining, claimed)
	s.log().Info("scanner migrated thumbnails", "set", setName, "migrated", migrated, "pending", len(moves)-migrated)
}

// planThumbMoves lists the rows of a set whose generated thumbnail is not at
// its current canonical path. Only video and image rows with a source file
// on disk (seenRel) and a thumbnail inside a .thumbnails tree qualify; audio
// covers and images serving as their own thumbnail live elsewhere and are
// left alone. The result is sorted by relPath so the migration is
// deterministic despite the map iteration.
func planThumbMoves(existing map[string]model.Media, seenRel map[string]struct{}, setPath string) []thumbMove {
	var moves []thumbMove
	for relPath, m := range existing {
		if m.Type != model.MediaTypeVideo && m.Type != model.MediaTypeImage {
			continue
		}
		if _, onDisk := seenRel[relPath]; !onDisk {
			continue
		}
		parent, ok := thumb.ParentOf(m.ThumbnailPath)
		if !ok {
			continue
		}
		// Derive the source from relPath: AbsPath may be empty on old rows.
		src := filepath.Join(setPath, filepath.FromSlash(relPath))
		to := thumb.ThumbnailPathFor(src, parent)
		if to == m.ThumbnailPath {
			continue
		}
		moves = append(moves, thumbMove{media: m, src: src, parent: parent, from: m.ThumbnailPath, to: to})
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].media.RelPath < moves[j].media.RelPath })
	return moves
}

// moveThumbnail puts mv's thumbnail at its new path and returns that path,
// or "" when the row must keep its old thumbnail for now. An exclusively
// owned thumbnail is renamed; a shared one, or one whose rename failed (the
// old file may already be gone), is generated afresh from the source.
func (s *FSScanner) moveThumbnail(ctx context.Context, mv thumbMove, exclusive bool) string {
	if exclusive {
		if err := s.fs.MkdirAll(filepath.Dir(mv.to), 0o755); err == nil {
			if err = s.fs.Rename(mv.from, mv.to); err == nil {
				return mv.to
			}
		}
	}
	var (
		newPath string
		err     error
	)
	if mv.media.Type == model.MediaTypeVideo {
		newPath, err = s.thumbMkr.MakeVideo(ctx, mv.src, mv.parent, mv.media.Duration)
	} else {
		newPath, err = s.thumbMkr.MakeImage(ctx, mv.src, mv.parent)
	}
	if err != nil {
		s.log().Warn("scanner thumbnail migration failed", "path", mv.src, "err", err)
		return ""
	}
	return newPath
}

// removeStaleThumbnails deletes the old thumbnail files that no row points
// at any more and that are not some row's new thumbnail. Files that were
// renamed are already gone, which is not an error.
func (s *FSScanner) removeStaleThumbnails(moves []thumbMove, remaining map[string]int, claimed map[string]struct{}) {
	done := make(map[string]struct{}, len(moves))
	for _, mv := range moves {
		if _, seen := done[mv.from]; seen {
			continue
		}
		done[mv.from] = struct{}{}
		if _, isClaimed := claimed[mv.from]; isClaimed || remaining[mv.from] > 0 {
			continue
		}
		if err := s.fs.Remove(mv.from); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log().Warn("scanner stale thumbnail removal failed", "path", mv.from, "err", err)
		}
	}
}
