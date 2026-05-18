package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// PlaybackHint contains codec/container metadata needed by a client to decide
// whether to play natively or request a transcoded variant. No actual
// transcoding takes place — the hint is derived purely from existing DB fields.
type PlaybackHint struct {
	StreamURL       string  `json:"stream_url"`
	Container       string  `json:"container"`
	VideoCodec      string  `json:"video_codec"`
	AudioCodec      string  `json:"audio_codec"`
	DurationSeconds float64 `json:"duration_seconds"`
	FileSizeBytes   int64   `json:"file_size_bytes"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Bitrate         int     `json:"bitrate"`
	NeedsTranscode  bool    `json:"needs_transcode"`
}

// PlaybackHintsService returns playback hints for a media item.
type PlaybackHintsService interface {
	// GetPlaybackHint returns playback hints for a media item visible to the user.
	GetPlaybackHint(ctx context.Context, mediaID, userID int64) (*PlaybackHint, error)
}

// Compile-time check: playbackHintsService implements PlaybackHintsService.
var _ PlaybackHintsService = (*playbackHintsService)(nil)

// playbackHintsService is the concrete implementation of PlaybackHintsService.
type playbackHintsService struct {
	helper *accessHelper
}

// NewPlaybackHintsService creates a PlaybackHintsService backed by an accessHelper.
func NewPlaybackHintsService(helper *accessHelper) *playbackHintsService {
	return &playbackHintsService{helper: helper}
}

// GetPlaybackHint fetches a media item, verifies access, and assembles hint fields.
// It performs no I/O beyond the DB lookup inside verifyAccess.
func (s *playbackHintsService) GetPlaybackHint(ctx context.Context, mediaID, userID int64) (*PlaybackHint, error) {
	media, err := s.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, fmt.Errorf("verify access: %w", err)
	}

	return buildPlaybackHint(media), nil
}

// buildPlaybackHint assembles a PlaybackHint from Media fields without any I/O.
// It splits the stored codec string into separate video/audio components and
// determines whether the file is likely to require transcoding.
func buildPlaybackHint(media *model.Media) *PlaybackHint {
	streamURL := fmt.Sprintf("/api/v1/media/%d/stream", media.ID)
	container := containerFromPath(media.FileName)
	videoCodec, audioCodec := splitCodecs(media.Codec)

	return &PlaybackHint{
		StreamURL:       streamURL,
		Container:       container,
		VideoCodec:      videoCodec,
		AudioCodec:      audioCodec,
		DurationSeconds: media.Duration,
		FileSizeBytes:   media.FileSizeBytes,
		Width:           media.Width,
		Height:          media.Height,
		Bitrate:         media.Bitrate,
		NeedsTranscode:  needsTranscode(container, videoCodec, audioCodec),
	}
}

// containerFromPath extracts the lowercase file extension (without dot) from a filename.
func containerFromPath(filename string) string {
	ext := strings.TrimPrefix(filepath.Ext(filename), ".")
	return strings.ToLower(ext)
}

// splitCodecs splits a codec string of the form "video/audio" or "codec" into
// separate video and audio components, normalised to lowercase. When only one
// component is present, it is treated as the video codec and audio is left empty.
func splitCodecs(codec string) (videoCodec, audioCodec string) {
	parts := strings.SplitN(codec, "/", 2)
	if len(parts) == 2 {
		return strings.ToLower(strings.TrimSpace(parts[0])), strings.ToLower(strings.TrimSpace(parts[1]))
	}
	return strings.ToLower(strings.TrimSpace(codec)), ""
}

// nativeContainers lists containers that web browsers and common native players
// can play without transcoding.
var nativeContainers = map[string]bool{
	"mp4":  true,
	"webm": true,
	"ogg":  true,
	"mp3":  true,
	"m4a":  true,
	"wav":  true,
	"aac":  true,
	"opus": true,
}

// nativeVideoCodecs lists video codecs that can be played natively by most clients.
// Codecs absent from this map (e.g. wmv, mpeg2, xvid) trigger needsTranscode=true.
var nativeVideoCodecs = map[string]bool{
	"h264":   true,
	"avc":    true, // synonym used by some probers
	"avc1":   true,
	"vp8":    true,
	"vp9":    true,
	"av1":    true,
	"hevc":   true, // supported natively on Apple platforms
	"h265":   true, // synonym for hevc
	"theora": true,
}

// nativeAudioCodecs lists audio codecs that can be played natively by most clients.
// Codecs absent from this map (e.g. flac, ac3, dts) trigger needsTranscode=true.
// Empty string (no audio track) is handled by the if-guard in needsTranscode.
var nativeAudioCodecs = map[string]bool{
	"aac":    true,
	"mp3":    true,
	"opus":   true,
	"vorbis": true,
}

// needsTranscode returns true when the container, video codec, or audio codec
// is unlikely to play natively without transcoding.
// Containers like .mkv and codecs like flac, wmv, mpeg2 are flagged as true.
// Inputs are expected to already be lowercase (as returned by splitCodecs).
// This is a best-effort heuristic — no actual transcoding occurs here.
func needsTranscode(container, videoCodec, audioCodec string) bool {
	if !nativeContainers[container] {
		return true
	}

	// Non-empty video codec that is not in the native list requires transcode.
	if videoCodec != "" && !nativeVideoCodecs[videoCodec] {
		return true
	}

	// Audio codec: flac and other exotic codecs require transcode.
	if audioCodec != "" && !nativeAudioCodecs[audioCodec] {
		return true
	}

	return false
}
