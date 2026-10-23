package probe

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
)

func TestAudioTagNumbers(t *testing.T) {
	for _, tc := range []struct {
		value                 string
		gain, positive, valid bool
		want                  float64
	}{
		{" -6.25 dB ", true, false, true, -6.25}, {"0 DB", true, false, true, 0}, {"+2.5", true, false, true, 2.5},
		{"123.5", false, true, true, 123.5}, {"0", false, true, false, 0}, {"0", false, false, true, 0},
		{"NaN", true, false, false, 0}, {"Inf", true, false, false, 0}, {"1e999", true, false, false, 0},
		{"-0.1", false, false, false, 0}, {"120 BPM", false, true, false, 0}, {"1/2", false, true, false, 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got := audioNumber(tc.value, tc.gain, tc.positive)
			if (got != nil) != tc.valid || (got != nil && *got != tc.want) {
				t.Fatalf("got %v want valid=%v value=%v", got, tc.valid, tc.want)
			}
		})
	}
	m := parseMusicTags(map[string]string{"tbpm": "125", "replaygain_track_gain": "0 dB", "replaygain_album_gain": "-6 dB", "replaygain_track_peak": "0.89", "replaygain_album_peak": "1"})
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var saved model.MusicMetadata
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.BPM == nil || *saved.BPM != 125 || saved.ReplayGainTrackGainDB == nil || *saved.ReplayGainTrackGainDB != 0 || saved.ReplayGainAlbumGainDB == nil || *saved.ReplayGainAlbumGainDB != -6 || saved.ReplayGainTrackPeak == nil || *saved.ReplayGainTrackPeak != 0.89 {
		t.Fatalf("snapshot %s", data)
	}
	cleared := parseMusicTags(map[string]string{})
	if cleared.BPM != nil || cleared.ReplayGainTrackGainDB != nil {
		t.Fatalf("cleared %+v", cleared)
	}
}

func TestAudioSourceFieldsFirstAudioStream(t *testing.T) {
	data := []byte(`{"streams":[{"codec_type":"video","sample_rate":"96000","bits_per_sample":32,"channels":8},{"codec_type":"audio","codec_name":"flac","sample_rate":"48000","bits_per_raw_sample":"24","bits_per_sample":16,"channels":2},{"codec_type":"audio","sample_rate":"22050","bits_per_sample":8,"channels":1}]}`)
	meta, err := parseFFprobeOutput(data, true)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Music.SampleRate != 48000 || meta.Music.BitDepth != 24 || meta.Music.Channels != 2 {
		t.Fatalf("%+v", meta.Music)
	}
	meta, err = parseFFprobeOutput([]byte(`{"streams":[{"codec_type":"audio","codec_name":"mp3","sample_rate":"44100","sample_fmt":"fltp","bits_per_sample":0,"channels":1}]}`), true)
	if err != nil || meta.Music.BitDepth != 0 || meta.Music.SampleRate != 44100 || meta.Music.Channels != 1 {
		t.Fatalf("%+v %v", meta, err)
	}
	meta, err = parseFFprobeOutput([]byte(`{"streams":[{"codec_type":"audio","bits_per_raw_sample":"0","bits_per_sample":16}]}`), true)
	if err != nil || meta.Music.BitDepth != 16 {
		t.Fatalf("%+v %v", meta, err)
	}
}

func TestAudioTechnicalRealContainers(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	for _, tc := range []struct {
		ext, codec string
		depth      int
	}{{"flac", "flac", 24}, {"mp3", "libmp3lame", 0}, {"opus", "libopus", 0}, {"m4a", "aac", 0}} {
		t.Run(tc.ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source."+tc.ext)
			args := []string{"-f", "lavfi", "-i", "sine=duration=0.1:sample_rate=48000", "-ac", "2", "-c:a", tc.codec}
			if tc.ext == "flac" {
				args = append(args, "-sample_fmt", "s32")
			}
			args = append(args, "-metadata", "title=Source test", "-metadata", "bpm=123.5", "-metadata", "replaygain_track_gain=0 dB", "-metadata", "replaygain_album_gain=-6.25 dB", "-metadata", "replaygain_track_peak=0.89")
			ffsafetest.Generate(t, path, args...)
			meta, err := NewFFProber().Probe(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			m := meta.Music
			if m.SampleRate != 48000 || m.BitDepth != tc.depth || m.Channels != 2 {
				t.Fatalf("source fields %+v", m)
			}
			// MP4 doesn't preserve arbitrary tags by default; known source fields still do.
			if tc.ext != "m4a" && (m.BPM == nil || *m.BPM != 123.5 || m.ReplayGainTrackGainDB == nil || *m.ReplayGainTrackGainDB != 0 || m.ReplayGainAlbumGainDB == nil || *m.ReplayGainAlbumGainDB != -6.25) {
				t.Fatalf("tags %+v", m)
			}
		})
	}
}
