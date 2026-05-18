package service

import (
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

func TestContainerFromPath(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"movie.mp4", "mp4"},
		{"audio.FLAC", "flac"},
		{"video.MKV", "mkv"},
		{"no-ext", ""},
		{"archive.tar.gz", "gz"},
		{"doc.PDF", "pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := containerFromPath(tt.filename)
			if got != tt.want {
				t.Errorf("containerFromPath(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestSplitCodecs(t *testing.T) {
	tests := []struct {
		codec     string
		wantVideo string
		wantAudio string
	}{
		{"h264/aac", "h264", "aac"},
		{"h264 / aac", "h264", "aac"},
		{"h264", "h264", ""},
		{"", "", ""},
		{"vp9/opus", "vp9", "opus"},
	}
	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			v, a := splitCodecs(tt.codec)
			if v != tt.wantVideo || a != tt.wantAudio {
				t.Errorf("splitCodecs(%q) = (%q, %q), want (%q, %q)", tt.codec, v, a, tt.wantVideo, tt.wantAudio)
			}
		})
	}
}

func TestNeedsTranscode(t *testing.T) {
	tests := []struct {
		name       string
		container  string
		videoCodec string
		audioCodec string
		want       bool
	}{
		// mp4 + h264 + aac — fully native
		{"mp4 h264 aac", "mp4", "h264", "aac", false},
		// mkv container — always transcode
		{"mkv", "mkv", "h264", "aac", true},
		// webm + vp9 + opus — native
		{"webm vp9 opus", "webm", "vp9", "opus", false},
		// mp4 + exotic video codec
		{"mp4 wmv", "mp4", "wmv", "aac", true},
		// mp4 + flac audio
		{"mp4 flac", "mp4", "h264", "flac", true},
		// audio-only mp3
		{"mp3 no video", "mp3", "", "mp3", false},
		// unknown container
		{"avi", "avi", "xvid", "mp3", true},
		// m4a audio
		{"m4a", "m4a", "", "aac", false},
		// hevc is listed as native (Apple)
		{"mp4 hevc", "mp4", "hevc", "aac", false},
		// av1 is native
		{"webm av1", "webm", "av1", "opus", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsTranscode(tt.container, tt.videoCodec, tt.audioCodec)
			if got != tt.want {
				t.Errorf("needsTranscode(%q, %q, %q) = %v, want %v",
					tt.container, tt.videoCodec, tt.audioCodec, got, tt.want)
			}
		})
	}
}

func TestBuildPlaybackHint(t *testing.T) {
	media := &model.Media{
		ID:            42,
		FileName:      "sample.mp4",
		Codec:         "h264/aac",
		Duration:      123.5,
		FileSizeBytes: 9876543,
		Width:         1920,
		Height:        1080,
		Bitrate:       4000000,
	}

	hint := buildPlaybackHint(media)

	if hint.StreamURL != "/api/v1/media/42/stream" {
		t.Errorf("unexpected StreamURL: %s", hint.StreamURL)
	}
	if hint.Container != "mp4" {
		t.Errorf("unexpected Container: %s", hint.Container)
	}
	if hint.VideoCodec != "h264" {
		t.Errorf("unexpected VideoCodec: %s", hint.VideoCodec)
	}
	if hint.AudioCodec != "aac" {
		t.Errorf("unexpected AudioCodec: %s", hint.AudioCodec)
	}
	if hint.DurationSeconds != 123.5 {
		t.Errorf("unexpected DurationSeconds: %f", hint.DurationSeconds)
	}
	if hint.FileSizeBytes != 9876543 {
		t.Errorf("unexpected FileSizeBytes: %d", hint.FileSizeBytes)
	}
	if hint.Width != 1920 {
		t.Errorf("unexpected Width: %d", hint.Width)
	}
	if hint.Height != 1080 {
		t.Errorf("unexpected Height: %d", hint.Height)
	}
	if hint.Bitrate != 4000000 {
		t.Errorf("unexpected Bitrate: %d", hint.Bitrate)
	}
	if hint.NeedsTranscode {
		t.Errorf("expected NeedsTranscode=false for mp4/h264/aac")
	}
}

func TestBuildPlaybackHint_MKV(t *testing.T) {
	media := &model.Media{
		ID:       7,
		FileName: "film.mkv",
		Codec:    "h264/ac3",
	}
	hint := buildPlaybackHint(media)
	if !hint.NeedsTranscode {
		t.Errorf("expected NeedsTranscode=true for mkv/ac3")
	}
	if hint.Container != "mkv" {
		t.Errorf("expected container mkv, got %s", hint.Container)
	}
}
