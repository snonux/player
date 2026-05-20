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
	store     repository.WriteServiceStore
	clock     clock.Clock
	mediaRoot string
	thumbGen  thumb.Generator
	prober    probe.Prober
	helper    *accessHelper
}

// NewWriteService creates a WriteService.
func NewWriteService(store repository.WriteServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober, helper *accessHelper) *writeService {
	return &writeService{
		store:     store,
		clock:     clk,
		mediaRoot: mediaRoot,
		thumbGen:  thumbGen,
		prober:    prober,
		helper:    helper,
	}
}

func (s *writeService) SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.helper.verifyModifyAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.SoftDeleteMedia(ctx, mediaID)
}

func (s *writeService) RestoreMedia(ctx context.Context, mediaID, userID int64) error {
	_, err := s.helper.verifyRestoreAccess(ctx, mediaID, userID)
	if err != nil {
		return err
	}
	return s.store.RestoreMedia(ctx, mediaID)
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
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

	if err := ImportMediaFile(ctx, s.store, media, s.prober, s.thumbGen); err != nil {
		removeAndLog(path)
		s.store.HardDeleteMedia(ctx, media.ID)
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
		return fmt.Errorf("probe media: %w", err)
	}

	// Thumbnail destination is derived via internal/thumb so re-generation
	// targets the exact same path used by scanner + import; otherwise stale
	// JPEGs would linger alongside the freshly written one.
	parent := filepath.Dir(media.AbsPath)
	thumbDir := thumb.ThumbnailDir(parent)
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return fmt.Errorf("mkdir thumbnails: %w", err)
	}
	thumbnailPath := thumb.ThumbnailPathFor(media.AbsPath, parent)

	if err := s.thumbGen.Generate(ctx, media.AbsPath, thumbnailPath, meta.Duration); err != nil {
		return fmt.Errorf("generate thumbnail: %w", err)
	}

	media.ThumbnailPath = thumbnailPath
	if err := s.store.UpdateMedia(ctx, media); err != nil {
		return fmt.Errorf("update media: %w", err)
	}
	return nil
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

	media, err := s.store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
	if err != nil {
		return fmt.Errorf("list media: %w", err)
	}

	prefix := filepath.ToSlash(strings.Trim(folder, "/"))
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

	baseDir := filepath.Join(s.mediaRoot, filepath.FromSlash(set.RootPath))
	if prefix != "" {
		baseDir = filepath.Join(baseDir, filepath.FromSlash(prefix))
	}
	coverPath := filepath.Join(filepath.Clean(baseDir), ".cover.jpg")

	switch {
	case len(artworkCandidates) > 0:
		candidate := randomMedia(artworkCandidates)
		if err := s.thumbGen.Generate(ctx, candidate.AbsPath, coverPath, 0); err != nil {
			return fmt.Errorf("generate cover: %w", err)
		}
	case len(videoCandidates) > 0:
		candidate := randomMedia(videoCandidates)
		meta, err := s.prober.Probe(ctx, candidate.AbsPath)
		if err != nil {
			return fmt.Errorf("probe cover candidate: %w", err)
		}
		if err := s.thumbGen.Generate(ctx, candidate.AbsPath, coverPath, meta.Duration); err != nil {
			return fmt.Errorf("generate cover: %w", err)
		}
	case len(imageCandidates) > 0:
		candidate := randomMedia(imageCandidates)
		if err := s.thumbGen.Generate(ctx, candidate.AbsPath, coverPath, 0); err != nil {
			return fmt.Errorf("generate cover: %w", err)
		}
	case len(thumbnailCandidates) > 0:
		candidate := randomMedia(thumbnailCandidates)
		if err := copyFile(candidate.ThumbnailPath, coverPath); err != nil {
			return fmt.Errorf("copy thumbnail cover: %w", err)
		}
	default:
		// Use the sentinel so handleError maps this to HTTP 400 (bad request)
		// instead of falling through to the default 500 branch — the request
		// is well-formed, the set just has nothing usable as a cover.
		return ErrEmptySetForCover
	}

	return nil
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
	defer in.Close()

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
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

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
