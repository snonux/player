package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/transcode"
)

// CompatStreamService returns a client-compatible rendition (H.264/AAC MP4
// for video, AAC M4A for audio) of media the clients cannot decode natively.
type CompatStreamService interface {
	// CompatStream returns the rendition of a media item for an authorized
	// user. It returns ErrTranscodePending while the rendition is still
	// being produced.
	CompatStream(ctx context.Context, mediaID, userID int64) (*transcode.Rendition, error)
	// SharedCompatStream returns the rendition of the media item behind a
	// public share token.
	SharedCompatStream(ctx context.Context, token string) (*transcode.Rendition, error)
}

// RenditionProvider produces (or fetches from cache) the rendition of a
// source file. It is implemented by transcode.Cache; the interface keeps the
// service testable without ffmpeg or a filesystem cache.
type RenditionProvider interface {
	Ensure(ctx context.Context, src transcode.Source) (transcode.Rendition, error)
}

// Compile-time check: compatStreamService implements CompatStreamService.
var _ CompatStreamService = (*compatStreamService)(nil)

// compatStreamService applies the same access rules as the plain stream
// endpoints and then delegates the transcode to a RenditionProvider.
type compatStreamService struct {
	helper     *accessHelper
	shares     MediaShareService
	renditions RenditionProvider
	mediaRoot  string // sources must stay under this directory when non-empty
}

// NewCompatStreamService creates a CompatStreamService. mediaRoot, when
// non-empty, restricts transcode sources to files below it (the same
// traversal guard the plain streamer applies).
func NewCompatStreamService(helper *accessHelper, shares MediaShareService, renditions RenditionProvider, mediaRoot string) *compatStreamService {
	return &compatStreamService{helper: helper, shares: shares, renditions: renditions, mediaRoot: mediaRoot}
}

// CompatStream verifies the user's access exactly like StreamMedia does and
// returns the rendition.
func (s *compatStreamService) CompatStream(ctx context.Context, mediaID, userID int64) (*transcode.Rendition, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	return s.rendition(ctx, media)
}

// SharedCompatStream validates the share token, produces the rendition, and
// only then records a share use. Counting after the rendition is ready keeps
// the "transcode in progress" retries of a waiting client from burning
// through a share's max_uses budget before any byte was served.
func (s *compatStreamService) SharedCompatStream(ctx context.Context, token string) (*transcode.Rendition, error) {
	share, err := s.shares.ValidateShareToken(ctx, token)
	if err != nil {
		return nil, err
	}
	media, err := s.helper.store.GetMediaByID(ctx, share.MediaID)
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if media == nil {
		return nil, ErrMediaNotFound
	}
	rendition, err := s.rendition(ctx, media)
	if err != nil {
		return nil, err
	}
	// StreamSharedMedia re-validates the token and atomically consumes one
	// use; its file result (the original) is not needed here.
	if _, err := s.shares.StreamSharedMedia(ctx, token); err != nil {
		return nil, err
	}
	return rendition, nil
}

// rendition maps a media row to a transcode source and translates the
// provider's errors into service sentinels the API layer understands.
func (s *compatStreamService) rendition(ctx context.Context, media *model.Media) (*transcode.Rendition, error) {
	kind, err := renditionKind(media.Type)
	if err != nil {
		return nil, err
	}
	if !pathWithinRoot(s.mediaRoot, media.AbsPath) {
		return nil, fmt.Errorf("%w: path escapes media root", ErrForbidden)
	}

	r, err := s.renditions.Ensure(ctx, transcode.Source{MediaID: media.ID, Path: media.AbsPath, Kind: kind})
	switch {
	case err == nil:
		return &r, nil
	case errors.Is(err, transcode.ErrPending):
		return nil, ErrTranscodePending
	case errors.Is(err, transcode.ErrSourceMissing):
		return nil, fmt.Errorf("%w: %w", ErrNotFound, err)
	default:
		return nil, fmt.Errorf("transcode media %d: %w", media.ID, err)
	}
}

// renditionKind selects the rendition profile for a media type. Images have
// no audio/video rendition and are rejected.
func renditionKind(t model.MediaType) (transcode.Kind, error) {
	switch t {
	case model.MediaTypeVideo:
		return transcode.KindVideo, nil
	case model.MediaTypeAudio:
		return transcode.KindAudio, nil
	default:
		return 0, ErrNotTranscodable
	}
}

// pathWithinRoot reports whether path lies below root. An empty root disables
// the check (tests and setups without a media root). It guards against a
// compromised AbsPath in the database being used to read arbitrary files.
func pathWithinRoot(root, path string) bool {
	if root == "" {
		return true
	}
	prefix := filepath.Clean(root) + string(filepath.Separator)
	return strings.HasPrefix(filepath.Clean(path), prefix)
}
