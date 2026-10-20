package service

import (
	"context"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
)

// AudioFormat is the encoding of an audio item as stored, plus whether
// clients play a compatibility rendition (AAC) instead of the original.
type AudioFormat struct {
	probe.AudioFormat
	Transcoded bool `json:"transcoded"`
}

func (s *browseService) GetAudioFormat(ctx context.Context, mediaID, userID int64) (*AudioFormat, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	return localAudioFormat(ctx, s.mediaRoot, media), nil
}

func (s *shareService) GetSharedAudioFormat(ctx context.Context, token string, credentials ...string) (*AudioFormat, error) {
	media, err := s.ResolveSharedMedia(ctx, token, credentials...)
	if err != nil {
		return nil, err
	}
	return localAudioFormat(ctx, s.lyricsRoot, media), nil
}

// Missing, unreadable, unsafe and non-audio sources all answer an empty
// format: the information is decoration and never blocks playback.
func localAudioFormat(ctx context.Context, root string, media *model.Media) *AudioFormat {
	_, _, source, ok := localAudioSource(ctx, root, media)
	if !ok {
		return &AudioFormat{}
	}
	format, err := probe.ReadAudioFormat(ctx, source)
	if err != nil {
		return &AudioFormat{}
	}
	return &AudioFormat{AudioFormat: format, Transcoded: media.NeedsCompatStream()}
}
