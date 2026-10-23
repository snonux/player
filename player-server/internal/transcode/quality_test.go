package transcode

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSelectedQualityCache(t *testing.T) {
	runner := &fakeRunner{payload: "quality"}
	c, src, clk := newTestCache(t, runner, Options{})
	paths := map[string]bool{}
	for _, kind := range []Kind{KindAudio, KindAAC128, KindAAC256, KindMP3192, KindOpus96} {
		src.Kind = kind
		r, err := c.Ensure(context.Background(), src)
		if err != nil {
			t.Fatal(err)
		}
		if paths[r.Path] {
			t.Fatal("profiles shared cached bytes")
		}
		paths[r.Path] = true
	}
	waitIdle(t, c)
	files, total, err := c.scan()
	if err != nil || len(files) != 5 || total != 35 {
		t.Fatalf("profile budget scan: files=%d bytes=%d err=%v", len(files), total, err)
	}
	for path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("another profile removed %s: %v", path, err)
		}
	}
	clk.T = clk.T.Add(time.Hour)
	c.opts.MaxBytes = 10
	if err := c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, total, err = c.scan()
	if err != nil || total > 10 {
		t.Fatalf("selected profile budget not enforced: %d %v", total, err)
	}
}

func TestSelectedQualityEncoder(t *testing.T) {
	f := &FFmpegRunner{threads: 1}
	for _, tc := range []struct {
		kind             Kind
		codec, rate, mux string
	}{
		{KindAAC128, "aac", "128k", "mp4"}, {KindAAC256, "aac", "256k", "mp4"}, {KindMP3192, "libmp3lame", "192k", "mp3"}, {KindOpus96, "libopus", "96k", "ogg"},
	} {
		args := strings.Join(f.args(plan{kind: tc.kind}, []string{"-i", "source"}, "/out", 100), " ")
		for _, expected := range []string{"-c:a " + tc.codec, "-b:a " + tc.rate, "-f " + tc.mux, "-map 0:a:0 -vn", "-fs 100"} {
			if !strings.Contains(args, expected) {
				t.Errorf("profile %d missing %s in %s", tc.kind, expected, args)
			}
		}
	}
}
