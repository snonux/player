package service

import (
	"context"
	"strconv"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/transcode"
)

// QualityStreamService offers explicitly selected audio encodes with the same
// access and resource limits as compatibility playback.
type QualityStreamService interface {
	QualityStream(context.Context, int64, int64, string) (*transcode.Rendition, error)
}

// QualityStream authorizes a music request before starting a bounded encode.
func (s *compatStreamService) QualityStream(ctx context.Context, mediaID, userID int64, profile string) (*transcode.Rendition, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	kind, ok := transcode.AudioProfileKind(profile)
	if !ok || media.Type != model.MediaTypeAudio {
		return nil, ErrNotTranscodable
	}
	path, err := resolveSourcePath(s.mediaRoot, media.AbsPath)
	if err != nil {
		return nil, err
	}
	r, err := s.renditions.Ensure(ctx, transcode.Source{MediaID: media.ID, Path: path, Kind: kind, Requester: "user:" + strconv.FormatInt(userID, 10)})
	if err != nil {
		return nil, mapRenditionError(media.ID, err)
	}
	return &r, nil
}
