package probe

import (
	"context"
	"testing"
	"time"

	"codeberg.org/snonux/play/internal/model"
)

func TestParseFFprobeOutput(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    *model.Metadata
		wantErr bool
	}{
		{
			name: "video with all fields",
			input: `{
				"format": {"duration": "123.45", "bit_rate": "500000"},
				"streams": [
					{"codec_name": "h264", "width": 1920, "height": 1080, "codec_type": "video"},
					{"codec_name": "aac", "codec_type": "audio"}
				]
			}`,
			want: &model.Metadata{
				Duration:   123.45,
				Codec:      "h264",
				Resolution: "1920x1080",
				Bitrate:    500000,
			},
		},
		{
			name: "audio only no video stream",
			input: `{
				"format": {"duration": "200.1", "bit_rate": "128000"},
				"streams": [
					{"codec_name": "mp3", "codec_type": "audio"}
				]
			}`,
			want: &model.Metadata{
				Duration: 200.1,
				Codec:    "mp3",
				Bitrate:  128000,
			},
		},
		{
			name:    "invalid json",
			input:   `{bad json`,
			wantErr: true,
		},
		{
			name: "empty streams uses first stream fallback",
			input: `{
				"format": {},
				"streams": [
					{"codec_name": "vp9", "codec_type": "video", "width": 0, "height": 0}
				]
			}`,
			want: &model.Metadata{Codec: "vp9"},
		},
		{
			name:  "no format or streams",
			input: `{"format":{},"streams":[]}`,
			want:  &model.Metadata{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFFprobeOutput([]byte(c.input))
			if (err != nil) != c.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.wantErr {
				return
			}
			if got.Duration != c.want.Duration {
				t.Errorf("Duration = %v, want %v", got.Duration, c.want.Duration)
			}
			if got.Codec != c.want.Codec {
				t.Errorf("Codec = %v, want %v", got.Codec, c.want.Codec)
			}
			if got.Resolution != c.want.Resolution {
				t.Errorf("Resolution = %v, want %v", got.Resolution, c.want.Resolution)
			}
			if got.Bitrate != c.want.Bitrate {
				t.Errorf("Bitrate = %v, want %v", got.Bitrate, c.want.Bitrate)
			}
		})
	}
}

func TestMockProber(t *testing.T) {
	ctx := context.Background()
	m := &MockProber{}
	meta, err := m.Probe(ctx, "any")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}

	m.ProbeFunc = func(context.Context, string) (*model.Metadata, error) {
		return &model.Metadata{Duration: 42}, nil
	}
	meta, err = m.Probe(ctx, "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Duration != 42 {
		t.Errorf("duration = %v, want 42", meta.Duration)
	}
}

func TestFFProber_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	p := NewFFProber()
	_, err := p.Probe(ctx, "nonexistent_path_should_fail")
	if err == nil {
		t.Fatal("expected error when context is cancelled or ffprobe fails")
	}
}
