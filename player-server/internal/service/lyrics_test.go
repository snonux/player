package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
)

func TestParseLyrics(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		synced     bool
		texts      []string
		times      []float64
	}{
		{"plain", "\ufefffirst\r\n\r\n<script>second</script>", false, []string{"first", "<script>second</script>"}, nil},
		{"timestamps", "[ar:artist]\n[lang:eng]\n[00:02.25][00:01] chorus\n[00:00.20]intro\n[offset:-500]", true, []string{"intro", "chorus", "chorus"}, []float64{.7, 1.5, 2.75}},
		{"positive offset", "[offset: +200 ]\n[00:00]zero\n[00:01.123]one", true, []string{"zero", "one"}, []float64{0, .923}},
		{"metadata only", "[ti:title]\n[ar:artist]", false, nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLyrics(tt.text, "sidecar")
			if result.Synced != tt.synced || len(result.Lines) != len(tt.texts) {
				t.Fatalf("%+v", result)
			}
			for i, line := range result.Lines {
				if line.Text != tt.texts[i] {
					t.Errorf("text %q", line.Text)
				}
				if tt.synced {
					if line.Start == nil || *line.Start != tt.times[i] {
						t.Errorf("start %v want %v", line.Start, tt.times[i])
					}
				} else if line.Start != nil {
					t.Fatal("plain has timestamp")
				}
			}
		})
	}
}
func TestLocalLyricsPrecedenceAndSafety(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.mp3")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "fake media")
	media := &model.Media{AbsPath: path, Type: model.MediaTypeAudio}
	lrc := filepath.Join(root, "song.lrc")
	txt := filepath.Join(root, "song.txt")
	write(lrc, "[00:01]synced")
	write(txt, "plain")
	got := localLyrics(context.Background(), root, media)
	if got.Source != "sidecar" || !got.Synced || got.Lines[0].Text != "synced" {
		t.Fatalf("%+v", got)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	if result := localLyrics(context.Background(), relativeRoot, media); len(result.Lines) != 1 || !result.Synced {
		t.Fatalf("relative root with absolute source: %+v", result)
	}
	linkRoot := filepath.Join(t.TempDir(), "library")
	if err := os.Symlink(root, linkRoot); err != nil {
		t.Fatal(err)
	}
	linked := &model.Media{AbsPath: filepath.Join(linkRoot, "song.mp3"), Type: model.MediaTypeAudio}
	if result := localLyrics(context.Background(), linkRoot, linked); len(result.Lines) != 1 || !result.Synced {
		t.Fatalf("symlink root: %+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := localLyrics(ctx, root, media); len(result.Lines) != 0 {
		t.Fatal("cancelled lookup returned lyrics")
	}
	os.Remove(lrc)
	got = localLyrics(context.Background(), root, media)
	if got.Synced || got.Lines[0].Text != "plain" {
		t.Fatalf("%+v", got)
	}
	os.Remove(txt)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(outside, "private")
	for _, tt := range []struct {
		name  string
		setup func()
	}{
		{"symlink sidecar", func() {
			if err := os.Symlink(outside, lrc); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversize", func() { write(lrc, strings.Repeat("a", probe.MaxLyricsBytes+1)) }},
		{"directory", func() {
			if err := os.Mkdir(lrc, 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"fifo", func() {
			if err := syscall.Mkfifo(lrc, 0600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer os.Remove(lrc)
			got := localLyrics(context.Background(), root, media)
			if len(got.Lines) != 0 {
				t.Fatalf("unsafe lyrics: %+v", got)
			}
		})
	}
	media.AbsPath = outside
	if got := localLyrics(context.Background(), root, media); len(got.Lines) != 0 {
		t.Fatal("outside source")
	}
	media.AbsPath = filepath.Join(root, "link.mp3")
	os.Symlink(outside, media.AbsPath)
	if got := localLyrics(context.Background(), root, media); len(got.Lines) != 0 {
		t.Fatal("symlink source escaped")
	}
	media.AbsPath = path
	media.Type = model.MediaTypeImage
	if got := localLyrics(context.Background(), root, media); len(got.Lines) != 0 {
		t.Fatal("image lyrics")
	}
}
func TestLyricsExpansionBound(t *testing.T) {
	got := parseLyrics(strings.Repeat("[00:01]", 1000)+strings.Repeat("x", 10000), "sidecar")
	if len(got.Lines) != 0 {
		t.Fatal("expanded lyrics exceeded limit")
	}
}

func TestLocalLyricsRealEmbeddedFallback(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	ffsafetest.Generate(t, path, "-f", "lavfi", "-i", "sine=duration=0.1", "-metadata", "lyrics=[00:01]embedded")
	media := &model.Media{AbsPath: path, Type: model.MediaTypeAudio}
	got := localLyrics(context.Background(), root, media)
	if got.Source != "embedded" || !got.Synced || len(got.Lines) != 1 || got.Lines[0].Text != "embedded" {
		t.Fatalf("%+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "song.txt"), []byte("sidecar"), 0600); err != nil {
		t.Fatal(err)
	}
	got = localLyrics(context.Background(), root, media)
	if got.Source != "sidecar" || got.Synced || got.Lines[0].Text != "sidecar" {
		t.Fatalf("%+v", got)
	}
}

func TestLocalLyricsAdditionalEmbeddedFallback(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	ffsafetest.Generate(t, path, "-f", "lavfi", "-i", "sine=duration=0.1", "-metadata", "lyrics=<tt>malformed", "-metadata", "unsyncedlyrics=1\n00:00:01,000 --> 00:00:02,000\nembedded fallback")
	media := &model.Media{AbsPath: path, Type: model.MediaTypeAudio}
	got := localLyrics(context.Background(), root, media)
	if got.Source != "embedded" || !got.Synced || len(got.Lines) != 1 || got.Lines[0].Text != "embedded fallback" {
		t.Fatalf("%+v", got)
	}
}
