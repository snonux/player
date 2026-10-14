package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/mediatype"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// removeAndLog unlinks path and logs a warning when the unlink fails with
// something other than fs.ErrNotExist. NotExist is fine — the file was already
// cleaned up elsewhere — but permission/I/O errors are real operator signal
// that would otherwise vanish if we kept discarding os.Remove's error.
func removeAndLog(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Default().Warn("cleanup unlink failed", "path", path, "err", err)
	}
}

// writeService handles mutations such as upload, soft-delete and restore.
type writeService struct {
	trashLock *sync.Mutex
	store     repository.WriteServiceStore
	clock     clock.Clock
	mediaRoot string
	// thumbGen renders covers into a temporary file before atomic replacement.
	thumbGen    thumb.Generator
	thumbLayout thumb.Layout
	// thumbs makes and removes media thumbnails; nil without a generator.
	// It keeps them beside the source unless WithThumbnailLayout says
	// otherwise.
	thumbs ThumbnailMaker
	prober probe.Prober
	helper *accessHelper
	logger *slog.Logger
}

// NewWriteService creates a WriteService. Media thumbnails are produced by
// a thumb.FSMaker wrapped around thumbGen, the same implementation the
// scanner uses. The service logs to the default logger; the constructor
// takes none, and adding one would change every caller.
func NewWriteService(store repository.WriteServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober, helper *accessHelper) *writeService {
	logger := slog.Default()
	return &writeService{
		store:     store,
		trashLock: new(sync.Mutex),
		clock:     clk,
		mediaRoot: mediaRoot,
		thumbGen:  thumbGen,
		thumbs:    newThumbnailMaker(thumbGen, logger),
		prober:    prober,
		helper:    helper,
		logger:    logger,
	}
}

// WithThumbnailLayout makes the service create media thumbnails where
// layout says, i.e. in the thumbnail cache when one is configured. It must
// be called before the service is used.
func (s *writeService) WithThumbnailLayout(layout thumb.Layout) *writeService {
	s.thumbLayout = layout
	s.thumbs = newThumbnailMakerIn(s.thumbGen, s.logger, layout)
	return s
}

func (s *writeService) SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error {
	s.trashLock.Lock()
	defer s.trashLock.Unlock()
	media, err := s.helper.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	path := media.AbsPath
	if path == "" {
		set, err := s.store.GetSetByID(ctx, media.SetID)
		if err != nil {
			return fmt.Errorf("get set: %w", err)
		}
		if set == nil {
			return ErrNotFound
		}
		path = filepath.Join(s.mediaRoot, set.RootPath, media.RelPath)
	}
	if !confinedPath(s.mediaRoot, path) {
		return ErrForbidden
	}
	if err := writableMediaDir(s.mediaRoot, filepath.Dir(path)); err != nil {
		return err
	}
	return s.store.SoftDeleteMedia(ctx, mediaID)
}

func (s *writeService) RestoreMedia(ctx context.Context, mediaID, userID int64) error {
	s.trashLock.Lock()
	defer s.trashLock.Unlock()
	media, err := s.helper.verifyRestoreAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	if err := s.checkRestoreSource(ctx, media); err != nil {
		return err
	}
	return s.store.RestoreMedia(ctx, mediaID)
}

// checkRestoreSource refuses to activate a row that could not be played.
// It only reads the source, so recovery still works in a read-only set.
func (s *writeService) checkRestoreSource(ctx context.Context, media *model.Media) error {
	path := media.AbsPath
	if path == "" {
		if media.RelPath == "" {
			return ErrNotFound
		}
		set, err := s.store.GetSetByID(ctx, media.SetID)
		if err != nil {
			return fmt.Errorf("get restore set: %w", err)
		}
		if set == nil {
			return ErrNotFound
		}
		path = filepath.Join(s.mediaRoot, set.RootPath, filepath.FromSlash(media.RelPath))
	}
	if !confinedPath(s.mediaRoot, path) {
		return ErrForbidden
	}
	info, err := os.Stat(path)
	if err != nil {
		return s.restoreSourceError(media.ID, err)
	}
	if !info.Mode().IsRegular() {
		return ErrNotFound
	}
	file, err := os.Open(path)
	if err != nil {
		return s.restoreSourceError(media.ID, err)
	}
	if err := file.Close(); err != nil {
		return s.restoreSourceError(media.ID, err)
	}
	return nil
}

func (s *writeService) restoreSourceError(id int64, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if errors.Is(err, os.ErrPermission) {
		return ErrForbidden
	}
	s.logger.Warn("restore source unavailable", "id", id, "err", err)
	return errors.New("restore source unavailable")
}

