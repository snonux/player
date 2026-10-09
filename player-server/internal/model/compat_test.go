package model

import (
	"encoding/json"
	"testing"
)

// compatCase is one media row and whether it needs the compat stream.
type compatCase struct {
	name  string
	media Media
	want  bool
}

func compatCases() []compatCase {
	return []compatCase{
		// Legacy containers, whatever the probed codec says.
		{"avi mpeg4", Media{Type: MediaTypeVideo, FileName: "a.avi", Codec: "mpeg4"}, true},
		{"avi h264", Media{Type: MediaTypeVideo, FileName: "a.AVI", Codec: "h264"}, true},
		{"wmv", Media{Type: MediaTypeVideo, FileName: "a.wmv", Codec: "wmv3"}, true},
		{"flv", Media{Type: MediaTypeVideo, FileName: "a.flv", Codec: "flv1"}, true},
		{"wma", Media{Type: MediaTypeAudio, FileName: "a.wma", Codec: "wmav2"}, true},
		{"wma unprobed", Media{Type: MediaTypeAudio, FileName: "a.wma"}, true},
		{"asf", Media{Type: MediaTypeVideo, FileName: "a.asf", Codec: "wmv2/wmav2"}, true},
		// Legacy codec inside a container clients can demux. The codec
		// strings are the ones probe.codecString writes: "video/audio" for
		// video files, or the video codec alone for rows probed before the
		// audio codec was recorded.
		{"mkv with wmv3", Media{Type: MediaTypeVideo, FileName: "a.mkv", Codec: "WMV3"}, true},
		{"mp4 with wma audio", Media{Type: MediaTypeVideo, FileName: "a.mp4", Codec: "h264/wmav2"}, true},
		{"mkv with ac3 audio", Media{Type: MediaTypeVideo, FileName: "a.mkv", Codec: "h264/ac3"}, true},
		{"mkv with dts audio", Media{Type: MediaTypeVideo, FileName: "a.mkv", Codec: "hevc/dts"}, true},
		{"xvid in mkv", Media{Type: MediaTypeVideo, FileName: "a.mkv", Codec: "mpeg4/mp3"}, true},
		{"mpeg2 in mp4", Media{Type: MediaTypeVideo, FileName: "a.mp4", Codec: "mpeg2video"}, true},
		{"audio row with cover art", Media{Type: MediaTypeAudio, FileName: "a.m4a", Codec: "mjpeg/wmav2"}, true},
		{"theora video", Media{Type: MediaTypeVideo, FileName: "a.ogg", Codec: "theora/vorbis"}, true},
		// Audio media is judged by audio codecs only: a video codec in its
		// string is cover art or a track the audio player ignores.
		{"ogg audio with theora track", Media{Type: MediaTypeAudio, FileName: "a.ogg", Codec: "theora/vorbis"}, false},
		{"audio row holding only a video codec", Media{Type: MediaTypeAudio, FileName: "a.ogg", Codec: "theora"}, false},
		// Formats the clients play as-is.
		{"mp4 h264", Media{Type: MediaTypeVideo, FileName: "a.mp4", Codec: "h264"}, false},
		{"mp4 h264 aac", Media{Type: MediaTypeVideo, FileName: "a.mp4", Codec: "h264/aac"}, false},
		{"mkv h264", Media{Type: MediaTypeVideo, FileName: "a.mkv", Codec: "h264"}, false},
		{"mp3 with cover art", Media{Type: MediaTypeAudio, FileName: "a.mp3", Codec: "mjpeg/mp3"}, false},
		{"webm", Media{Type: MediaTypeVideo, FileName: "a.webm", Codec: "vp9"}, false},
		{"mp3", Media{Type: MediaTypeAudio, FileName: "a.mp3", Codec: "mp3"}, false},
		{"flac", Media{Type: MediaTypeAudio, FileName: "a.flac", Codec: "flac"}, false},
		// Never for images or untyped rows, even with a misleading name.
		{"image", Media{Type: MediaTypeImage, FileName: "a.jpg", Codec: "mjpeg"}, false},
		{"image named avi", Media{Type: MediaTypeImage, FileName: "a.avi"}, false},
		{"no type", Media{FileName: "a.avi"}, false},
	}
}

func TestMedia_NeedsCompatStream(t *testing.T) {
	for _, tt := range compatCases() {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.media.NeedsCompatStream(); got != tt.want {
				t.Errorf("NeedsCompatStream() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMedia_MarshalJSONAddsTranscoded(t *testing.T) {
	pos := 12.5
	tests := []struct {
		name       string
		media      any // value and pointer must marshal identically
		transcoded bool
		wantPos    bool
	}{
		{"legacy value", Media{ID: 3, Type: MediaTypeVideo, FileName: "a.avi"}, true, false},
		{"legacy pointer", &Media{ID: 3, Type: MediaTypeVideo, FileName: "a.avi"}, true, false},
		{"native with position", Media{ID: 3, Type: MediaTypeVideo, FileName: "a.mp4", PositionSeconds: &pos}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.media)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got["transcoded"] != tt.transcoded {
				t.Errorf("transcoded = %v, want %v (%s)", got["transcoded"], tt.transcoded, raw)
			}
			// The stored fields and their tags must be unaffected.
			if got["id"] != float64(3) || got["file_name"] == "" {
				t.Errorf("stored fields missing: %s", raw)
			}
			if _, ok := got["position_seconds"]; ok != tt.wantPos {
				t.Errorf("position_seconds present = %v, want %v", ok, tt.wantPos)
			}
		})
	}
}

func TestMedia_JSONRoundTrip(t *testing.T) {
	in := Media{ID: 9, SetID: 2, Type: MediaTypeAudio, FileName: "song.wma", Codec: "wmav2", Duration: 3.5}
	raw, err := json.Marshal([]Media{in})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out []Media
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].ID != in.ID || out[0].FileName != in.FileName || out[0].Codec != in.Codec || out[0].Duration != in.Duration {
		t.Errorf("round trip changed media: %+v", out)
	}
}
