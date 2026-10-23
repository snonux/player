package thumb

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/snonux/player/internal/ffsafe"
	"github.com/snonux/player/internal/ffsafe/ffsafetest"
)

func TestEmbeddedArtworkArgs(t *testing.T) {
	input := filepath.Join(t.TempDir(), "-song %03d.flac")
	output := filepath.Join(t.TempDir(), "-art %03d.jpg")
	args, err := thumbArgs(input, output, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	safe, err := ffsafe.SourceArgs(input)
	if err != nil {
		t.Fatal(err)
	}
	ffsafetest.AssertHardened(t, args, safe, input)
	idx := slices.Index(args, "-map")
	if idx < 0 || args[idx+1] != "0:2" {
		t.Fatalf("missing exact artwork stream: %q", args)
	}
	if slices.Contains(args, "-ss") {
		t.Fatal("embedded art must not seek")
	}
	if args[len(args)-1] != "file:"+output {
		t.Fatalf("unsafe output: %q", args)
	}
}

func TestEmbeddedArtwork_RealRefusesMissingAndDisguisedAudio(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.flac")
	ffsafetest.Audio(t, plain, false)
	ffsafetest.Audio(t, filepath.Join(dir, "secret.flac"), true)
	disguised := filepath.Join(dir, "playlist.flac")
	if err := os.WriteFile(disguised, []byte("ffconcat version 1.0\nfile 'secret.flac'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{plain, disguised, filepath.Join(dir, "missing.flac")} {
		out := filepath.Join(dir, "out.jpg")
		if err := NewFFmpegGenerator().Generate(context.Background(), input, out, 120); err == nil {
			t.Fatalf("accepted audio without readable art: %s", input)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("wrote output on failure: %v", err)
		}
	}
}
