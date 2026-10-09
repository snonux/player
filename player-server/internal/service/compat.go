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
	// public share token. credentials are the viewing credentials the
	// client presented, if any; a valid one keeps the share usable after
	// max_uses is reached. It never counts a share use; see EnsureShareViewing.
	SharedCompatStream(ctx context.Context, token string, credentials ...string) (*transcode.Rendition, error)
	// EnsureShareViewing returns the viewing a content request runs under:
	// the one a credential stands for, or a newly opened one, which consumes
	// one share use (Opened is set). The caller invokes it when it is
	// certain to deliver content: after it has opened the rendition, and
	// not for HEAD probes. It returns ErrShareExpired when a new viewing
	// is needed and the share has no use left.
	EnsureShareViewing(ctx context.Context, token string, credentials ...string) (ShareViewing, error)
}

// shareRequester is the requester identity of every anonymous share request.
// All share links together get one job budget: tokens are free to create and
// to hand out, so a budget per token would let anonymous requests fill the
// transcode queue and lock out signed-in users.
const shareRequester = "shares"

// RenditionProvider produces (or fetches from cache) the rendition of a
// source file. It is implemented by transcode.Cache; the interface keeps the
// service testable without ffmpeg or a filesystem cache.
type RenditionProvider interface {
	Ensure(ctx context.Context, src transcode.Source) (transcode.Rendition, error)
}

// SharedMediaAccess is the part of the share service the compat stream
// needs: look up what a token shares without counting a use, and settle the
// viewing (counting a use if the client has none) once content is actually
// delivered. It is implemented by shareService.
type SharedMediaAccess interface {
	ResolveSharedMedia(ctx context.Context, token string, credentials ...string) (*model.Media, error)
	EnsureShareViewing(ctx context.Context, token string, credentials ...string) (ShareViewing, error)
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

// SharedCompatStream validates the share token and obtains the rendition.
//
// It deliberately does not count a share use. A use is counted per viewing,
// not per request (see share_viewing.go): a client that presents a valid
// viewing credential is never charged, and one without is charged by the API
// layer only at the moment content is really about to be sent
// (EnsureShareViewing). Counting here would spend a use on answers that
// deliver nothing: "still transcoding", "busy", failures, HEAD probes, and a
// rendition evicted before it could be opened.
func (s *compatStreamService) SharedCompatStream(ctx context.Context, token string, credentials ...string) (*transcode.Rendition, error) {
	media, err := s.shares.ResolveSharedMedia(ctx, token, credentials...)
	if err != nil {
		return nil, err
	}
	return s.rendition(ctx, media, shareRequester)
}

// EnsureShareViewing settles the viewing of a content request to the share
// behind token; see SharedMediaAccess.
func (s *compatStreamService) EnsureShareViewing(ctx context.Context, token string, credentials ...string) (ShareViewing, error) {
	return s.shares.EnsureShareViewing(ctx, token, credentials...)
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
		// A source that is still being written is retried like a pending
		// job: a later request transcodes the settled file.
		return ErrTranscodePending
	case errors.Is(err, transcode.ErrBusy), errors.Is(err, transcode.ErrClosed):
		// During shutdown the client is told to retry; the next instance
		// will take the job.
		return ErrTranscodeBusy
	case errors.Is(err, transcode.ErrNoSpace), errors.Is(err, transcode.ErrTooLarge):
		// Both mean "no room for this rendition"; the client cannot tell
		// (or do anything about) whether the disk or the budget is short.
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
// This is deliberately stricter than /stream, which checks the path
// lexically: a set whose directory is a symlink to a location outside the
// media root plays there but gets 403 here.
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