func (s *writeService) UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error) {
	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return nil, ErrNotFound
	}

	if err := s.helper.verifySetModifyAccess(ctx, setID, userID); err != nil {
		return nil, err
	}

	if !mediatype.IsSupportedExt(filename) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedExtension, filepath.Ext(filename))
	}

	dir := filepath.Clean(filepath.Join(s.mediaRoot, set.RootPath))
	if !confinedPath(s.mediaRoot, dir) {
		return nil, ErrForbidden
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, mutationError(err)
	}

	if err := writableMediaDir(s.mediaRoot, dir); err != nil {
		return nil, err
	}
	path := uniqueFilename(dir, filename)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator)) {
		return nil, errors.New("invalid filename")
	}

	media, err := s.saveUploadedMedia(ctx, setID, path, data, size)
	if err != nil {
		removeAndLog(path)
		return nil, err
	}

	if err := ImportMediaFile(ctx, s.store, media, s.prober, s.thumbs); err != nil {
		removeAndLog(path)
		if cleanupErr := s.store.HardDeleteMedia(ctx, media.ID); cleanupErr != nil {
			s.logger.Warn("upload row cleanup failed", "id", media.ID, "err", cleanupErr)
		}
		return nil, err
	}

	return media, nil
}

func (s *writeService) RegenerateThumbnail(ctx context.Context, mediaID, userID int64) error {
	media, err := s.helper.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	if media.Type != model.MediaTypeVideo && media.Type != model.MediaTypeImage {
		return errors.New("thumbnails can only be generated for video and image files")
	}

	meta, err := s.prober.Probe(ctx, media.AbsPath)
	if err != nil {
		return probeError(media.AbsPath, err)
	}

	// The thumbnail is made by the ThumbnailMaker, at the one path scanner
	// and import use as well. A row still pointing at a thumbnail written
	// under an older naming scheme therefore moves to its own, current path
	// here; see generateThumbnail for what writing there can overwrite. The
	// maker only reports success for a complete, verified file, so the row
	// is never switched to, and the previous thumbnail never deleted for, a
	// thumbnail that does not exist.
	if s.thumbs == nil {
		return errors.New("thumbnail generation is not configured")
	}
	previous := media.ThumbnailPath
	thumbnailPath, err := s.thumbs.Make(ctx, media.AbsPath, meta.Duration)
	if err != nil {
		return fmt.Errorf("generate thumbnail: %w", err)
	}

	media.ThumbnailPath = thumbnailPath
	if err := s.store.UpdateMedia(ctx, media); err != nil {
		return fmt.Errorf("update media: %w", err)
	}
	s.removeReplacedThumbnail(ctx, media, previous)
	return nil
}

// removeReplacedThumbnail deletes the generated thumbnail file media pointed
// at before RegenerateThumbnail moved it to a different path. Without this
// the old file would be orphaned: the rescan migration only looks at paths
// rows still store.
//
// With a thumbnail cache the previous file is one of three things: the
// row's own cache path, which is the new path too (nothing to do); a file
// in an abandoned cache directory, which is outside every set and stays
// where it is like the rest of that directory; or a thumbnail still beside
// the source because no rescan has moved it yet, which is deleted under the
// rules below, together with its .thumbnails directory once that is empty.
//
// The file is kept unless all of this holds:
//   - it is a generated thumbnail inside the directory of the row's own
//     set, the same confinement the scanner applies. A stale stored path
//     (media root moved, database copied) may point anywhere, also into a
//     .thumbnails directory above the media root;
//   - no other row of the set still points at it. Under the old stem-based
//     naming several rows could share one thumbnail;
//   - it is not the source file of a media item. A media file can live in a
//     directory called .thumbnails and serve as another row's thumbnail.
//
// Failing to clean up is logged, not returned: the regeneration succeeded.
func (s *writeService) removeReplacedThumbnail(ctx context.Context, media *model.Media, previous string) {
	if previous == media.ThumbnailPath || !thumb.IsGenerated(previous) {
		return
	}
	setDir, ok := setDirOf(s.mediaRoot, media)
	if !ok || !pathWithin(setDir, previous) {
		return
	}
	siblings, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &media.SetID, IncludeDeleted: true})
	if err != nil {
		s.logger.Warn("replaced thumbnail kept, cannot list media", "path", previous, "err", err)
		return
	}
	for i := range siblings {
		if siblings[i].ThumbnailPath == previous || siblings[i].AbsPath == previous {
			return
		}
	}
	s.thumbs.Remove(previous)
}

