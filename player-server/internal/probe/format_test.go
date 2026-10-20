package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/ffsafe"
	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
)

func TestFormatArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	args, err := formatArgs(path)
	if err != nil {
		t.Fatal(err)
	}
	input, err := ffsafe.InputArgs(path)
	if err != nil {
		t.Fatal(err)
	}
	ffsafetest.AssertHardened(t, args, input, path)
}

func TestParseAudioFormat(t *testing.T) {
	for _, tt := range []struct {
		name, json string
		want       AudioFormat
	}{
		{"flac uses raw depth and container rate",
			`{"streams":[{"codec_name":"flac","sample_rate":"96000","channels":2,"channel_layout":"stereo","bits_per_sample":0,"bits_per_raw_sample":"24"}],"format":{"bit_rate":"2500000"}}`,
			AudioFormat{Codec: "flac", CodecLabel: "FLAC", Bitrate: 2500000, SampleRate: 96000, BitDepth: 24, Channels: 2, ChannelLayout: "stereo", Lossless: true}},
		{"pcm uses bits per sample",
			`{"streams":[{"codec_name":"pcm_s16le","sample_rate":"44100","channels":2,"bits_per_sample":16,"bit_rate":"1411200"}]}`,
			AudioFormat{Codec: "pcm_s16le", CodecLabel: "PCM", Bitrate: 1411200, SampleRate: 44100, BitDepth: 16, Channels: 2, Lossless: true}},
		{"lossy has no depth",
			`{"streams":[{"codec_name":"mp3","sample_rate":"44100","channels":2,"bits_per_raw_sample":"16","bit_rate":"320000"}],"format":{"bit_rate":"330000"}}`,
			AudioFormat{Codec: "mp3", CodecLabel: "MP3", Bitrate: 320000, SampleRate: 44100, Channels: 2}},
		{"he-aac is named",
			`{"streams":[{"codec_name":"aac","profile":"HE-AACv2","sample_rate":"48000","channels":2}]}`,
			AudioFormat{Codec: "aac", CodecLabel: "HE-AACv2", Profile: "HE-AACv2", SampleRate: 48000, Channels: 2}},
		{"unknown profile and bad numbers are dropped",
			`{"streams":[{"codec_name":"newcodec","profile":"unknown","sample_rate":"N/A","channels":-1,"bit_rate":"-5"}],"format":{"bit_rate":"x"}}`,
			AudioFormat{Codec: "newcodec", CodecLabel: "NEWCODEC"}},
		{"no audio stream", `{"streams":[],"format":{"bit_rate":"1000"}}`, AudioFormat{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAudioFormat([]byte(tt.json))
			if err != nil || got != tt.want {
				t.Fatalf("got %+v %v want %+v", got, err, tt.want)
			}
		})
	}
	if _, err := parseAudioFormat([]byte("not json")); err == nil {
		t.Fatal("invalid json accepted")
	}
}

func TestReadAudioFormatReal(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	for _, tt := range []struct {
		file       string
		args       []string
		codec      string
		sampleRate int
		bitDepth   int
		channels   int
		lossless   bool
	}{
		{"hi.flac", []string{"-ar", "96000", "-ac", "2", "-sample_fmt", "s32"}, "flac", 96000, 24, 2, true},
		{"cd.wav", []string{"-ar", "44100", "-c:a", "pcm_s16le"}, "pcm_s16le", 44100, 16, 1, true},
		{"song.mp3", []string{"-ar", "48000", "-b:a", "192k"}, "mp3", 48000, 0, 1, false},
	} {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join(dir, tt.file)
			ffsafetest.Generate(t, path, append([]string{"-f", "lavfi", "-i", "sine=duration=0.5"}, tt.args...)...)
			got, err := ReadAudioFormat(context.Background(), path)
			if err != nil || got.Codec != tt.codec || got.SampleRate != tt.sampleRate || got.BitDepth != tt.bitDepth ||
				got.Channels != tt.channels || got.Lossless != tt.lossless || got.Bitrate <= 0 {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	// Embedded cover art is a video stream and must not hide the audio one.
	art := filepath.Join(dir, "art.flac")
	ffsafetest.Audio(t, art, true)
	if got, err := ReadAudioFormat(context.Background(), art); err != nil || got.Codec != "flac" {
		t.Fatalf("with artwork: %+v %v", got, err)
	}
	evil := filepath.Join(dir, "evil.mp3")
	if err := os.WriteFile(evil, []byte("ffconcat version 1.0\nfile '/etc/passwd'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadAudioFormat(context.Background(), evil); err == nil || got != (AudioFormat{}) {
		t.Fatalf("playlist accepted %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadAudioFormat(ctx, filepath.Join(dir, "hi.flac")); err == nil || !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("cancelled read: %v", err)
	}
}
