package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/transcode"
)

var errShareOriginalOnly = &apiError{msg: "this share permits Original audio only; use its playback link with a compatible player", status: http.StatusBadRequest}

// ErrShareRenditionRequired routes an enforced share encode through the bounded
// rendition pipeline before a viewing is claimed.
var ErrShareRenditionRequired = &apiError{msg: "this share requires its selected audio rendition", status: http.StatusConflict}

// ErrShareEncodingFailed gives guests a safe recovery action for a forced encode.
var ErrShareEncodingFailed = &apiError{msg: "Shared audio could not be converted. Ask the owner to choose another audio quality or check the server encoder.", status: http.StatusInternalServerError}

// ErrInvalidShareQuality rejects unsupported policies without an original fallback.
var ErrInvalidShareQuality = &apiError{msg: "choose Auto, Original, or a supported audio quality profile; explicit quality is for audio only", status: http.StatusBadRequest}

// SharedAudioQuality reloads the live share policy for every content request.
func (s *shareService) SharedAudioQuality(ctx context.Context, access ShareAccess) (string, error) {
	share, err := s.usableShare(ctx, access.Token, access.Credentials...)
	if err != nil {
		return "", err
	}
	return share.AudioQuality, nil
}

func firstShareQuality(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func validateShareQuality(quality string, kind model.MediaType) error {
	if quality == "" {
		return nil
	}
	if kind != model.MediaTypeAudio {
		return ErrInvalidShareQuality
	}
	if quality == "original" {
		return nil
	}
	if _, ok := transcode.AudioProfileKind(quality); !ok {
		return ErrInvalidShareQuality
	}
	return nil
}

func applyShareQuality(result *GetSharedMediaResult, quality string) {
	result.AudioQuality = quality
	if result.Media == nil || result.Media.Type != model.MediaTypeAudio {
		return
	}
	if quality == "original" {
		result.Transcoded = false
		result.PlaybackURL = result.StreamURL
	}
	if quality != "" && quality != "original" {
		result.Transcoded = true
		result.PlaybackURL = strings.TrimSuffix(result.StreamURL, "/stream") + "/compat"
	}
}

func (s *compatStreamService) sharedRendition(ctx context.Context, access ShareAccess) (*transcode.Rendition, error) {
	media, err := s.shares.ResolveSharedItem(ctx, access)
	if err != nil {
		return nil, err
	}
	quality, err := s.shares.SharedAudioQuality(ctx, access)
	if err != nil {
		return nil, err
	}
	if quality == "" {
		return s.rendition(ctx, media, shareRequester)
	}
	if quality == "original" {
		return nil, errShareOriginalOnly
	}
	if err := validateShareQuality(quality, media.Type); err != nil {
		return nil, err
	}
	kind, _ := transcode.AudioProfileKind(quality)
	path, err := resolveSourcePathRoots(s.roots, s.mediaRoot, media.AbsPath)
	if err != nil {
		return nil, err
	}
	rendition, err := s.renditions.Ensure(ctx, transcode.Source{MediaID: media.ID, Path: path, Kind: kind, Requester: shareRequester})
	if err != nil {
		mapped := mapRenditionError(media.ID, err)
		var status interface{ HTTPStatus() int }
		if errors.As(mapped, &status) {
			return nil, mapped
		}
		return nil, fmt.Errorf("%w: %w", ErrShareEncodingFailed, mapped)
	}
	// An encode may wait for a job. Recheck the live policy and creator access
	// before making these bytes available, including for HEAD probes.
	current, err := s.shares.SharedAudioQuality(ctx, access)
	if err != nil {
		return nil, err
	}
	if current != quality {
		return nil, ErrTranscodePending
	}
	if _, err := s.shares.ResolveSharedItem(ctx, access); err != nil {
		return nil, err
	}
	return &rendition, nil
}
