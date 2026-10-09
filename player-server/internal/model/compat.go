package model

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// NeedsCompatStream reports whether at least one of the clients (the web
// player or the Android player) cannot decode the original file, so clients
// must play the server-side compatibility rendition instead of the original
// bytes. Some flagged formats do play on one client (Android's ExoPlayer
// reads AVI and MPEG-4 part 2, for example); the rule is deliberately the
// same for everyone so both clients and the server agree on one URL.
//
// It looks at the container (file extension) and at the probed codecs, so a
// legacy codec inside a modern container (WMV3 in .mkv, AC-3 audio next to
// H.264) is caught as well. Images are never transcoded.
//
// Limits, both caused by deciding from stored metadata without touching the
// file: a file whose extension lies about its container is judged by that
// extension, and rows probed before the audio codec of video files was
// recorded (see probe.codecString) are judged by container and video codec
// only until they are probed again.
func (m Media) NeedsCompatStream() bool {
	if m.Type != MediaTypeVideo && m.Type != MediaTypeAudio {
		return false
	}
	return isLegacyContainer(m.FileName) || hasLegacyCodec(m.Codec)
}

// MarshalJSON adds the derived "transcoded" flag to the stored fields, so
// every API response that contains a media item carries the playback rule
// without each service having to remember to set it.
func (m Media) MarshalJSON() ([]byte, error) {
	// plain has Media's fields but not its methods, which prevents infinite
	// recursion into this MarshalJSON.
	type plain Media
	return json.Marshal(struct {
		plain
		Transcoded bool `json:"transcoded"`
	}{plain: plain(m), Transcoded: m.NeedsCompatStream()})
}

// isLegacyContainer reports whether the file extension names a container that
// browsers cannot demux at all (AVI, ASF/WMV/WMA, FLV).
func isLegacyContainer(fileName string) bool {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".avi", ".wmv", ".wma", ".asf", ".flv":
		return true
	}
	return false
}

// hasLegacyCodec reports whether any codec in the stored codec string
// ("video/audio", "video" or "audio", as written by probe.codecString) lacks
// a decoder in browsers.
func hasLegacyCodec(codec string) bool {
	for _, name := range strings.Split(codec, "/") {
		switch strings.ToLower(strings.TrimSpace(name)) {
		// Windows Media and Flash era video.
		case "wmv1", "wmv2", "wmv3", "vc1", "msmpeg4v1", "msmpeg4v2", "msmpeg4v3",
			"flv1", "vp6", "vp6f", "vp6a":
			return true
		// Pre-H.264 video no browser ships a decoder for (Xvid/DivX,
		// MPEG-1/2, H.263, RealVideo, Sorenson, Theora).
		case "mpeg4", "mpeg1video", "mpeg2video", "h263",
			"rv10", "rv20", "rv30", "rv40", "svq1", "svq3", "theora":
			return true
		// Audio browsers cannot decode: Windows Media, and the cinema
		// codecs (Dolby/DTS) that otherwise leave the video silent.
		case "wmav1", "wmav2", "wmapro", "wmalossless", "wmavoice",
			"ac3", "eac3", "dts", "truehd", "cook":
			return true
		}
	}
	return false
}
