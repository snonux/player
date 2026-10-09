package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
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

// SharedMediaAccess is the part of the share service the compat stream
// needs: look up what a token shares without counting a use, and count a use
// once content is actually delivered. It is implemented by shareService.
type SharedMediaAccess interface {
	ResolveSharedMedia(ctx context.Context, token string) (*model.Media, error)
	ConsumeShareUse(ctx context.Context, token string) error
}

// Compile-time checks.
var (
	_ CompatStreamService = (*compatStreamService)(nil)
	_ SharedMediaAccess   = (*shareService)(nil)
)

// compatStreamService applies the same access rules as the plain stream
// endpoints and then delegates the transcode to a RenditionProvider.
type compatStreamService struct {
	helper     *accessHelper
	shares     SharedMediaAccess
	renditions RenditionProvider
	mediaRoot  string // sources must stay under this directory when non-empty
}

// NewCompatStreamService creates a CompatStreamService. mediaRoot, when
// non-empty, restricts transcode sources to files that really (after
// resolving symlinks) live below it.
func NewCompatStreamService(helper *accessHelper, shares SharedMediaAccess, renditions RenditionProvider, mediaRoot string) *compatStreamService {
	return &compatStreamService{helper: helper, shares: shares, renditions: renditions, mediaRoot: mediaRoot}
}

// CompatStream verifies the user's access exactly like StreamMedia does and
// returns the rendition.
func (s *compatStreamService) CompatStream(ctx context.Context, mediaID, userID int64) (*transcode.Rendition, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	return s.rendition(ctx, media, "user:"+strconv.FormatInt(userID, 10))
}

// SharedCompatStream validates the share token, obtains the rendition, and
// only then records a share use.
//
// Use counting matches /s/{token}/stream: every request that is served
// counts, including each Range request of one playback. The difference is
// what does not count — answers without content ("still transcoding",
// "busy", failures) — so a client polling for a rendition does not burn
// through max_uses before a single byte was delivered.
func (s *compatStreamService) SharedCompatStream(ctx context.Context, token string) (*transcode.Rendition, error) {
	media, err := s.shares.ResolveSharedMedia(ctx, token)
	if err != nil {
		return nil, err
	}
	// The token, not a user, is the requester: an anonymous holder of one
	// share link gets the same job limit as one user.
	rendition, err := s.rendition(ctx, media, "share:"+token)
	if err != nil {
		return nil, err
	}
	if err := s.shares.ConsumeShareUse(ctx, token); err != nil {
		return nil, err
	}
	return rendition, nil
}

// rendition checks that the item is eligible and asks the provider for it.
//
// Only media flagged by the shared rule (model.Media.NeedsCompatStream) is
// transcoded. Everything else plays from the plain stream, and refusing it
// here keeps a user from occupying the transcoder with files that need no
// transcode at all.
func (s *compatStreamService) rendition(ctx context.Context, media *model.Media, requester string) (*transcode.Rendition, error) {
	kind, err := renditionKind(media.Type)
	if err != nil {
		return nil, err
	}
	if !media.NeedsCompatStream() {
		return nil, ErrCompatNotNeeded
	}
	path, err := resolveSourcePath(s.mediaRoot, media.AbsPath)
	if err != nil {
		return nil, err
	}

	r, err := s.renditions.Ensure(ctx, transcode.Source{MediaID: media.ID, Path: path, Kind: kind, Requester: requester})
	if err != nil {
		return nil, mapRenditionError(media.ID, err)
	}
	return &r, nil
}

// mapRenditionError translates provider errors into service sentinels the
// API layer understands. Sentinels are returned bare — never wrapping the
// provider error — because their text reaches anonymous share users and the
// provider's errors contain server file paths. Unexpected errors keep their
// detail for the server log; the API answers those with a generic message.
func mapRenditionError(mediaID int64, err error) error {
	switch {
	case errors.Is(err, transcode.ErrPending), errors.Is(err, transcode.ErrSourceChanged):
		// A changed source is retried like a pending job: the next
		// request transcodes the new version.
		return ErrTranscodePending
	case errors.Is(err, transcode.ErrBusy):
		return ErrTranscodeBusy
	case errors.Is(err, transcode.ErrNoSpace):
		return ErrTranscodeNoSpace
	case errors.Is(err, transcode.ErrSourceMissing):
		return ErrNotFound
	default:
		return fmt.Errorf("transcode media %d: %w", mediaID, err)
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

// resolveSourcePath returns the real location of a media file after checking
// that it lies inside the media root.
//
// Symlinks are resolved first, on both sides: a lexical check alone would
// accept a link inside the media root that points at any file on the server,
// and unlike the plain stream the transcode result is persisted in the cache.
// An empty root disables the check (tests, setups without a media root).
// Errors carry no path because they reach the client.
func resolveSourcePath(root, path string) (string, error) {
	if root == "" {
		return path, nil
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", ErrNotFound
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", ErrNotFound
	}
	if !pathWithinRoot(realRoot, realPath) {
		return "", fmt.Errorf("%w: path escapes media root", ErrForbidden)
	}
	return realPath, nil
}

// pathWithinRoot reports whether path lies below root, lexically. An empty
// root disables the check (tests and setups without a media root). It guards
// against a compromised AbsPath in the database being used to read arbitrary
// files.
func pathWithinRoot(root, path string) bool {
	if root == "" {
		return true
	}
	prefix := filepath.Clean(root) + string(filepath.Separator)
	return strings.HasPrefix(filepath.Clean(path), prefix)
}