func (s *writeService) RegenerateSetCover(ctx context.Context, setID int64, folder string, userID int64) error {
	if err := s.helper.verifySetModifyAccess(ctx, setID, userID); err != nil {
		return err
	}

	set, err := s.store.GetSetByID(ctx, setID)
	if err != nil {
		return fmt.Errorf("get set: %w", err)
	}
	if set == nil {
		return ErrNotFound
	}

	_, coverPath, err := coverDestination(s.mediaRoot, s.thumbLayout, set.RootPath, folder)
	if err != nil {
		return err
	}
	layout := s.thumbLayout
	media, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return fmt.Errorf("list media: %w", err)
	}
	candidate, copyThumbnail, err := coverCandidate(media, filepath.ToSlash(strings.Trim(folder, "/")))
	if err != nil {
		return err
	}
	source := candidate.AbsPath
	if copyThumbnail {
		source = candidate.ThumbnailPath
	}
	if !confinedPath(s.mediaRoot, source) && !(copyThumbnail && layout.CacheDir() != "" && confinedPath(layout.CacheDir(), source)) {
		return ErrForbidden
	}
	return s.renderCover(ctx, candidate, copyThumbnail, coverPath)
}

func coverCandidate(media []model.Media, prefix string) (model.Media, bool, error) {
	var artworkCandidates []model.Media
	var videoCandidates []model.Media
	var imageCandidates []model.Media
	var thumbnailCandidates []model.Media
	for _, m := range media {
		if m.DeletedAt != nil {
			continue
		}
		rel := filepath.ToSlash(m.RelPath)
		if prefix != "" {
			if !strings.HasPrefix(rel, prefix+"/") {
				continue
			}
			suffix := strings.TrimPrefix(rel, prefix+"/")
			if strings.Contains(suffix, "/") {
				continue
			}
		}
		switch {
		case isFolderArtworkMedia(m):
			artworkCandidates = append(artworkCandidates, m)
		case m.Type == model.MediaTypeVideo:
			videoCandidates = append(videoCandidates, m)
		case m.Type == model.MediaTypeImage:
			imageCandidates = append(imageCandidates, m)
		case m.ThumbnailPath != "":
			thumbnailCandidates = append(thumbnailCandidates, m)
		}
	}

	switch {
	case len(artworkCandidates) > 0:
		return randomMedia(artworkCandidates), false, nil
	case len(videoCandidates) > 0:
		return randomMedia(videoCandidates), false, nil
	case len(imageCandidates) > 0:
		return randomMedia(imageCandidates), false, nil
	case len(thumbnailCandidates) > 0:
		return randomMedia(thumbnailCandidates), true, nil
	default:
		return model.Media{}, false, ErrEmptySetForCover
	}
}

func (s *writeService) renderCover(ctx context.Context, candidate model.Media, copyThumbnail bool, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return mutationError(err)
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".tmp-cover-*.jpg")
	if err != nil {
		return mutationError(err)
	}
	tmp := f.Name()
	defer removeAndLog(tmp)
	if err := errors.Join(f.Chmod(0o644), f.Close()); err != nil {
		return err
	}
	if copyThumbnail {
		err = copyFile(candidate.ThumbnailPath, tmp)
	} else {
		if s.thumbGen == nil {
			return errors.New("cover generation is not configured")
		}
		var duration float64
		if candidate.Type == model.MediaTypeVideo {
			meta, probeErr := s.prober.Probe(ctx, candidate.AbsPath)
			if probeErr != nil {
				return fmt.Errorf("probe cover candidate: %w", probeErr)
			}
			duration = meta.Duration
		}
		err = s.thumbGen.Generate(ctx, candidate.AbsPath, tmp, duration)
	}
	if err != nil {
		return fmt.Errorf("generate cover: %w", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errors.New("generator wrote an empty cover")
	}
	return os.Rename(tmp, dst)
}

func randomMedia(media []model.Media) model.Media {
	if len(media) == 1 {
		return media[0]
	}
	return media[mrand.Intn(len(media))]
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	// Do NOT defer out.Close(): for the destination file we must observe the
	// Close error explicitly. When the OS flushes buffered writes during Close
	// it can surface disk-full, quota, or I/O errors that a deferred close
	// would silently swallow, leaving a truncated/corrupt destination behind.
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		removeAndLog(dst)
		return copyErr
	}
	if closeErr != nil {
		removeAndLog(dst)
		return fmt.Errorf("close %q: %w", dst, closeErr)
	}
	return nil
}

func (s *writeService) saveUploadedMedia(ctx context.Context, setID int64, path string, data io.Reader, size int64) (*model.Media, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, mutationError(err)
	}
	defer func() { _ = f.Close() }()

	n, err := io.Copy(f, data)
	if err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	now := s.clock.Now()
	media := &model.Media{
		SetID:         setID,
		RelPath:       filepath.Base(path),
		FileName:      filepath.Base(path),
		AbsPath:       path,
		Type:          mediatype.TypeForExt(filepath.Base(path)),
		FileSizeBytes: n,
		CreatedAt:     now,
	}
	_ = size

	id, err := s.store.CreateMedia(ctx, media)
	if err != nil {
		return nil, fmt.Errorf("create media: %w", err)
	}
	media.ID = id
	return media, nil
}
