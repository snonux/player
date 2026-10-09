package model

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// NeedsCompatStream reports whether neither the web player nor the Android
// player can decode the original file, so clients must play the server-side
// compatibility rendition instead of the original bytes.
//
// It is the single rule shared by every client. The decision looks at both
// the container (file extension) and the probed codec, so a legacy codec
// inside a modern container (e.g. WMV3 in .mkv) is caught as well. Images are
// never transcoded.
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
// ("video" or "video/audio", as written by the prober) is a Windows Media or
// Flash era codec without decoder support in browsers and ExoPlayer.
func hasLegacyCodec(codec string) bool {
	for _, name := range strings.Split(codec, "/") {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "wmv1", "wmv2", "wmv3", "vc1", "msmpeg4v1", "msmpeg4v2", "msmpeg4v3",
			"flv1", "vp6", "vp6f", "vp6a",
			"wmav1", "wmav2", "wmapro", "wmalossless", "wmavoice":
			return true
		}
	}
	return false
}
