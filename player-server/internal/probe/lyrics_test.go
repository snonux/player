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

func TestLyricsArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	args, err := lyricsArgs(path)
	if err != nil {
		t.Fatal(err)
	}
	input, err := ffsafe.InputArgs(path)
	if err != nil {
		t.Fatal(err)
	}
	ffsafetest.AssertHardened(t, args, input, path)
}
func TestReadLyricsReal(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	for _, extension := range []string{".mp3", ".flac", ".ogg", ".m4a"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "song"+extension)
			text := "[00:00.5]embedded first\n[00:01]embedded second"
			ffsafetest.Generate(t, path, "-f", "lavfi", "-i", "sine=duration=0.1", "-metadata", "lyrics="+text)
			got, err := ReadLyrics(context.Background(), path)
			if err != nil || got != text {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "evil.mp3")
	if err := os.WriteFile(path, []byte("ffconcat version 1.0\nfile '/etc/passwd'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadLyrics(context.Background(), path); err == nil || got != "" {
		t.Fatalf("playlist accepted %q %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadLyrics(ctx, path); err == nil {
		t.Fatal("cancelled read succeeded")
	}
}
func TestLyricsOutputBound(t *testing.T) {
	b := &boundedLyricsOutput{}
	if _, err := b.Write([]byte(strings.Repeat("a", 2*MaxLyricsBytes+1))); err == nil || b.Len() != 0 {
		t.Fatal("output not bounded")
	}
}

func TestReadLyricsCandidatesReal(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "song.flac")
	ffsafetest.Generate(t, path, "-f", "lavfi", "-i", "sine=duration=0.1", "-metadata", "lyrics=<tt>malformed", "-metadata", "unsyncedlyrics=fallback")
	got, err := ReadLyricsCandidates(context.Background(), path)
	if err != nil || len(got) != 2 || got[0] != "<tt>malformed" || got[1] != "fallback" {
		t.Fatalf("%q %v", got, err)
	}
}
